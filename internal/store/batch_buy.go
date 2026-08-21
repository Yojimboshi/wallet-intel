package store

import (
	"strings"
	"sync"
	"time"
)

// BatchBuyLeg is one watched-wallet buy contributing to a token batch.
type BatchBuyLeg struct {
	Wallet    string
	Label     string
	TradeUsd  float64
	TxHash    string
	At        time.Time
}

// BatchBuyResult describes batch state after recording a leg.
type BatchBuyResult struct {
	TotalUsd   float64
	Legs       int
	ShouldFire bool // cumulative total crossed minBuyUsd
	Expired    bool // max legs reached without crossing threshold
}

// BatchBuyTracker sums recent buys of the same token by one wallet.
type BatchBuyTracker struct {
	mu      sync.Mutex
	batches map[string]*batchBuyState
}

type batchBuyState struct {
	startedAt time.Time
	totalUsd  float64
	legs      []BatchBuyLeg
	fired     bool
}

func NewBatchBuyTracker() *BatchBuyTracker {
	return &BatchBuyTracker{batches: make(map[string]*batchBuyState)}
}

func batchKey(chain, wallet, token string) string {
	return strings.ToLower(chain) + ":" + strings.ToLower(wallet) + ":" + strings.ToLower(token)
}

// Add records a sub-threshold buy leg. Returns ShouldFire when cumulative size >= minBuyUsd.
func (b *BatchBuyTracker) Add(
	chain, token, wallet, label string,
	tradeUsd float64,
	txHash string,
	window time.Duration,
	maxLegs int,
	minBuyUsd float64,
	now time.Time,
) BatchBuyResult {
	if b == nil || tradeUsd <= 0 || minBuyUsd <= 0 {
		return BatchBuyResult{}
	}
	if window <= 0 {
		window = 120 * time.Second
	}
	if maxLegs <= 0 {
		maxLegs = 5
	}

	key := batchKey(chain, wallet, token)

	b.mu.Lock()
	defer b.mu.Unlock()

	b.pruneLocked(now, window)

	st, ok := b.batches[key]
	if !ok || st.fired || now.Sub(st.startedAt) > window {
		st = &batchBuyState{startedAt: now}
		b.batches[key] = st
	}

	if st.fired {
		return BatchBuyResult{TotalUsd: st.totalUsd, Legs: len(st.legs)}
	}

	st.legs = append(st.legs, BatchBuyLeg{
		Wallet:   wallet,
		Label:    label,
		TradeUsd: tradeUsd,
		TxHash:   txHash,
		At:       now,
	})
	st.totalUsd += tradeUsd

	if len(st.legs) > maxLegs {
		delete(b.batches, key)
		return BatchBuyResult{TotalUsd: st.totalUsd, Legs: len(st.legs), Expired: true}
	}

	if st.totalUsd >= minBuyUsd {
		st.fired = true
		return BatchBuyResult{
			TotalUsd:   st.totalUsd,
			Legs:       len(st.legs),
			ShouldFire: true,
		}
	}

	return BatchBuyResult{
		TotalUsd: st.totalUsd,
		Legs:     len(st.legs),
	}
}

// Clear drops an in-progress batch for one wallet (e.g. after a single large buy).
func (b *BatchBuyTracker) Clear(chain, wallet, token string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	delete(b.batches, batchKey(chain, wallet, token))
	b.mu.Unlock()
}

func (b *BatchBuyTracker) pruneLocked(now time.Time, window time.Duration) {
	for key, st := range b.batches {
		if now.Sub(st.startedAt) > window {
			delete(b.batches, key)
		}
	}
}
