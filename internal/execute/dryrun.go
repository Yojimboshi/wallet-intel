package execute

import (
	"context"
	"log"
	"sync"

	"github.com/Yojimboshi/wallet-intel/internal/config"
	"github.com/ethereum/go-ethereum/common"
)

// DryRun logs copy intent and records requests without broadcasting transactions.
type DryRun struct {
	cfg      config.ExecutionConfig
	chainID  string
	mu       sync.Mutex
	Requests []Request
}

func NewDryRun(cfg config.ExecutionConfig, chainID string) *DryRun {
	return &DryRun{cfg: cfg, chainID: chainID}
}

func (d *DryRun) Mirror(ctx context.Context, req Request) (common.Hash, error) {
	if req.SizeUsd <= 0 {
		return common.Hash{}, errString("execution amount must be positive")
	}
	d.mu.Lock()
	d.Requests = append(d.Requests, req)
	d.mu.Unlock()
	log.Printf(
		"DRY-RUN COPY [%s] %s %s ~$%.0f token %s src %s",
		d.chainID,
		req.Trade.Side,
		req.Token.Symbol,
		req.SizeUsd,
		req.Trade.Token.Hex(),
		req.SourceLabel,
	)
	return common.Hash{}, nil
}

func (d *DryRun) Count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.Requests)
}

type errString string

func (e errString) Error() string { return string(e) }
