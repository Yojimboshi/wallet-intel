package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// SeenTokens tracks first-seen (chain, wallet, token) for copy-first-buy-only policy.
type SeenTokens struct {
	path  string
	mu    sync.Mutex
	seen  map[string]struct{}
	mysql *MySQL
}

func NewSeenTokens(path string) (*SeenTokens, error) {
	if path == "" {
		path = "data/seen-tokens.json"
	}
	s := &SeenTokens{
		path: path,
		seen: make(map[string]struct{}),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *SeenTokens) UseMySQL(db *MySQL) error {
	s.mysql = db
	if db == nil {
		return nil
	}
	keys, err := db.LoadSeenKeys(context.Background())
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}
	s.mu.Lock()
	for _, k := range keys {
		s.seen[k] = struct{}{}
	}
	err = s.persistLocked()
	s.mu.Unlock()
	return err
}

func (s *SeenTokens) SeenCopy(chain, wallet, token string, global bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.seen[copyKey(chain, wallet, token, global)]
	return ok
}

func (s *SeenTokens) MarkCopy(chain, wallet, token string, global bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := copyKey(chain, wallet, token, global)
	if _, ok := s.seen[k]; ok {
		return nil
	}
	s.seen[k] = struct{}{}
	if err := s.persistLocked(); err != nil {
		return err
	}
	if s.mysql != nil {
		if err := s.mysql.MarkSeen(context.Background(), k); err != nil {
			return fmt.Errorf("mysql seen: %w", err)
		}
	}
	return nil
}

func copyKey(chain, wallet, token string, global bool) string {
	if global {
		return strings.ToLower(chain) + ":token:" + strings.ToLower(token)
	}
	return key(chain, wallet, token)
}

func key(chain, wallet, token string) string {
	return strings.ToLower(chain) + ":" + strings.ToLower(wallet) + ":" + strings.ToLower(token)
}

func (s *SeenTokens) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var keys []string
	if err := json.Unmarshal(data, &keys); err != nil {
		return fmt.Errorf("parse %s: %w", s.path, err)
	}
	for _, k := range keys {
		s.seen[k] = struct{}{}
	}
	return nil
}

func (s *SeenTokens) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	keys := make([]string, 0, len(s.seen))
	for k := range s.seen {
		keys = append(keys, k)
	}
	data, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}
