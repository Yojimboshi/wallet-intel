package exit

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/big"
	"sync"
	"time"

	"github.com/Yojimboshi/wallet-intel/internal/alerts"
	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/Yojimboshi/wallet-intel/internal/config"
	"github.com/Yojimboshi/wallet-intel/internal/enrich"
	"github.com/Yojimboshi/wallet-intel/internal/execute"
	"github.com/Yojimboshi/wallet-intel/internal/parse"
	"github.com/Yojimboshi/wallet-intel/internal/pool"
	"github.com/Yojimboshi/wallet-intel/internal/store"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

type priceCache struct {
	mu    sync.Mutex
	items map[string]cachedPrice
}

type cachedPrice struct {
	info      enrich.TokenInfo
	fetchedAt time.Time
}

type Monitor struct {
	chainCfg      chain.Config
	policy        config.ExitPolicy
	positions     *store.Positions
	enricher      enrich.Lookup
	executor      execute.Executor
	notify        alerts.Notifier
	execCfg       config.ExecutionConfig
	nativeOracle  *pool.NativeOracle
	events        *store.EventLog
	hodlBook      *store.HodlBook
	prices        priceCache
	exitState     map[string]exitAttempt
	exitMu        sync.Mutex
	persistTick   uint64
	lastCheck     time.Time
	checkMu       sync.Mutex
	readClient    *ethclient.Client
}

type exitAttempt struct {
	alerted       bool
	alertReason   string
	tries         int
	slippagePolls int
}

func NewMonitor(
	chainCfg chain.Config,
	policy config.ExitPolicy,
	positions *store.Positions,
	enricher enrich.Lookup,
	executor execute.Executor,
	notify alerts.Notifier,
	execCfg config.ExecutionConfig,
	nativeOracle *pool.NativeOracle,
	events *store.EventLog,
	hodlBook *store.HodlBook,
) *Monitor {
	return &Monitor{
		chainCfg:     chainCfg,
		policy:       policy.WithDefaults(),
		positions:    positions,
		enricher:     enricher,
		executor:     executor,
		notify:       notify,
		execCfg:      execCfg,
		nativeOracle: nativeOracle,
		events:       events,
		hodlBook:     hodlBook,
		prices:       priceCache{items: make(map[string]cachedPrice)},
		exitState:    make(map[string]exitAttempt),
	}
}

func (m *Monitor) Run(ctx context.Context, client *ethclient.Client) {
	if !m.policy.EnabledForMonitor() {
		return
	}
	m.readClient = client

	if client != nil && m.policy.UseBlockMonitor() {
		m.runBlockMonitor(ctx, client)
		return
	}

	interval := time.Duration(m.policy.PollIntervalSec) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("exit monitor on %s every %s (dexscreener poll)", m.chainCfg.Name, interval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.tick(ctx, client, false)
		}
	}
}

func (m *Monitor) runBlockMonitor(ctx context.Context, client *ethclient.Client) {
	headers := make(chan *types.Header)
	sub, err := client.SubscribeNewHead(ctx, headers)
	if err != nil {
		log.Printf("exit block subscribe failed (%v) — falling back to poll", err)
		m.policy.MonitorOnBlock = false
		m.Run(ctx, client)
		return
	}
	defer sub.Unsubscribe()

	gap := time.Duration(m.policy.PollIntervalSec) * time.Second
	if gap <= 0 {
		gap = 30 * time.Second
	}
	log.Printf("exit monitor on %s newHeads (pool reads at most every %s)", m.chainCfg.Name, gap)
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-sub.Err():
			log.Printf("exit block sub error: %v", err)
			return
		case h := <-headers:
			if h == nil {
				continue
			}
			m.tick(ctx, client, true)
		}
	}
}

func (m *Monitor) tick(ctx context.Context, client *ethclient.Client, onBlock bool) {
	if ctx.Err() != nil {
		return
	}
	if !m.hasOpenOnChain() {
		return
	}
	minGap := time.Duration(m.policy.PollIntervalSec) * time.Second
	if minGap <= 0 {
		minGap = 30 * time.Second
	}
	m.checkMu.Lock()
	if !m.lastCheck.IsZero() && time.Since(m.lastCheck) < minGap {
		m.checkMu.Unlock()
		return
	}
	m.lastCheck = time.Now()
	m.checkMu.Unlock()

	open := m.positions.OpenList()
	for _, pos := range open {
		if pos.Chain != string(m.chainCfg.ID) {
			continue
		}
		if err := m.checkPosition(ctx, client, pos, onBlock); err != nil {
			if !isContextDone(err) {
				log.Printf("exit monitor %s: %v", pos.TokenSymbol, err)
			}
		}
	}
}

func (m *Monitor) checkPosition(ctx context.Context, client *ethclient.Client, pos store.Position, onBlock bool) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	token := common.HexToAddress(pos.Token)
	info, err := m.lookupPrice(ctx, token)
	if err != nil {
		return fmt.Errorf("price %s: %w", pos.Token, err)
	}

	liqUsd := info.Liquidity
	if client != nil && pos.Pair != "" {
		pair := common.HexToAddress(pos.Pair)
		nativePrice, err := m.nativeUSD(ctx)
		if err != nil {
			if isContextDone(err) {
				return nil
			}
			log.Printf("native usd price %s: %v — skipping on-chain liq", m.chainCfg.ID, err)
		} else {
			snap, err := pool.ReadSnapshot(ctx, client, m.chainCfg, pair, pos.DEX, nativePrice)
			if err != nil {
				if isContextDone(err) {
					return nil
				}
				log.Printf("pool liq %s (%s %s): %v — using dexscreener", pos.TokenSymbol, pos.DEX, pos.Pair, err)
			} else {
				onChainLiq := snap.TVLUsd
				liqUsd = onChainLiq
				info.Liquidity = onChainLiq
				m.persistTick++
				if m.persistTick%12 == 0 {
					_ = m.positions.UpdateLiquidity(pos.Chain, pos.Token, onChainLiq)
				}
				if pos.EntryLiquidityUsd > 0 && onChainLiq < pos.EntryLiquidityUsd*0.75 {
					log.Printf("liq watch %s [%s]: entry $%.0f → tvl $%.0f active $%.0f",
						pos.TokenSymbol, snap.Kind, pos.EntryLiquidityUsd, snap.TVLUsd, snap.ActiveLiqUsd)
				}
			}
		}
	}

	sig, ok := Evaluate(pos, info, liqUsd, m.policy, time.Now().UTC())
	if !ok {
		return nil
	}
	return m.triggerExit(ctx, pos, info, sig)
}

func (m *Monitor) lookupPrice(ctx context.Context, token common.Address) (enrich.TokenInfo, error) {
	key := token.Hex()
	m.prices.mu.Lock()
	if c, ok := m.prices.items[key]; ok && time.Since(c.fetchedAt) < 15*time.Second {
		m.prices.mu.Unlock()
		return c.info, nil
	}
	m.prices.mu.Unlock()

	info, err := m.enricher.LookupToken(ctx, token)
	if err != nil {
		return enrich.TokenInfo{}, err
	}
	m.prices.mu.Lock()
	m.prices.items[key] = cachedPrice{info: info, fetchedAt: time.Now().UTC()}
	m.prices.mu.Unlock()
	return info, nil
}

func (m *Monitor) OnWalletSell(ctx context.Context, tr parse.Trade, info enrich.TokenInfo) error {
	if !m.policy.MirrorWalletSell {
		return nil
	}
	pos, ok := m.positions.FindOpen(string(m.chainCfg.ID), tr.Token.Hex())
	if !ok {
		return nil
	}
	if !sameAddr(pos.SourceWallet, tr.Wallet.Hex()) {
		return nil
	}
	sig := Signal{Reason: fmt.Sprintf("mirror sell by %s", tr.WalletLabel), PnLPct: 0, Liquidity: info.Liquidity}
	if info.PriceUsd > 0 && pos.EntryPriceUsd > 0 {
		sig.PnLPct = (info.PriceUsd - pos.EntryPriceUsd) / pos.EntryPriceUsd * 100
		sig.CurrentUsd = info.PriceUsd
	}
	return m.triggerExit(ctx, pos, info, sig)
}

func (m *Monitor) triggerExit(ctx context.Context, pos store.Position, info enrich.TokenInfo, sig Signal) error {
	sym := pos.TokenSymbol
	if sym == "" {
		sym = info.Symbol
	}
	if sym == "" {
		sym = pos.Token
	}

	if client := m.readClient; client != nil {
		bal, execAddr, err := m.tokenBalance(ctx, client, pos)
		if err != nil {
			log.Printf("[exit] balance check %s %s: %v", pos.Chain, sym, err)
		} else if bal.Sign() <= 0 {
			reason := fmt.Sprintf("no holdings on exec wallet %s", execAddr.Hex())
			log.Printf("[exit] CLOSE %s %s: %s", pos.Chain, sym, reason)
			m.clearExitState(pos)
			return m.closeNoHoldings(pos, sym, sig, reason)
		}
	}

	key := positionExitKey(pos.Chain, pos.Token)
	m.exitMu.Lock()
	st := m.exitState[key]
	if st.tries >= maxExitSellTries {
		m.exitMu.Unlock()
		return nil
	}
	firstAlert := !st.alerted || st.alertReason != sig.Reason
	if firstAlert {
		st.alerted = true
		st.alertReason = sig.Reason
		m.exitState[key] = st
	}
	m.exitMu.Unlock()

	if firstAlert {
		log.Printf("[exit] TRIGGER %s %s: %s (pnl %.1f%%)",
			pos.Chain, sym, sig.Reason, sig.PnLPct)
		_ = m.events.Append(store.Event{
			Type:    "exit_trigger",
			Chain:   pos.Chain,
			Token:   pos.Token,
			Symbol:  sym,
			Reason:  sig.Reason,
			PnLPct:  sig.PnLPct,
			SizeUsd: pos.EntrySizeUsd,
			Detail:  fmt.Sprintf("entry=$%.6f now=$%.6f liq=$%.0f stage=%d frac=%d keep=%v collect=%v",
				pos.EntryPriceUsd, sig.CurrentUsd, sig.Liquidity, sig.Stage, sig.SellFractionBps, sig.KeepOpen, sig.TransferToCollector),
		})

		msg := FormatExitAlert(pos, info, sig)
		if err := m.notify.Send(ctx, msg); err != nil {
			log.Printf("exit alert: %v", err)
		}
	}

	closeReason := sig.Reason
	if m.executor == nil || !m.execCfg.AllowLiveExecution {
		log.Printf("exit signal [%s] %s — execution off, alert only", sym, sig.Reason)
		closeReason = sig.Reason + " (dry)"
		m.clearExitState(pos)
		return m.finishExit(ctx, pos, info, sym, sig, closeReason, common.Hash{})
	}

	tradeSide := "sell"
	if sig.TransferToCollector {
		tradeSide = "transfer"
	}
	execWallet, err := m.resolveExecWallet(pos)
	if err != nil {
		log.Printf("[exit] %s failed %s %s: %v", tradeSide, pos.Chain, sym, err)
		return err
	}
	req := execute.Request{
		SourceWallet: common.HexToAddress(pos.SourceWallet),
		SourceLabel:  pos.SourceLabel,
		Trade: parse.Trade{
			Wallet:      common.HexToAddress(pos.SourceWallet),
			WalletLabel: pos.SourceLabel,
			Side:        tradeSide,
			Token:       common.HexToAddress(pos.Token),
			TxHash:      common.HexToHash(pos.EntryTx),
		},
		Token:             info,
		Chain:             pos.Chain,
		SizeUsd:           pos.EntrySizeUsd,
		QuoteToken:        common.HexToAddress(pos.QuoteToken),
		HubToken:          common.HexToAddress(pos.HubToken),
		SellFractionBps:   sig.SellFractionBps,
		TransferRecipient: m.execCfg.CollectorEVM,
		ExecWallet:        execWallet,
	}
	txHash, err := m.executor.Mirror(ctx, req)
	if err != nil {
		if isNoBalanceSellErr(err) {
			reason := "no holdings on exec wallet"
			log.Printf("[exit] CLOSE %s %s: %s", pos.Chain, sym, reason)
			m.clearExitState(pos)
			return m.closeNoHoldings(pos, sym, sig, reason)
		}
		if !sig.TransferToCollector && isUnsellableSellErr(err) {
			return m.closeUnsellable(pos, info, sym, sig, err.Error())
		}
		if !sig.TransferToCollector && execute.IsSlippageSellErr(err) {
			m.exitMu.Lock()
			st = m.exitState[key]
			st.slippagePolls++
			polls := st.slippagePolls
			m.exitState[key] = st
			m.exitMu.Unlock()
			if polls >= m.policy.MaxExitSlippagePolls {
				return m.markManualSellNeeded(ctx, pos, info, sym, sig, polls, err.Error())
			}
			log.Printf("[exit] sell slippage %s %s poll %d/%d — will retry: %v",
				pos.Chain, sym, polls, m.policy.MaxExitSlippagePolls, err)
			return nil
		}
		m.exitMu.Lock()
		st = m.exitState[key]
		st.tries++
		tries := st.tries
		m.exitState[key] = st
		m.exitMu.Unlock()
		log.Printf("[exit] %s failed %s %s (try %d/%d): %v", tradeSide, pos.Chain, sym, tries, maxExitSellTries, err)
		if tries >= maxExitSellTries {
			log.Printf("[exit] %s gave up %s %s — position stays open for manual action", tradeSide, pos.Chain, sym)
			return nil
		}
		return nil
	}
	m.clearExitState(pos)
	return m.finishExit(ctx, pos, info, sym, sig, closeReason, txHash)
}

func (m *Monitor) finishExit(ctx context.Context, pos store.Position, info enrich.TokenInfo, sym string, sig Signal, closeReason string, txHash common.Hash) error {
	if sig.TransferToCollector {
		m.recordCollectorHodl(pos, info, sym, txHash)
		if err := m.positions.Close(pos.Chain, pos.Token, closeReason); err != nil {
			return err
		}
		m.logPositionClose(pos, sym, closeReason, sig.PnLPct)
		return nil
	}
	if sig.KeepOpen && sig.Stage > 0 {
		if err := m.positions.MarkTPStage(pos.Chain, pos.Token, sig.Stage, closeReason); err != nil {
			return err
		}
		m.logPartialExit(pos, sym, closeReason, sig.PnLPct)
		return nil
	}
	if err := m.positions.Close(pos.Chain, pos.Token, closeReason); err != nil {
		return err
	}
	m.logPositionClose(pos, sym, closeReason, sig.PnLPct)
	return nil
}

func (m *Monitor) recordCollectorHodl(pos store.Position, info enrich.TokenInfo, sym string, txHash common.Hash) {
	if m.hodlBook == nil || m.execCfg.CollectorEVM == (common.Address{}) {
		return
	}
	remainUsd := pos.EntrySizeUsd * 0.25
	if pos.TP1Taken && !pos.TP2Taken {
		remainUsd = pos.EntrySizeUsd * 0.50
	} else if !pos.TP1Taken {
		remainUsd = pos.EntrySizeUsd * 0.25
	}
	hodl := store.HodlPosition{
		Chain:         pos.Chain,
		Token:         pos.Token,
		TokenSymbol:   pos.TokenSymbol,
		TokenName:     pos.TokenName,
		EntryTx:       txHash.Hex(),
		EntryPriceUsd: info.PriceUsd,
		EntrySizeUsd:  remainUsd,
		ExecWallet:    m.execCfg.CollectorEVM.Hex(),
		Notes:         "copy exit moonbag",
		OpenedAt:      time.Now().UTC(),
	}
	if err := m.hodlBook.Add(hodl); err != nil {
		log.Printf("[exit] hodl record %s: %v", sym, err)
	}
	_ = m.events.Append(store.Event{
		Type:    "hodl_open",
		Chain:   pos.Chain,
		Token:   pos.Token,
		Symbol:  sym,
		SizeUsd: remainUsd,
		TxHash:  txHash.Hex(),
		Detail:  "collector moonbag from copy exit",
	})
}

func (m *Monitor) markManualSellNeeded(ctx context.Context, pos store.Position, info enrich.TokenInfo, sym string, sig Signal, polls int, detail string) error {
	reason := fmt.Sprintf("manual sell needed after %d slippage polls", polls)
	log.Printf("[exit] MANUAL %s %s: %s", pos.Chain, sym, reason)
	m.clearExitState(pos)
	_ = m.positions.MarkManualExit(pos.Chain, pos.Token, reason)
	_ = m.positions.RecordManualIntervention(store.ManualIntervention{
		Chain:       pos.Chain,
		Token:       pos.Token,
		TokenSymbol: sym,
		Kind:        "sell",
		Reason:      reason,
		Detail:      detail,
	})
	msg := FormatManualSellAlert(pos, info, sig, polls)
	if err := m.notify.Send(ctx, msg); err != nil {
		log.Printf("manual sell alert: %v", err)
	}
	_ = m.events.Append(store.Event{
		Type:    "manual_sell_required",
		Chain:   pos.Chain,
		Token:   pos.Token,
		Symbol:  sym,
		Reason:  reason,
		PnLPct:  sig.PnLPct,
		SizeUsd: pos.EntrySizeUsd,
		Detail:  detail,
	})
	return nil
}

func FormatManualSellAlert(pos store.Position, info enrich.TokenInfo, sig Signal, polls int) string {
	sym := pos.TokenSymbol
	if sym == "" {
		sym = info.Symbol
	}
	return fmt.Sprintf(
		"⚠️ <b>MANUAL SELL</b> %s on %s\nAuto-sell stopped after %d slippage retries.\nLast signal: %s (%.1f%%)\nSize: $%.0f — sell wallet manually",
		sym, pos.Chain, polls, sig.Reason, sig.PnLPct, pos.EntrySizeUsd,
	)
}

func (m *Monitor) closeUnsellable(pos store.Position, info enrich.TokenInfo, sym string, sig Signal, detail string) error {
	reason := "unsellable honeypot"
	if detail != "" {
		reason = reason + ": " + detail
	}
	log.Printf("[exit] CLOSE unsellable %s %s: %s", pos.Chain, sym, reason)
	m.clearExitState(pos)
	_ = m.positions.Close(pos.Chain, pos.Token, reason)
	m.logPositionClose(pos, sym, reason, sig.PnLPct)
	return nil
}

func (m *Monitor) closeNoHoldings(pos store.Position, sym string, sig Signal, reason string) error {
	if reason == "" {
		reason = "no holdings on exec wallet"
	}
	_ = m.positions.Close(pos.Chain, pos.Token, reason)
	m.logPositionClose(pos, sym, reason, sig.PnLPct)
	return nil
}

func (m *Monitor) tokenBalance(ctx context.Context, client *ethclient.Client, pos store.Position) (*big.Int, common.Address, error) {
	execAddr := m.execWalletAddress(pos)
	token := common.HexToAddress(pos.Token)
	bal, err := execute.TokenBalance(ctx, client, token, execAddr)
	if err != nil {
		return nil, execAddr, err
	}
	return bal, execAddr, nil
}

func (m *Monitor) execWalletAddress(pos store.Position) common.Address {
	if pos.ExecWallet != "" {
		return common.HexToAddress(pos.ExecWallet)
	}
	return m.execCfg.WalletAddress
}

func (m *Monitor) resolveExecWallet(pos store.Position) (execute.Wallet, error) {
	addr := m.execWalletAddress(pos)
	if addr == (common.Address{}) {
		return execute.Wallet{}, fmt.Errorf("exec wallet not configured")
	}
	for _, w := range m.execCfg.Wallets {
		if w.Address == addr {
			return execute.Wallet{Label: w.Label, Address: w.Address, PrivateKey: w.PrivateKey}, nil
		}
	}
	if m.execCfg.WalletAddress == addr && m.execCfg.PrivateKey != "" {
		return execute.Wallet{
			Label:      m.execCfg.ActiveWallet,
			Address:    addr,
			PrivateKey: m.execCfg.PrivateKey,
		}, nil
	}
	return execute.Wallet{}, fmt.Errorf("exec wallet %s not in pool", addr.Hex())
}

func (m *Monitor) clearExitState(pos store.Position) {
	key := positionExitKey(pos.Chain, pos.Token)
	m.exitMu.Lock()
	delete(m.exitState, key)
	m.exitMu.Unlock()
}

func (m *Monitor) logPositionClose(pos store.Position, sym, reason string, pnlPct float64) {
	openN := m.positions.CountOpen()
	log.Printf("[position] CLOSE %s %s: %s (pnl %.1f%%, open: %d)",
		pos.Chain, sym, reason, pnlPct, openN)
	_ = m.events.Append(store.Event{
		Type:    "position_close",
		Chain:   pos.Chain,
		Token:   pos.Token,
		Symbol:  sym,
		Reason:  reason,
		PnLPct:  pnlPct,
		SizeUsd: pos.EntrySizeUsd,
		Detail:  fmt.Sprintf("open=%d", openN),
	})
}

func (m *Monitor) logPartialExit(pos store.Position, sym, reason string, pnlPct float64) {
	log.Printf("[position] TP1 %s %s: %s (pnl %.1f%%) — remainder open",
		pos.Chain, sym, reason, pnlPct)
	_ = m.events.Append(store.Event{
		Type:    "partial_exit",
		Chain:   pos.Chain,
		Token:   pos.Token,
		Symbol:  sym,
		Reason:  reason,
		PnLPct:  pnlPct,
		SizeUsd: pos.EntrySizeUsd,
		Detail:  "tp1 taken — remainder still open",
	})
}

func FormatExitAlert(pos store.Position, info enrich.TokenInfo, sig Signal) string {
	sym := pos.TokenSymbol
	if sym == "" {
		sym = info.Symbol
	}
	label := "EXIT"
	extra := ""
	switch {
	case sig.TransferToCollector:
		label = "MOONBAG"
		extra = "\nTransferring remainder (~25%) to collector"
	case sig.KeepOpen && sig.Stage == StageTP1:
		label = "TP1"
		pct := sig.SellFractionBps / 100
		if pct <= 0 {
			pct = 50
		}
		extra = fmt.Sprintf("\nPartial: selling %d%% — 50%% rides to 10x", pct)
	case sig.KeepOpen && sig.Stage == StageTP2:
		label = "TP2"
		pct := sig.SellFractionBps / 100
		if pct <= 0 {
			pct = 50
		}
		extra = fmt.Sprintf("\nPartial: selling %d%% of remainder — final 25%% → collector", pct)
	}
	mult := 1 + sig.PnLPct/100
	return fmt.Sprintf(
		"🔴 <b>%s</b> %s on %s\nReason: %s\nEntry: $%.4f → Now: $%.4f (%.1f%% / %.1fx)\nLiq: %s | Size: $%.0f%s",
		label, sym, pos.Chain, sig.Reason,
		pos.EntryPriceUsd, sig.CurrentUsd, sig.PnLPct, mult,
		enrich.FormatUsd(sig.Liquidity), pos.EntrySizeUsd, extra,
	)
}

func sameAddr(a, b string) bool {
	return common.HexToAddress(a) == common.HexToAddress(b)
}

func isContextDone(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (m *Monitor) hasOpenOnChain() bool {
	if m.positions == nil {
		return false
	}
	chainID := string(m.chainCfg.ID)
	for _, pos := range m.positions.OpenList() {
		if pos.Chain == chainID {
			return true
		}
	}
	return false
}

func (m *Monitor) nativeUSD(ctx context.Context) (float64, error) {
	if m.nativeOracle != nil {
		return m.nativeOracle.USD(ctx)
	}
	if m.execCfg.NativeUsdPrice > 0 {
		return m.execCfg.NativeUsdPrice, nil
	}
	return 0, fmt.Errorf("native usd price unavailable")
}
