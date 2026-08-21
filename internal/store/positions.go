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

type PositionStatus string

const (
	PositionOpen       PositionStatus = "open"
	PositionClosed     PositionStatus = "closed"
	PositionManualExit PositionStatus = "manual_exit"
)

func positionIsActive(status PositionStatus) bool {
	return status == PositionOpen || status == PositionManualExit
}

// Position is an open copy-trade we are managing exits for.
type Position struct {
	Chain              string         `json:"chain"`
	Token              string         `json:"token"`
	TokenSymbol        string         `json:"tokenSymbol"`
	TokenName          string         `json:"tokenName,omitempty"`
	Pair               string         `json:"pair,omitempty"`
	DEX                string         `json:"dex,omitempty"`
	QuoteToken         string         `json:"quoteToken,omitempty"`
	HubToken           string         `json:"hubToken,omitempty"`
	SourceWallet       string         `json:"sourceWallet"`
	SourceLabel        string         `json:"sourceLabel"`
	ExecWallet         string         `json:"execWallet"`
	EntryTx            string         `json:"entryTx"`
	EntryPriceUsd      float64        `json:"entryPriceUsd"`
	EntrySizeUsd       float64        `json:"entrySizeUsd"`
	EntryLiquidityUsd  float64        `json:"entryLiquidityUsd"`
	LastLiquidityUsd   float64        `json:"lastLiquidityUsd"`
	TP1Taken           bool           `json:"tp1Taken,omitempty"`
	TP2Taken           bool           `json:"tp2Taken,omitempty"`
	OpenedAt           time.Time      `json:"openedAt"`
	Status             PositionStatus `json:"status"`
	ExitReason         string         `json:"exitReason,omitempty"`
	ClosedAt           *time.Time     `json:"closedAt,omitempty"`
}

type Positions struct {
	path  string
	mu    sync.Mutex
	list  []Position
	mysql *MySQL
}

func NewPositions(path string) (*Positions, error) {
	if path == "" {
		path = "data/positions.json"
	}
	p := &Positions{path: path}
	if err := p.load(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Positions) UseMySQL(db *MySQL) error {
	p.mysql = db
	if db == nil {
		return nil
	}
	ctx := context.Background()
	rows, err := db.LoadPositions(ctx)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	p.mu.Lock()
	p.list = rows
	err = p.persistLocked()
	p.mu.Unlock()
	return err
}

func (p *Positions) Open(pos Position) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := positionKey(pos.Chain, pos.Token)
	for _, existing := range p.list {
		if positionKey(existing.Chain, existing.Token) == key && positionIsActive(existing.Status) {
			return nil // already tracking
		}
	}
	pos.Status = PositionOpen
	if pos.OpenedAt.IsZero() {
		pos.OpenedAt = time.Now().UTC()
	}
	p.list = append(p.list, pos)
	if err := p.persistLocked(); err != nil {
		return err
	}
	if p.mysql != nil {
		if err := p.mysql.InsertPosition(context.Background(), pos); err != nil {
			return fmt.Errorf("mysql position open: %w", err)
		}
	}
	return nil
}

func (p *Positions) CountOpen() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, pos := range p.list {
		if positionIsActive(pos.Status) {
			n++
		}
	}
	return n
}

func (p *Positions) OpenList() []Position {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Position, 0)
	for _, pos := range p.list {
		if pos.Status == PositionOpen {
			out = append(out, pos)
		}
	}
	return out
}

func (p *Positions) ManualExitList() []Position {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Position, 0)
	for _, pos := range p.list {
		if pos.Status == PositionManualExit {
			out = append(out, pos)
		}
	}
	return out
}

func (p *Positions) FindOpen(chain, token string) (Position, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := positionKey(chain, token)
	for _, pos := range p.list {
		if pos.Status == PositionOpen && positionKey(pos.Chain, pos.Token) == key {
			return pos, true
		}
	}
	return Position{}, false
}

func (p *Positions) MarkManualExit(chain, token, reason string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := positionKey(chain, token)
	for i := range p.list {
		if p.list[i].Status == PositionOpen && positionKey(p.list[i].Chain, p.list[i].Token) == key {
			p.list[i].Status = PositionManualExit
			p.list[i].ExitReason = reason
			if err := p.persistLocked(); err != nil {
				return err
			}
			if p.mysql != nil {
				if err := p.mysql.MarkPositionManualExit(context.Background(), chain, token, reason); err != nil {
					return fmt.Errorf("mysql manual exit: %w", err)
				}
			}
			return nil
		}
	}
	return nil
}

func (p *Positions) UpdateLiquidity(chain, token string, liqUsd float64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := positionKey(chain, token)
	for i := range p.list {
		if p.list[i].Status == PositionOpen && positionKey(p.list[i].Chain, p.list[i].Token) == key {
			p.list[i].LastLiquidityUsd = liqUsd
			if err := p.persistLocked(); err != nil {
				return err
			}
			if p.mysql != nil {
				if err := p.mysql.UpdatePositionLiquidity(context.Background(), chain, token, liqUsd); err != nil {
					return fmt.Errorf("mysql position liq: %w", err)
				}
			}
			return nil
		}
	}
	return nil
}

func (p *Positions) MarkTPStage(chain, token string, stage int, reason string) error {
	if stage < 1 {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	key := positionKey(chain, token)
	for i := range p.list {
		if p.list[i].Status == PositionOpen && positionKey(p.list[i].Chain, p.list[i].Token) == key {
			if stage >= 1 {
				p.list[i].TP1Taken = true
			}
			if stage >= 2 {
				p.list[i].TP2Taken = true
			}
			p.list[i].ExitReason = reason
			if err := p.persistLocked(); err != nil {
				return err
			}
			if p.mysql != nil {
				if err := p.mysql.MarkPositionTPStage(context.Background(), chain, token, stage, reason); err != nil {
					return fmt.Errorf("mysql tp stage: %w", err)
				}
			}
			return nil
		}
	}
	return nil
}

// MarkTP1Taken records completion of staged TP1 (legacy alias).
func (p *Positions) MarkTP1Taken(chain, token, reason string) error {
	return p.MarkTPStage(chain, token, 1, reason)
}

func (p *Positions) Close(chain, token, reason string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := positionKey(chain, token)
	for i := range p.list {
		if positionIsActive(p.list[i].Status) && positionKey(p.list[i].Chain, p.list[i].Token) == key {
			now := time.Now().UTC()
			p.list[i].Status = PositionClosed
			p.list[i].ExitReason = reason
			p.list[i].ClosedAt = &now
			if err := p.persistLocked(); err != nil {
				return err
			}
			if p.mysql != nil {
				if err := p.mysql.ClosePosition(context.Background(), chain, token, reason, now); err != nil {
					return fmt.Errorf("mysql position close: %w", err)
				}
			}
			return nil
		}
	}
	return nil
}

func (p *Positions) RecordManualIntervention(row ManualIntervention) error {
	if p.mysql == nil {
		return nil
	}
	return p.mysql.InsertManualIntervention(context.Background(), row)
}

func (p *Positions) ResolveManualIntervention(chain, token string) error {
	if p.mysql == nil {
		return nil
	}
	return p.mysql.ResolveManualIntervention(context.Background(), chain, token)
}

func positionKey(chain, token string) string {
	return strings.ToLower(chain) + ":" + strings.ToLower(token)
}

func (p *Positions) load() error {
	data, err := os.ReadFile(p.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := json.Unmarshal(data, &p.list); err != nil {
		return fmt.Errorf("parse %s: %w", p.path, err)
	}
	return nil
}

func (p *Positions) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(p.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p.list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.path, data, 0o600)
}
