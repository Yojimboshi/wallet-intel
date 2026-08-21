package watch

import (
	"context"
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
	"github.com/Yojimboshi/wallet-intel/internal/safety"
	"github.com/Yojimboshi/wallet-intel/internal/store"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

var topicTransfer = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))

// SellHandler reacts to watched-wallet sells (e.g. mirror exit).
type SellHandler interface {
	OnWalletSell(ctx context.Context, tr parse.Trade, info enrich.TokenInfo) error
}

type Watcher struct {
	client      *ethclient.Client
	chainCfg    chain.Config
	watched     map[common.Address]string
	copyWallets map[common.Address]bool
	rules       config.Rules
	execCfg     config.ExecutionConfig
	executor    execute.Executor
	safety      safety.Checker
	seen        *store.SeenTokens
	positions   *store.Positions
	nativeOracle *pool.NativeOracle
	onSell      SellHandler
	enricher    enrich.Lookup
	notify      alerts.Notifier
	log         *store.JSONL
	events      *store.EventLog
	txDedup     txDeduper
	batchBuy    *store.BatchBuyTracker
	flipGuard   *store.FlipGuard
	mysql       *store.MySQL
}

func New(
	client *ethclient.Client,
	chainCfg chain.Config,
	wallets []config.WatchedWallet,
	rules config.Rules,
	execCfg config.ExecutionConfig,
	executor execute.Executor,
	safetyChecker safety.Checker,
	seen *store.SeenTokens,
	positions *store.Positions,
	nativeOracle *pool.NativeOracle,
	onSell SellHandler,
	enricher enrich.Lookup,
	notify alerts.Notifier,
	tradeLog *store.JSONL,
	events *store.EventLog,
	batchBuy *store.BatchBuyTracker,
) *Watcher {
	watched := make(map[common.Address]string)
	copyWallets := make(map[common.Address]bool)
	for _, w := range wallets {
		if len(w.Chains) > 0 {
			if _, ok := w.Chains[string(chainCfg.ID)]; !ok {
				continue
			}
		}
		watched[w.Address] = w.Label
		if w.Copy {
			copyWallets[w.Address] = true
		}
	}
	return &Watcher{
		client:       client,
		chainCfg:     chainCfg,
		watched:      watched,
		copyWallets:  copyWallets,
		rules:        rules,
		execCfg:      execCfg,
		executor:     executor,
		safety:       safetyChecker,
		seen:         seen,
		positions:    positions,
		nativeOracle: nativeOracle,
		onSell:       onSell,
		enricher:     enricher,
		notify:       notify,
		log:          tradeLog,
		events:       events,
		batchBuy:     batchBuy,
		flipGuard: store.NewFlipGuard(store.FlipGuardConfig{
			RecentSellBlock: time.Duration(rules.FlipRecentSellBlockSecOrDefault()) * time.Second,
			CycleWindow:     time.Duration(rules.FlipCycleWindowSecOrDefault()) * time.Second,
			MuteAfterCycles: rules.FlipMuteAfterCyclesOrDefault(),
			MuteFor:         time.Duration(rules.FlipMuteSecOrDefault()) * time.Second,
		}),
	}
}

func (w *Watcher) UseMySQL(db *store.MySQL) {
	w.mysql = db
}

func (w *Watcher) saveDecision(ctx context.Context, d store.TradeDecision) {
	if w.mysql == nil {
		return
	}
	if err := w.mysql.InsertTradeDecision(ctx, d); err != nil {
		log.Printf("mysql trade_decision: %v", err)
	}
}

func (w *Watcher) Run(ctx context.Context) error {
	if len(w.watched) == 0 {
		return fmt.Errorf("no wallets configured for chain %s", w.chainCfg.ID)
	}

	head, err := w.client.HeaderByNumber(ctx, nil)
	if err != nil {
		return fmt.Errorf("get latest block: %w", err)
	}

	log.Printf("watching %d wallet(s) on %s from block %d", len(w.watched), w.chainCfg.Name, head.Number.Uint64())
	return w.runLogSubscriptions(ctx)
}

// runLogSubscriptions watches wallet ERC-20 transfers via eth_subscribe logs.
// Infura only charges when a block contains a matching transfer (not every block).
// Requires a WebSocket RPC endpoint (wss://).
func (w *Watcher) runLogSubscriptions(ctx context.Context) error {
	walletTopics := walletTopicHashes(w.watched)

	incoming := make(chan types.Log, 64)
	outgoing := make(chan types.Log, 64)

	inQuery := ethereum.FilterQuery{
		Topics: [][]common.Hash{
			{topicTransfer},
			nil,
			walletTopics,
		},
	}
	outQuery := ethereum.FilterQuery{
		Topics: [][]common.Hash{
			{topicTransfer},
			walletTopics,
		},
	}

	inSub, err := w.client.SubscribeFilterLogs(ctx, inQuery, incoming)
	if err != nil {
		return fmt.Errorf("subscribe incoming transfers: %w (requires WebSocket RPC wss://)", err)
	}
	defer inSub.Unsubscribe()

	outSub, err := w.client.SubscribeFilterLogs(ctx, outQuery, outgoing)
	if err != nil {
		return fmt.Errorf("subscribe outgoing transfers: %w (requires WebSocket RPC wss://)", err)
	}
	defer outSub.Unsubscribe()

	log.Printf("log subscription active on %s", w.chainCfg.Name)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-inSub.Err():
			return fmt.Errorf("incoming log subscription: %w", err)
		case err := <-outSub.Err():
			return fmt.Errorf("outgoing log subscription: %w", err)
		case lg := <-incoming:
			w.handleTransferLog(ctx, lg)
		case lg := <-outgoing:
			w.handleTransferLog(ctx, lg)
		}
	}
}

func (w *Watcher) handleTransferLog(ctx context.Context, lg types.Log) {
	if lg.Removed {
		return
	}
	if !w.txDedup.mark(lg.TxHash) {
		return
	}
	if err := w.processTx(ctx, lg.TxHash, lg.BlockNumber); err != nil {
		log.Printf("tx %s: %v", lg.TxHash.Hex(), err)
	}
}

func (w *Watcher) processTx(ctx context.Context, txHash common.Hash, blockNum uint64) error {
	receipt, err := w.client.TransactionReceipt(ctx, txHash)
	if err != nil {
		return err
	}
	trades := parse.ParseLogs(receipt.Logs, w.watched, w.chainCfg, txHash, blockNum)
	if needsNativeValue(trades) {
		tx, _, err := w.client.TransactionByHash(ctx, txHash)
		if err == nil && tx != nil && tx.Value() != nil && tx.Value().Sign() > 0 {
			parse.ApplyTxNativeValue(trades, receipt.Logs, w.chainCfg, tx.Value())
		}
	}
	if len(trades) > 1 {
		log.Printf("[tx] %s %s block=%d trades=%d",
			w.chainCfg.ID, txHash.Hex(), blockNum, len(trades))
	}
	clusters := w.buildTxClusterBuys(ctx, trades)
	for _, tr := range trades {
		if err := w.handleTrade(ctx, tr, clusters); err != nil {
			return err
		}
	}
	return nil
}

func (w *Watcher) buildTxClusterBuys(ctx context.Context, trades []parse.Trade) map[common.Address]store.TxClusterBuy {
	if len(trades) < 2 {
		return nil
	}
	txHash := trades[0].TxHash
	if txHash == (common.Hash{}) {
		return nil
	}
	legs := make([]store.TxClusterLeg, 0, len(trades))
	for _, tr := range trades {
		if tr.TxHash != txHash || tr.Side != "buy" {
			continue
		}
		info, err := w.enricher.LookupToken(ctx, tr.Token)
		if err != nil {
			info = enrich.TokenInfo{}
		}
		usd := parse.TradeUsd(tr, info, w.chainCfg, w.nativeUSD(ctx))
		legs = append(legs, store.TxClusterLeg{
			Token:    tr.Token,
			Wallet:   tr.Wallet,
			Side:     tr.Side,
			TradeUsd: usd,
		})
	}
	return store.BuildTxClusterBuys(legs)
}

func needsNativeValue(trades []parse.Trade) bool {
	for _, tr := range trades {
		if tr.Side == "buy" && (tr.QuoteAmount == nil || tr.QuoteAmount.Sign() == 0) {
			return true
		}
	}
	return false
}

func (w *Watcher) HandleTrade(ctx context.Context, tr parse.Trade) error {
	return w.handleTrade(ctx, tr, nil)
}

func (w *Watcher) handleTrade(ctx context.Context, tr parse.Trade, clusters map[common.Address]store.TxClusterBuy) error {
	chainKey := string(w.chainCfg.ID)
	if !parse.ValidToken(tr.Token) {
		log.Printf("skip alert | invalid token address | %s", w.formatTradeLine(tr, enrich.TokenInfo{}, 0))
		d := store.TradeDecisionFrom(tr, enrich.TokenInfo{}, chainKey, 0, 0, 0)
		d.AlertAction = "skip"
		d.AlertReason = "invalid token address"
		w.saveDecision(ctx, d)
		return nil
	}
	info, err := w.enricher.LookupToken(ctx, tr.Token)
	if err != nil {
		log.Printf("enrich %s: %v", tr.Token.Hex(), err)
		info = enrich.TokenInfo{}
	}

	tradeUsd := parse.TradeUsd(tr, info, w.chainCfg, w.nativeUSD(ctx))
	effectiveUsd, batchLegs, pending, pendingAction, pendingReason := w.resolveBuySize(tr, info, tradeUsd, clusters)
	if pending {
		d := store.TradeDecisionFrom(tr, info, chainKey, tradeUsd, effectiveUsd, batchLegs)
		d.AlertAction = pendingAction
		d.AlertReason = pendingReason
		w.saveDecision(ctx, d)
		return nil
	}
	if ok, reason := w.rules.PassesAlert(tr.Side, effectiveUsd, info); !ok {
		log.Printf("skip alert | %s | %s", reason, w.formatTradeLine(tr, info, tradeUsd))
		d := store.TradeDecisionFrom(tr, info, chainKey, tradeUsd, effectiveUsd, batchLegs)
		d.AlertAction = "skip"
		d.AlertReason = reason
		w.saveDecision(ctx, d)
		return nil
	}

	sym := info.Symbol
	if sym == "" {
		sym = tr.Token.Hex()
	}
	watchLine := w.formatTradeLine(tr, info, effectiveUsd)
	if batchLegs > 1 {
		watchLine = fmt.Sprintf("batch %d legs $%.0f | %s", batchLegs, effectiveUsd, watchLine)
	}

	flip := w.flipGuard.Observe(chainKey, tr.Wallet.Hex(), tr.Token.Hex(), tr.Side, time.Now().UTC())
	if flip.Muted && !flip.JustMuted {
		log.Printf("skip alert | flip mute (%d cycles) | %s", flip.Cycles, watchLine)
		d := store.TradeDecisionFrom(tr, info, chainKey, tradeUsd, effectiveUsd, batchLegs)
		d.AlertAction = "skip"
		d.AlertReason = fmt.Sprintf("flip mute after %d cycles", flip.Cycles)
		if tr.Side == "sell" && w.onSell != nil {
			if err := w.onSell.OnWalletSell(ctx, tr, info); err != nil {
				log.Printf("exit on sell %s: %v", tr.Token.Hex(), err)
			}
		}
		copyAction, copyReason, copySize := w.tryCopy(ctx, tr, info, effectiveUsd)
		d.CopyAction = copyAction
		d.CopyReason = copyReason
		d.CopySizeUsd = copySize
		w.saveDecision(ctx, d)
		return nil
	}

	log.Printf("[watch] %s", watchLine)
	_ = w.events.Append(store.Event{
		Type:        "watch",
		Chain:       chainKey,
		Token:       tr.Token.Hex(),
		Symbol:      sym,
		Side:        tr.Side,
		Wallet:      tr.Wallet.Hex(),
		WalletLabel: tr.WalletLabel,
		SizeUsd:     effectiveUsd,
		TxHash:      tr.TxHash.Hex(),
		Detail:      batchDetail(batchLegs, effectiveUsd, tradeUsd),
	})

	msg := alerts.FormatTrade(tr, info, w.chainCfg)
	if batchLegs > 1 {
		msg = fmt.Sprintf("Batch buy (%d legs, ~$%.0f total)\n%s", batchLegs, effectiveUsd, msg)
	}
	if err := w.notify.Send(ctx, msg); err != nil {
		return err
	}
	if flip.JustMuted {
		muteMsg := fmt.Sprintf(
			"🔇 flip mute: %s × %s — %d buy/sell cycles, silencing watch alerts for %s",
			tr.WalletLabel, sym, flip.Cycles, w.flipGuard.MuteFor().Round(time.Second),
		)
		log.Printf("[watch] %s", muteMsg)
		_ = w.events.Append(store.Event{
			Type:        "flip_mute",
			Chain:       chainKey,
			Token:       tr.Token.Hex(),
			Symbol:      sym,
			Wallet:      tr.Wallet.Hex(),
			WalletLabel: tr.WalletLabel,
			Detail:      fmt.Sprintf("cycles=%d mute=%s", flip.Cycles, w.flipGuard.MuteFor()),
		})
		_ = w.notify.Send(ctx, muteMsg)
	}
	if err := w.log.Append(chainKey, tr, info); err != nil {
		return err
	}
	if tr.Side == "sell" && w.onSell != nil {
		if err := w.onSell.OnWalletSell(ctx, tr, info); err != nil {
			log.Printf("exit on sell %s: %v", tr.Token.Hex(), err)
		}
	}
	copyAction, copyReason, copySize := w.tryCopy(ctx, tr, info, effectiveUsd)
	d := store.TradeDecisionFrom(tr, info, chainKey, tradeUsd, effectiveUsd, batchLegs)
	d.AlertAction = "follow"
	d.CopyAction = copyAction
	d.CopyReason = copyReason
	d.CopySizeUsd = copySize
	w.saveDecision(ctx, d)
	return nil
}

// resolveBuySize returns USD for alert/copy filters. When pending is true, pendingAction
// is "pending" (batch accumulating) or "skip" (batch expired).
func (w *Watcher) resolveBuySize(tr parse.Trade, info enrich.TokenInfo, tradeUsd float64, clusters map[common.Address]store.TxClusterBuy) (effectiveUsd float64, batchLegs int, pending bool, pendingAction, pendingReason string) {
	effectiveUsd = tradeUsd
	if tr.Side != "buy" || tradeUsd <= 0 {
		return effectiveUsd, 0, false, "", ""
	}

	minBuy := w.rules.MinBuyUsd
	if minBuy <= 0 || tradeUsd >= minBuy {
		if w.batchBuy != nil {
			w.batchBuy.Clear(string(w.chainCfg.ID), tr.Wallet.Hex(), tr.Token.Hex())
		}
		return effectiveUsd, 0, false, "", ""
	}

	if clusters != nil {
		if c, ok := clusters[tr.Token]; ok && c.Legs >= 2 {
			if c.TotalUsd >= minBuy {
				if w.batchBuy != nil {
					w.batchBuy.Clear(string(w.chainCfg.ID), tr.Wallet.Hex(), tr.Token.Hex())
				}
				return c.TotalUsd, c.Legs, false, "", ""
			}
			reason := fmt.Sprintf("cluster batch $%.0f/$%.0f (%d wallets)", c.TotalUsd, minBuy, c.Legs)
			return c.TotalUsd, c.Legs, true, "skip", reason
		}
	}

	if w.batchBuy == nil {
		return effectiveUsd, 0, false, "", ""
	}

	window := time.Duration(w.rules.BatchWindowSec()) * time.Second
	maxLegs := w.rules.BatchMaxLegs()
	r := w.batchBuy.Add(
		string(w.chainCfg.ID),
		tr.Token.Hex(),
		tr.Wallet.Hex(),
		tr.WalletLabel,
		tradeUsd,
		tr.TxHash.Hex(),
		window,
		maxLegs,
		minBuy,
		time.Now(),
	)

	if r.Expired {
		reason := fmt.Sprintf("batch expired (%d legs $%.0f, max %d)", r.Legs, r.TotalUsd, maxLegs)
		log.Printf("skip alert | %s | %s", reason, w.formatTradeLine(tr, info, tradeUsd))
		return 0, r.Legs, true, "skip", reason
	}
	if !r.ShouldFire {
		reason := fmt.Sprintf("batch $%.0f/$%.0f (%d legs)", r.TotalUsd, minBuy, r.Legs)
		log.Printf("skip alert | %s | %s", reason, w.formatTradeLine(tr, info, tradeUsd))
		return r.TotalUsd, r.Legs, true, "pending", reason
	}

	return r.TotalUsd, r.Legs, false, "", ""
}

func batchDetail(legs int, effectiveUsd, legUsd float64) string {
	if legs <= 1 {
		return ""
	}
	return fmt.Sprintf("batch=%d total=$%.0f leg=$%.0f", legs, effectiveUsd, legUsd)
}

func (w *Watcher) tryCopy(ctx context.Context, tr parse.Trade, info enrich.TokenInfo, tradeUsd float64) (action, reason string, sizeUsd float64) {
	if w.executor == nil || !w.copyWallets[tr.Wallet] {
		return "na", "", 0
	}
	if tr.Side == "sell" && !w.execCfg.CopySell {
		return "skip", "copySell disabled", 0
	}
	if tr.Side == "buy" && w.execCfg.MaxOpenPositions > 0 && w.positions != nil {
		if n := w.positions.CountOpen(); n >= w.execCfg.MaxOpenPositions {
			reason = fmt.Sprintf("%d open positions (max %d)", n, w.execCfg.MaxOpenPositions)
			log.Printf("copy blocked | %s | %s", reason, w.formatTradeLine(tr, info, tradeUsd))
			return "skip", reason, 0
		}
	}

	if ok, blockReason := w.execCfg.ExecRules.PassesExecute(tr.Side, tradeUsd, info); !ok {
		log.Printf("copy blocked | %s | %s", blockReason, w.formatTradeLine(tr, info, tradeUsd))
		return "skip", blockReason, 0
	}

	chainKey := string(w.chainCfg.ID)
	walletKey := tr.Wallet.Hex()
	tokenKey := tr.Token.Hex()
	if tr.Side == "buy" && w.flipGuard != nil && w.flipGuard.RecentlySold(chainKey, walletKey, tokenKey, time.Now().UTC()) {
		blockReason := "recent flip sell by source wallet"
		log.Printf("copy blocked | %s | %s", blockReason, w.formatTradeLine(tr, info, tradeUsd))
		return "skip", blockReason, 0
	}
	if w.execCfg.ExecRules.FirstBuyOnly && w.seen != nil {
		global := w.execCfg.ExecRules.DedupeAcrossWallets
		if w.seen.SeenCopy(chainKey, walletKey, tokenKey, global) {
			if global {
				blockReason := fmt.Sprintf("token already copied on %s", chainKey)
				log.Printf("copy blocked | %s | %s", blockReason, w.formatTradeLine(tr, info, tradeUsd))
				return "skip", blockReason, 0
			}
			log.Printf("copy blocked | not first buy | %s", w.formatTradeLine(tr, info, tradeUsd))
			return "skip", "not first buy", 0
		}
	}

	if w.safety != nil {
		result, err := w.safety.Check(ctx, w.chainCfg, tr.Token)
		if err != nil {
			blockReason := fmt.Sprintf("safety check failed: %v", err)
			log.Printf("copy blocked | %s | %s", blockReason, w.formatTradeLine(tr, info, tradeUsd))
			return "skip", blockReason, 0
		}
		if !result.OK {
			log.Printf("copy blocked | safety %s | %s", result.Reason, w.formatTradeLine(tr, info, tradeUsd))
			return "skip", result.Reason, 0
		}
	}

	if tr.Side == "buy" {
		if enrich.OneWayChart(info) {
			reason := "pair has buys but no sells"
			log.Printf("copy blocked | %s | %s", reason, w.formatTradeLine(tr, info, tradeUsd))
			return "skip", reason, 0
		}
		hub := common.HexToAddress(info.QuoteTokenAddress)
		if w.chainCfg.IsExecutionQuote(hub) || hub == tr.Token {
			hub = common.Address{}
		}
		router := w.execCfg.V2Router
		if router == (common.Address{}) {
			router = w.chainCfg.V2Router
		}
		if err := execute.ProbeSellQuoteWithHub(ctx, w.client, w.chainCfg, router, tr.Token, execute.PreferredQuote(w.chainCfg, info), hub, info.Decimals); err != nil {
			blockReason := "sell probe: " + err.Error()
			log.Printf("copy blocked | %s | %s", blockReason, w.formatTradeLine(tr, info, tradeUsd))
			return "skip", blockReason, 0
		}
		wallet := w.execCfg.WalletAddress
		if err := execute.ProbeSellGas(ctx, w.client, w.chainCfg, w.execCfg, wallet, tr.Token, execute.PreferredQuote(w.chainCfg, info), info.Decimals); err != nil {
			blockReason := err.Error()
			log.Printf("copy blocked | %s | %s", blockReason, w.formatTradeLine(tr, info, tradeUsd))
			return "skip", blockReason, 0
		}
	}

	sizeUsd = copySizeUsd(tradeUsd, w.execCfg)
	preferred := execute.PreferredQuote(w.chainCfg, info)
	quoteToken := preferred
	if tr.Side == "buy" {
		if qt, err := execute.ResolveCopyQuoteToken(ctx, w.client, w.chainCfg, tr.Token, preferred, sizeUsd, w.nativeUSD(ctx), info); err == nil {
			quoteToken = qt
		}
	}
	req := execute.Request{
		SourceWallet: tr.Wallet,
		SourceLabel:  tr.WalletLabel,
		Trade:        tr,
		Token:        info,
		Chain:        chainKey,
		SizeUsd:      sizeUsd,
		QuoteToken:   quoteToken,
		HubToken:     common.HexToAddress(hubTokenHex(info, quoteToken)),
	}
	hash, err := w.executor.Mirror(ctx, req)
	if err != nil {
		blockReason := err.Error()
		log.Printf("copy failed | %v | %s", err, w.formatTradeLine(tr, info, tradeUsd))
		return "skip", blockReason, 0
	}
	if w.execCfg.SimulateSwaps {
		w.markCopySeen(chainKey, walletKey, tokenKey)
		return "follow", "simulated", sizeUsd
	}
	if w.client == nil {
		w.markCopySeen(chainKey, walletKey, tokenKey)
		execWallet := w.execCfg.WalletAddress.Hex()
		_ = w.recordPosition(tr, info, sizeUsd, quoteToken.Hex(), hubTokenHex(info, quoteToken), execWallet, "dry-run")
		return "follow", "", sizeUsd
	}
	if hash == (common.Hash{}) {
		blockReason := "copy returned no transaction"
		log.Printf("copy failed | %s | %s", blockReason, w.formatTradeLine(tr, info, tradeUsd))
		return "skip", blockReason, 0
	}
	receipt, err := execute.WaitReceipt(ctx, w.client, hash, 90*time.Second)
	if err != nil {
		blockReason := fmt.Sprintf("copy receipt: %v", err)
		log.Printf("copy failed | %s | %s", blockReason, w.formatTradeLine(tr, info, tradeUsd))
		return "skip", blockReason, 0
	}
	if receipt.Status == 0 {
		blockReason := "copy tx reverted"
		log.Printf("copy failed | %s | %s", blockReason, w.formatTradeLine(tr, info, tradeUsd))
		return "skip", blockReason, 0
	}
	execAddr, err := w.copySender(ctx, hash)
	if err != nil {
		blockReason := fmt.Sprintf("copy sender: %v", err)
		log.Printf("copy failed | %s | %s", blockReason, w.formatTradeLine(tr, info, tradeUsd))
		return "skip", blockReason, 0
	}
	if !w.recordPosition(tr, info, sizeUsd, quoteToken.Hex(), hubTokenHex(info, quoteToken), execAddr.Hex(), hash.Hex()) {
		return "follow", "", sizeUsd
	}
	w.markCopySeen(chainKey, walletKey, tokenKey)
	return "follow", "", sizeUsd
}

func (w *Watcher) markCopySeen(chainKey, walletKey, tokenKey string) {
	if w.execCfg.ExecRules.FirstBuyOnly && w.seen != nil {
		_ = w.seen.MarkCopy(chainKey, walletKey, tokenKey, w.execCfg.ExecRules.DedupeAcrossWallets)
	}
}

func hubTokenHex(info enrich.TokenInfo, quote common.Address) string {
	hub := common.HexToAddress(info.QuoteTokenAddress)
	if hub == (common.Address{}) || hub == quote {
		return ""
	}
	return hub.Hex()
}

func (w *Watcher) copySender(ctx context.Context, hash common.Hash) (common.Address, error) {
	tx, _, err := w.client.TransactionByHash(ctx, hash)
	if err != nil {
		return common.Address{}, err
	}
	signer := types.LatestSignerForChainID(big.NewInt(w.chainCfg.ChainID))
	return types.Sender(signer, tx)
}

func (w *Watcher) recordPosition(tr parse.Trade, info enrich.TokenInfo, sizeUsd float64, quoteToken, hubToken, execWallet, copyTx string) bool {
	if w.positions == nil || info.PriceUsd <= 0 {
		return false
	}
	pair := info.PairAddress
	if pair == "" && tr.Pair != (common.Address{}) {
		pair = tr.Pair.Hex()
	}
	dex := tr.DEX
	if dex == "" {
		dex = "uniswap-v2"
	}
	if quoteToken == "" {
		quoteToken = info.QuoteTokenAddress
	}
	entryLiq := info.Liquidity
	pos := store.Position{
		Chain:             string(w.chainCfg.ID),
		Token:             tr.Token.Hex(),
		TokenSymbol:       info.Symbol,
		TokenName:         info.Name,
		Pair:              pair,
		DEX:               dex,
		QuoteToken:        quoteToken,
		HubToken:          hubToken,
		SourceWallet:      tr.Wallet.Hex(),
		SourceLabel:       tr.WalletLabel,
		ExecWallet:        execWallet,
		EntryTx:           copyTx,
		EntryPriceUsd:     info.PriceUsd,
		EntrySizeUsd:      sizeUsd,
		EntryLiquidityUsd: entryLiq,
		LastLiquidityUsd:  entryLiq,
	}
	if err := w.positions.Open(pos); err != nil {
		log.Printf("[position] OPEN failed %s: %v", tr.Token.Hex(), err)
		return false
	}
	sym := info.Symbol
	if sym == "" {
		sym = tr.Token.Hex()
	}
	openN := w.positions.CountOpen()
	log.Printf("[position] OPEN %s %s $%.0f from %s wallet %s (open: %d) copy tx %s",
		w.chainCfg.ID, sym, sizeUsd, tr.WalletLabel, execWallet, openN, copyTx)
	_ = w.events.Append(store.Event{
		Type:        "position_open",
		Chain:       pos.Chain,
		Token:       pos.Token,
		Symbol:      sym,
		Wallet:      pos.SourceWallet,
		WalletLabel: pos.SourceLabel,
		SizeUsd:     sizeUsd,
		TxHash:      copyTx,
		Detail:      fmt.Sprintf("price=$%.6f liq=$%.0f open=%d exec=%s source_tx=%s", info.PriceUsd, entryLiq, openN, execWallet, tr.TxHash.Hex()),
	})
	return true
}

func copySizeUsd(tradeUsd float64, exec config.ExecutionConfig) float64 {
	if exec.CopyUsd > 0 {
		return exec.CopyUsd
	}
	if exec.MaxExecuteUsd > 0 {
		return exec.MaxExecuteUsd
	}
	if tradeUsd > 0 {
		return tradeUsd
	}
	if exec.MinExecuteUsd > 0 {
		return exec.MinExecuteUsd
	}
	return 100
}

func walletTopicHashes(watched map[common.Address]string) []common.Hash {
	topics := make([]common.Hash, 0, len(watched))
	for addr := range watched {
		topics = append(topics, common.BytesToHash(common.LeftPadBytes(addr.Bytes(), 32)))
	}
	return topics
}

type txDeduper struct {
	mu   sync.Mutex
	seen map[common.Hash]struct{}
}

func (d *txDeduper) mark(hash common.Hash) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.seen == nil {
		d.seen = make(map[common.Hash]struct{})
	}
	if _, ok := d.seen[hash]; ok {
		return false
	}
	d.seen[hash] = struct{}{}
	if len(d.seen) > 10_000 {
		d.seen = make(map[common.Hash]struct{})
	}
	return true
}

func (w *Watcher) nativeUSD(ctx context.Context) float64 {
	if w.nativeOracle == nil {
		return 0
	}
	price, err := w.nativeOracle.USD(ctx)
	if err != nil {
		return 0
	}
	return price
}

// formatTradeLine logs one full trade context per line (wallet, tx, block, spend).
func (w *Watcher) formatTradeLine(tr parse.Trade, info enrich.TokenInfo, tradeUsd float64) string {
	sym := info.Symbol
	if sym == "" {
		sym = tr.Token.Hex()
	}
	spent := formatQuoteSpent(tr, w.chainCfg)
	return fmt.Sprintf(
		"%s %s %s %s ~$%.0f spent=%s token=%s wallet=%s tx=%s block=%d log=%d",
		w.chainCfg.ID,
		tr.WalletLabel,
		tr.Side,
		sym,
		tradeUsd,
		spent,
		tr.Token.Hex(),
		tr.Wallet.Hex(),
		tr.TxHash.Hex(),
		tr.BlockNumber,
		tr.LogIndex,
	)
}

func formatQuoteSpent(tr parse.Trade, chainCfg chain.Config) string {
	if tr.QuoteAmount == nil || tr.QuoteAmount.Sign() == 0 {
		return "?"
	}
	decimals := 18
	if tr.QuoteToken != (common.Address{}) {
		if q, ok := chainCfg.QuoteTokens[tr.QuoteToken]; ok {
			decimals = q.Decimals
		}
	}
	symbol := tr.QuoteSymbol
	if symbol == "" {
		symbol = "quote"
	}
	return parse.FormatAmount(tr.QuoteAmount, decimals) + " " + symbol
}
