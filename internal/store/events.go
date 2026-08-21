package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Event is a single audit line in data/events.jsonl.
type Event struct {
	Timestamp   time.Time `json:"timestamp"`
	Type        string    `json:"type"`
	Chain       string    `json:"chain,omitempty"`
	Token       string    `json:"token,omitempty"`
	Symbol      string    `json:"symbol,omitempty"`
	Side        string    `json:"side,omitempty"`
	Wallet      string    `json:"wallet,omitempty"`
	WalletLabel string    `json:"walletLabel,omitempty"`
	Reason      string    `json:"reason,omitempty"`
	SizeUsd     float64   `json:"sizeUsd,omitempty"`
	PnLPct      float64   `json:"pnlPct,omitempty"`
	TxHash      string    `json:"txHash,omitempty"`
	Detail      string    `json:"detail,omitempty"`
}

type EventLog struct {
	path  string
	mysql *MySQL
}

func NewEventLog(path string) (*EventLog, error) {
	if path == "" {
		path = "data/events.jsonl"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create event log dir: %w", err)
	}
	return &EventLog{path: path}, nil
}

func (e *EventLog) UseMySQL(db *MySQL) {
	e.mysql = db
}

func (e *EventLog) Append(ev Event) error {
	if e == nil {
		return nil
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	line, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(e.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	if err != nil {
		return err
	}
	if e.mysql != nil {
		if err := e.mysql.InsertEvent(context.Background(), ev); err != nil {
			return fmt.Errorf("mysql event: %w", err)
		}
	}
	return nil
}
