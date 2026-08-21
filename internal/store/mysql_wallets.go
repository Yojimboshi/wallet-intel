package store

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Yojimboshi/wallet-intel/internal/config"
)

func (m *MySQL) InsertTradeDecision(ctx context.Context, d TradeDecision) error {
	if m == nil {
		return nil
	}
	ts := d.Timestamp
	if ts.IsZero() {
		ts = m.now()
	} else {
		ts = ts.In(m.loc)
	}
	_, err := m.db.ExecContext(ctx, `
		INSERT INTO trade_decisions (
			ts, chain, wallet, wallet_label, side, token, token_symbol,
			trade_usd, effective_usd, batch_legs, market_cap_usd, liquidity_usd,
			tx_hash, block_number, alert_action, alert_reason,
			copy_action, copy_reason, copy_size_usd
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ts, normChain(d.Chain), normAddr(d.Wallet), d.WalletLabel, d.Side, normAddr(d.Token), d.TokenSymbol,
		d.TradeUsd, d.EffectiveUsd, d.BatchLegs, d.MarketCapUsd, d.LiquidityUsd,
		d.TxHash, d.BlockNumber, d.AlertAction, d.AlertReason,
		d.CopyAction, d.CopyReason, d.CopySizeUsd,
	)
	return err
}

func (m *MySQL) SyncWatchedWallets(ctx context.Context, wallets []config.WatchedWallet) error {
	if m == nil {
		return nil
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM watched_wallets`); err != nil {
		return err
	}
	now := m.now()
	for _, w := range wallets {
		chains := chainList(w.Chains)
		chainsJSON, err := json.Marshal(chains)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO watched_wallets (address, label, chains, copy_flag, synced_at)
			VALUES (?, ?, ?, ?, ?)`,
			normAddr(w.Address.Hex()), w.Label, string(chainsJSON), boolToTiny(w.Copy), now,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (m *MySQL) SyncExecutionWallets(ctx context.Context, file config.ExecutionWalletsFile, activeLabel string) error {
	if m == nil {
		return nil
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM execution_wallets`); err != nil {
		return err
	}
	now := m.now()
	active := strings.TrimSpace(activeLabel)

	insert := func(network string, list []config.ExecutionWallet) error {
		for _, w := range list {
			addr := strings.TrimSpace(w.Address)
			if addr == "" {
				continue
			}
			isActive := active != "" && strings.EqualFold(w.Label, active)
			storeAddr := addr
			if network == "evm" {
				storeAddr = normAddr(addr)
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO execution_wallets (label, address, network, is_active, synced_at)
				VALUES (?, ?, ?, ?, ?)`,
				w.Label, storeAddr, network, boolToTiny(isActive), now,
			); err != nil {
				return err
			}
		}
		return nil
	}
	if err := insert("evm", file.EVM); err != nil {
		return err
	}
	if err := insert("svm", file.SVM); err != nil {
		return err
	}
	return tx.Commit()
}

func chainList(chains map[string]struct{}) []string {
	out := make([]string, 0, len(chains))
	for c := range chains {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

func boolToTiny(v bool) int {
	if v {
		return 1
	}
	return 0
}

// TableCounts returns row counts for main tables (for dbinit status).
func (m *MySQL) TableCounts(ctx context.Context) (map[string]int64, error) {
	if m == nil {
		return nil, fmt.Errorf("mysql not configured")
	}
	tables := []string{
		"watched_wallets", "execution_wallets", "trade_decisions",
		"trades", "events", "positions", "seen_tokens", "native_prices",
	}
	out := make(map[string]int64, len(tables))
	for _, t := range tables {
		var n int64
		if err := m.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+t).Scan(&n); err != nil {
			return nil, fmt.Errorf("count %s: %w", t, err)
		}
		out[t] = n
	}
	return out, nil
}
