package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// MySQL persists trades, events, positions, and seen-token keys.
type MySQL struct {
	db  *sql.DB
	loc *time.Location
}

func OpenMySQL(dsn string, loc *time.Location) (*MySQL, error) {
	if loc == nil {
		loc = time.Local
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("mysql ping: %w", err)
	}
	m := &MySQL{db: db, loc: loc}
	if err := m.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return m, nil
}

func (m *MySQL) now() time.Time {
	if m == nil || m.loc == nil {
		return time.Now()
	}
	return time.Now().In(m.loc)
}

func (m *MySQL) Close() error {
	if m == nil || m.db == nil {
		return nil
	}
	return m.db.Close()
}

func (m *MySQL) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS trades (
			id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
			ts DATETIME(3) NOT NULL,
			chain VARCHAR(16) NOT NULL,
			wallet CHAR(42) NOT NULL,
			wallet_label VARCHAR(64) NOT NULL DEFAULT '',
			side VARCHAR(8) NOT NULL,
			token CHAR(42) NOT NULL,
			token_symbol VARCHAR(64) NOT NULL DEFAULT '',
			token_amount VARCHAR(78) NOT NULL DEFAULT '',
			quote_symbol VARCHAR(16) NOT NULL DEFAULT '',
			quote_amount VARCHAR(78) NOT NULL DEFAULT '',
			market_cap_usd DOUBLE NOT NULL DEFAULT 0,
			liquidity_usd DOUBLE NOT NULL DEFAULT 0,
			tx_hash CHAR(66) NOT NULL,
			block_number BIGINT UNSIGNED NOT NULL,
			dex_url VARCHAR(512) NOT NULL DEFAULT '',
			UNIQUE KEY uq_trades_tx (chain, tx_hash, token, side, wallet),
			KEY idx_trades_ts (ts),
			KEY idx_trades_token (chain, token)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS events (
			id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
			ts DATETIME(3) NOT NULL,
			type VARCHAR(32) NOT NULL,
			chain VARCHAR(16) NOT NULL DEFAULT '',
			token CHAR(42) NOT NULL DEFAULT '',
			symbol VARCHAR(64) NOT NULL DEFAULT '',
			side VARCHAR(8) NOT NULL DEFAULT '',
			wallet CHAR(42) NOT NULL DEFAULT '',
			wallet_label VARCHAR(64) NOT NULL DEFAULT '',
			reason VARCHAR(255) NOT NULL DEFAULT '',
			size_usd DOUBLE NOT NULL DEFAULT 0,
			pnl_pct DOUBLE NOT NULL DEFAULT 0,
			tx_hash CHAR(66) NOT NULL DEFAULT '',
			detail TEXT,
			KEY idx_events_ts (ts),
			KEY idx_events_type (type, chain),
			KEY idx_events_token (chain, token)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS positions (
			id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
			chain VARCHAR(16) NOT NULL,
			token CHAR(42) NOT NULL,
			token_symbol VARCHAR(64) NOT NULL DEFAULT '',
			token_name VARCHAR(128) NOT NULL DEFAULT '',
			pair CHAR(42) NOT NULL DEFAULT '',
			dex VARCHAR(32) NOT NULL DEFAULT '',
			source_wallet CHAR(42) NOT NULL,
			source_label VARCHAR(64) NOT NULL DEFAULT '',
			exec_wallet CHAR(42) NOT NULL DEFAULT '',
			entry_tx CHAR(66) NOT NULL,
			entry_price_usd DOUBLE NOT NULL,
			entry_size_usd DOUBLE NOT NULL,
			entry_liquidity_usd DOUBLE NOT NULL DEFAULT 0,
			last_liquidity_usd DOUBLE NOT NULL DEFAULT 0,
			tp1_taken TINYINT(1) NOT NULL DEFAULT 0,
			tp2_taken TINYINT(1) NOT NULL DEFAULT 0,
			opened_at DATETIME(3) NOT NULL,
			status ENUM('open','closed') NOT NULL DEFAULT 'open',
			exit_reason VARCHAR(255) NOT NULL DEFAULT '',
			closed_at DATETIME(3) NULL,
			KEY idx_positions_status (status, chain),
			KEY idx_positions_token (chain, token)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS seen_tokens (
			copy_key VARCHAR(128) PRIMARY KEY,
			seen_at DATETIME(3) NOT NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS watched_wallets (
			address CHAR(42) PRIMARY KEY,
			label VARCHAR(64) NOT NULL DEFAULT '',
			chains JSON NOT NULL,
			copy_flag TINYINT(1) NOT NULL DEFAULT 0,
			synced_at DATETIME(3) NOT NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS execution_wallets (
			label VARCHAR(64) NOT NULL,
			address VARCHAR(128) NOT NULL,
			network VARCHAR(8) NOT NULL,
			is_active TINYINT(1) NOT NULL DEFAULT 0,
			synced_at DATETIME(3) NOT NULL,
			PRIMARY KEY (network, label),
			UNIQUE KEY uq_exec_addr (network, address)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS trade_decisions (
			id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
			ts DATETIME(3) NOT NULL,
			chain VARCHAR(16) NOT NULL,
			wallet CHAR(42) NOT NULL,
			wallet_label VARCHAR(64) NOT NULL DEFAULT '',
			side VARCHAR(8) NOT NULL,
			token CHAR(42) NOT NULL,
			token_symbol VARCHAR(64) NOT NULL DEFAULT '',
			trade_usd DOUBLE NOT NULL DEFAULT 0,
			effective_usd DOUBLE NOT NULL DEFAULT 0,
			batch_legs INT NOT NULL DEFAULT 0,
			market_cap_usd DOUBLE NOT NULL DEFAULT 0,
			liquidity_usd DOUBLE NOT NULL DEFAULT 0,
			tx_hash CHAR(66) NOT NULL,
			block_number BIGINT UNSIGNED NOT NULL DEFAULT 0,
			alert_action VARCHAR(16) NOT NULL,
			alert_reason VARCHAR(255) NOT NULL DEFAULT '',
			copy_action VARCHAR(8) NOT NULL DEFAULT 'na',
			copy_reason VARCHAR(255) NOT NULL DEFAULT '',
			copy_size_usd DOUBLE NOT NULL DEFAULT 0,
			KEY idx_td_ts (ts),
			KEY idx_td_wallet (wallet),
			KEY idx_td_alert (alert_action),
			KEY idx_td_copy (copy_action),
			KEY idx_td_token (chain, token)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS native_prices (
			chain VARCHAR(16) PRIMARY KEY,
			price_usd DOUBLE NOT NULL,
			updated_at DATETIME(3) NOT NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS manual_interventions (
			id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
			chain VARCHAR(16) NOT NULL,
			token CHAR(42) NOT NULL,
			token_symbol VARCHAR(64) NOT NULL DEFAULT '',
			kind VARCHAR(32) NOT NULL DEFAULT 'sell',
			reason VARCHAR(255) NOT NULL DEFAULT '',
			detail TEXT,
			status ENUM('pending','resolved') NOT NULL DEFAULT 'pending',
			created_at DATETIME(3) NOT NULL,
			resolved_at DATETIME(3) NULL,
			KEY idx_manual_pending (status, chain),
			KEY idx_manual_token (chain, token)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS hodl_positions (
			id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
			chain VARCHAR(16) NOT NULL,
			token CHAR(42) NOT NULL,
			token_symbol VARCHAR(64) NOT NULL DEFAULT '',
			token_name VARCHAR(128) NOT NULL DEFAULT '',
			entry_tx CHAR(66) NOT NULL DEFAULT '',
			entry_price_usd DOUBLE NOT NULL DEFAULT 0,
			entry_size_usd DOUBLE NOT NULL DEFAULT 0,
			exec_wallet CHAR(42) NOT NULL DEFAULT '',
			notes VARCHAR(255) NOT NULL DEFAULT '',
			opened_at DATETIME(3) NOT NULL,
			UNIQUE KEY uq_hodl (chain, token),
			KEY idx_hodl_chain (chain)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	}
	for _, stmt := range stmts {
		if _, err := m.db.Exec(stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	// widen for svm addresses if table existed from an older schema
	_, _ = m.db.Exec(`ALTER TABLE execution_wallets MODIFY address VARCHAR(128) NOT NULL`)
	_, _ = m.db.Exec(`ALTER TABLE positions MODIFY status VARCHAR(16) NOT NULL DEFAULT 'open'`)
	_, _ = m.db.Exec(`ALTER TABLE positions ADD COLUMN token_name VARCHAR(128) NOT NULL DEFAULT '' AFTER token_symbol`)
	_, _ = m.db.Exec(`ALTER TABLE hodl_positions ADD COLUMN token_name VARCHAR(128) NOT NULL DEFAULT '' AFTER token_symbol`)
	_, _ = m.db.Exec(`ALTER TABLE positions ADD COLUMN tp1_taken TINYINT(1) NOT NULL DEFAULT 0 AFTER last_liquidity_usd`)
	_, _ = m.db.Exec(`ALTER TABLE positions ADD COLUMN tp2_taken TINYINT(1) NOT NULL DEFAULT 0 AFTER tp1_taken`)
	_, _ = m.db.Exec(`ALTER TABLE positions ADD COLUMN quote_token CHAR(42) NOT NULL DEFAULT '' AFTER dex`)
	_, _ = m.db.Exec(`ALTER TABLE positions ADD COLUMN hub_token CHAR(42) NOT NULL DEFAULT '' AFTER quote_token`)
	return nil
}

func normChain(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
func normAddr(s string) string  { return strings.ToLower(strings.TrimSpace(s)) }

func (m *MySQL) InsertTrade(ctx context.Context, rec TradeRecord) error {
	if m == nil {
		return nil
	}
	ts := rec.Timestamp
	if ts.IsZero() {
		ts = m.now()
	}
	_, err := m.db.ExecContext(ctx, `
		INSERT IGNORE INTO trades (
			ts, chain, wallet, wallet_label, side, token, token_symbol,
			token_amount, quote_symbol, quote_amount, market_cap_usd,
			liquidity_usd, tx_hash, block_number, dex_url
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ts, normChain(rec.Chain), normAddr(rec.Wallet), rec.WalletLabel, rec.Side, normAddr(rec.Token), rec.TokenSymbol,
		rec.TokenAmount, rec.QuoteSymbol, rec.QuoteAmount, rec.MarketCap, rec.Liquidity,
		rec.TxHash, rec.BlockNumber, rec.DexURL,
	)
	return err
}

func (m *MySQL) InsertEvent(ctx context.Context, ev Event) error {
	if m == nil {
		return nil
	}
	ts := ev.Timestamp
	if ts.IsZero() {
		ts = m.now()
	}
	_, err := m.db.ExecContext(ctx, `
		INSERT INTO events (
			ts, type, chain, token, symbol, side, wallet, wallet_label,
			reason, size_usd, pnl_pct, tx_hash, detail
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ts, ev.Type, normChain(ev.Chain), normAddr(ev.Token), ev.Symbol, ev.Side, normAddr(ev.Wallet), ev.WalletLabel,
		ev.Reason, ev.SizeUsd, ev.PnLPct, ev.TxHash, ev.Detail,
	)
	return err
}

func (m *MySQL) InsertPosition(ctx context.Context, pos Position) error {
	if m == nil {
		return nil
	}
	openedAt := pos.OpenedAt
	if openedAt.IsZero() {
		openedAt = m.now()
	}
	_, err := m.db.ExecContext(ctx, `
		INSERT INTO positions (
			chain, token, token_symbol, token_name, pair, dex, quote_token, hub_token, source_wallet, source_label,
			exec_wallet, entry_tx, entry_price_usd, entry_size_usd,
			entry_liquidity_usd, last_liquidity_usd, tp1_taken, tp2_taken, opened_at, status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		normChain(pos.Chain), normAddr(pos.Token), pos.TokenSymbol, pos.TokenName, normAddr(pos.Pair), pos.DEX, normAddr(pos.QuoteToken), normAddr(pos.HubToken),
		normAddr(pos.SourceWallet), pos.SourceLabel, normAddr(pos.ExecWallet), pos.EntryTx, pos.EntryPriceUsd, pos.EntrySizeUsd,
		pos.EntryLiquidityUsd, pos.LastLiquidityUsd, boolToTiny(pos.TP1Taken), boolToTiny(pos.TP2Taken), openedAt, pos.Status,
	)
	return err
}

func (m *MySQL) UpdatePositionLiquidity(ctx context.Context, chain, token string, liqUsd float64) error {
	if m == nil {
		return nil
	}
	_, err := m.db.ExecContext(ctx, `
		UPDATE positions SET last_liquidity_usd = ?
		WHERE status = 'open' AND chain = ? AND token = ?`,
		liqUsd, normChain(chain), normAddr(token),
	)
	return err
}

func (m *MySQL) MarkPositionTPStage(ctx context.Context, chain, token string, stage int, reason string) error {
	if m == nil || stage < 1 {
		return nil
	}
	tp1 := 1
	tp2 := 0
	if stage >= 2 {
		tp2 = 1
	}
	_, err := m.db.ExecContext(ctx, `
		UPDATE positions SET tp1_taken = ?, tp2_taken = ?, exit_reason = ?
		WHERE status = 'open' AND chain = ? AND token = ?`,
		tp1, tp2, reason, normChain(chain), normAddr(token),
	)
	return err
}

func (m *MySQL) ClosePosition(ctx context.Context, chain, token, reason string, closedAt time.Time) error {
	if m == nil {
		return nil
	}
	_, err := m.db.ExecContext(ctx, `
		UPDATE positions SET status = 'closed', exit_reason = ?, closed_at = ?
		WHERE status IN ('open', 'manual_exit') AND chain = ? AND token = ?`,
		reason, closedAt, normChain(chain), normAddr(token),
	)
	return err
}

func (m *MySQL) MarkPositionManualExit(ctx context.Context, chain, token, reason string) error {
	if m == nil {
		return nil
	}
	_, err := m.db.ExecContext(ctx, `
		UPDATE positions SET status = 'manual_exit', exit_reason = ?
		WHERE status = 'open' AND chain = ? AND token = ?`,
		reason, normChain(chain), normAddr(token),
	)
	return err
}

type ManualIntervention struct {
	Chain       string
	Token       string
	TokenSymbol string
	Kind        string
	Reason      string
	Detail      string
}

func (m *MySQL) InsertManualIntervention(ctx context.Context, row ManualIntervention) error {
	if m == nil {
		return nil
	}
	kind := row.Kind
	if kind == "" {
		kind = "sell"
	}
	_, err := m.db.ExecContext(ctx, `
		INSERT INTO manual_interventions (
			chain, token, token_symbol, kind, reason, detail, status, created_at
		) VALUES (?, ?, ?, ?, ?, ?, 'pending', ?)`,
		normChain(row.Chain), normAddr(row.Token), row.TokenSymbol, kind,
		row.Reason, row.Detail, m.now(),
	)
	return err
}

func (m *MySQL) ResolveManualIntervention(ctx context.Context, chain, token string) error {
	if m == nil {
		return nil
	}
	_, err := m.db.ExecContext(ctx, `
		UPDATE manual_interventions SET status = 'resolved', resolved_at = ?
		WHERE status = 'pending' AND chain = ? AND token = ?`,
		m.now(), normChain(chain), normAddr(token),
	)
	return err
}

func (m *MySQL) InsertHodlPosition(ctx context.Context, pos HodlPosition) error {
	if m == nil {
		return nil
	}
	_, err := m.db.ExecContext(ctx, `
		INSERT INTO hodl_positions (
			chain, token, token_symbol, token_name, entry_tx, entry_price_usd,
			entry_size_usd, exec_wallet, notes, opened_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			token_symbol = VALUES(token_symbol),
			token_name = VALUES(token_name),
			entry_tx = VALUES(entry_tx),
			entry_price_usd = VALUES(entry_price_usd),
			entry_size_usd = entry_size_usd + VALUES(entry_size_usd),
			exec_wallet = VALUES(exec_wallet),
			notes = VALUES(notes),
			opened_at = VALUES(opened_at)`,
		normChain(pos.Chain), normAddr(pos.Token), pos.TokenSymbol, pos.TokenName, pos.EntryTx,
		pos.EntryPriceUsd, pos.EntrySizeUsd, normAddr(pos.ExecWallet), pos.Notes, pos.OpenedAt,
	)
	return err
}

func (m *MySQL) LoadPositions(ctx context.Context) ([]Position, error) {
	if m == nil {
		return nil, nil
	}
	rows, err := m.db.QueryContext(ctx, `
		SELECT chain, token, token_symbol, token_name, pair, dex, quote_token, hub_token, source_wallet, source_label,
			exec_wallet, entry_tx, entry_price_usd, entry_size_usd,
			entry_liquidity_usd, last_liquidity_usd, tp1_taken, tp2_taken, opened_at, status, exit_reason, closed_at
		FROM positions ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Position
	for rows.Next() {
		var pos Position
		var closedAt sql.NullTime
		var tp1, tp2 int
		if err := rows.Scan(
			&pos.Chain, &pos.Token, &pos.TokenSymbol, &pos.TokenName, &pos.Pair, &pos.DEX, &pos.QuoteToken, &pos.HubToken,
			&pos.SourceWallet, &pos.SourceLabel, &pos.ExecWallet, &pos.EntryTx,
			&pos.EntryPriceUsd, &pos.EntrySizeUsd, &pos.EntryLiquidityUsd, &pos.LastLiquidityUsd,
			&tp1, &tp2, &pos.OpenedAt, &pos.Status, &pos.ExitReason, &closedAt,
		); err != nil {
			return nil, err
		}
		pos.TP1Taken = tp1 != 0
		pos.TP2Taken = tp2 != 0
		if closedAt.Valid {
			t := closedAt.Time
			pos.ClosedAt = &t
		}
		out = append(out, pos)
	}
	return out, rows.Err()
}

func (m *MySQL) MarkSeen(ctx context.Context, key string) error {
	if m == nil {
		return nil
	}
	_, err := m.db.ExecContext(ctx,
		`INSERT IGNORE INTO seen_tokens (copy_key, seen_at) VALUES (?, ?)`,
		key, m.now(),
	)
	return err
}

func (m *MySQL) LoadSeenKeys(ctx context.Context) ([]string, error) {
	if m == nil {
		return nil, nil
	}
	rows, err := m.db.QueryContext(ctx, `SELECT copy_key FROM seen_tokens`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}
