package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// HodlPosition is a discretionary long-hold — no exit monitor, tracked separately from copy trades.
type HodlPosition struct {
	Chain         string    `json:"chain"`
	Token         string    `json:"token"`
	TokenSymbol   string    `json:"tokenSymbol"`
	TokenName     string    `json:"tokenName,omitempty"`
	EntryTx       string    `json:"entryTx"`
	EntryPriceUsd float64   `json:"entryPriceUsd"`
	EntrySizeUsd  float64   `json:"entrySizeUsd"`
	ExecWallet    string    `json:"execWallet,omitempty"`
	Notes         string    `json:"notes,omitempty"`
	OpenedAt      time.Time `json:"openedAt"`
}

type HodlBook struct {
	path  string
	mu    sync.Mutex
	list  []HodlPosition
	mysql *MySQL
}

func NewHodlBook(path string) (*HodlBook, error) {
	if path == "" {
		path = "data/hodl.json"
	}
	h := &HodlBook{path: path}
	if err := h.load(); err != nil {
		return nil, err
	}
	return h, nil
}

func (h *HodlBook) UseMySQL(db *MySQL) {
	h.mysql = db
}

func (h *HodlBook) Add(pos HodlPosition) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := hodlKey(pos.Chain, pos.Token)
	for _, existing := range h.list {
		if hodlKey(existing.Chain, existing.Token) == key {
			return nil
		}
	}
	if pos.OpenedAt.IsZero() {
		pos.OpenedAt = time.Now().UTC()
	}
	h.list = append(h.list, pos)
	if err := h.persistLocked(); err != nil {
		return err
	}
	if h.mysql != nil {
		if err := h.mysql.InsertHodlPosition(context.Background(), pos); err != nil {
			return fmt.Errorf("mysql hodl: %w", err)
		}
	}
	return nil
}

func (h *HodlBook) List() []HodlPosition {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]HodlPosition, len(h.list))
	copy(out, h.list)
	return out
}

func (h *HodlBook) Has(chain, token string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := hodlKey(chain, token)
	for _, pos := range h.list {
		if hodlKey(pos.Chain, pos.Token) == key {
			return true
		}
	}
	return false
}

func hodlKey(chain, token string) string {
	return strings.ToLower(chain) + ":" + strings.ToLower(token)
}

func (h *HodlBook) load() error {
	data, err := os.ReadFile(h.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return json.Unmarshal(data, &h.list)
}

func (h *HodlBook) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(h.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(h.list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(h.path, data, 0o600)
}
