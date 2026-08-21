package store

import (
	"context"
	"database/sql"
	"fmt"
)

func (m *MySQL) SaveNativePrice(ctx context.Context, chain string, priceUsd float64) error {
	if m == nil || priceUsd <= 0 {
		return nil
	}
	_, err := m.db.ExecContext(ctx, `
		INSERT INTO native_prices (chain, price_usd, updated_at)
		VALUES (?, ?, ?)
		ON DUPLICATE KEY UPDATE price_usd = VALUES(price_usd), updated_at = VALUES(updated_at)`,
		normChain(chain), priceUsd, m.now(),
	)
	return err
}

func (m *MySQL) GetNativePrice(ctx context.Context, chain string) (float64, error) {
	if m == nil {
		return 0, fmt.Errorf("mysql not configured")
	}
	var price float64
	err := m.db.QueryRowContext(ctx,
		`SELECT price_usd FROM native_prices WHERE chain = ?`, normChain(chain),
	).Scan(&price)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("no native price for %s", chain)
	}
	if err != nil {
		return 0, err
	}
	if price <= 0 {
		return 0, fmt.Errorf("invalid native price for %s", chain)
	}
	return price, nil
}
