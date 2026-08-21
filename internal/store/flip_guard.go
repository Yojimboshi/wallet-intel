package store

import (
	"strings"
	"sync"
	"time"
)

// FlipGuard detects wallet+token buy→sell churn loops used to bait copiers.
//
// Copy: skip buy if this wallet sold the same token recently.
// Alerts: after N complete buy→sell cycles in a window, mute further TG watch alerts.
type FlipGuard struct {
	mu sync.Mutex

	recentSellBlock time.Duration
	cycleWindow     time.Duration
	muteAfter       int
	muteFor         time.Duration

	states map[string]*flipState
}

type flipState struct {
	lastBuy     time.Time
	lastSell    time.Time
	cycles      int
	windowStart time.Time
	mutedUntil  time.Time
}

// FlipObserve is the result of recording one watched trade.
type FlipObserve struct {
	Cycles     int
	Muted      bool // alert should be suppressed
	JustMuted  bool // crossed mute threshold on this sell
	RecentSell bool // buy after a recent sell — skip copy
}

type FlipGuardConfig struct {
	RecentSellBlock time.Duration
	CycleWindow     time.Duration
	MuteAfterCycles int
	MuteFor         time.Duration
}

func NewFlipGuard(cfg FlipGuardConfig) *FlipGuard {
	if cfg.RecentSellBlock <= 0 {
		cfg.RecentSellBlock = 15 * time.Minute
	}
	if cfg.CycleWindow <= 0 {
		cfg.CycleWindow = 30 * time.Minute
	}
	if cfg.MuteAfterCycles <= 0 {
		cfg.MuteAfterCycles = 2
	}
	if cfg.MuteFor <= 0 {
		cfg.MuteFor = 30 * time.Minute
	}
	return &FlipGuard{
		recentSellBlock: cfg.RecentSellBlock,
		cycleWindow:     cfg.CycleWindow,
		muteAfter:       cfg.MuteAfterCycles,
		muteFor:         cfg.MuteFor,
		states:          make(map[string]*flipState),
	}
}

func flipKey(chain, wallet, token string) string {
	return strings.ToLower(chain) + ":" + strings.ToLower(wallet) + ":" + strings.ToLower(token)
}

// Observe records a buy or sell and returns mute / recent-sell signals.
func (f *FlipGuard) Observe(chain, wallet, token, side string, now time.Time) FlipObserve {
	if f == nil {
		return FlipObserve{}
	}
	key := flipKey(chain, wallet, token)

	f.mu.Lock()
	defer f.mu.Unlock()

	st := f.states[key]
	if st == nil {
		st = &flipState{}
		f.states[key] = st
	}

	// Mute expired → reset cycle counter for a fresh window.
	if !st.mutedUntil.IsZero() && now.After(st.mutedUntil) {
		st.mutedUntil = time.Time{}
		st.cycles = 0
		st.windowStart = time.Time{}
	}

	out := FlipObserve{}

	switch side {
	case "buy":
		if !st.lastSell.IsZero() && now.Sub(st.lastSell) <= f.recentSellBlock {
			out.RecentSell = true
		}
		st.lastBuy = now
	case "sell":
		if !st.lastBuy.IsZero() {
			if st.windowStart.IsZero() || now.Sub(st.windowStart) > f.cycleWindow {
				st.windowStart = st.lastBuy
				st.cycles = 0
			}
			if now.Sub(st.lastBuy) <= f.cycleWindow {
				st.cycles++
				if st.cycles >= f.muteAfter && st.mutedUntil.IsZero() {
					st.mutedUntil = now.Add(f.muteFor)
					out.JustMuted = true
				}
			}
			st.lastBuy = time.Time{}
		}
		st.lastSell = now
	}

	out.Cycles = st.cycles
	if !st.mutedUntil.IsZero() && !now.After(st.mutedUntil) {
		out.Muted = true
	}
	return out
}

// RecentlySold reports whether this wallet sold the token within the recent-sell block window.
func (f *FlipGuard) RecentlySold(chain, wallet, token string, now time.Time) bool {
	if f == nil {
		return false
	}
	key := flipKey(chain, wallet, token)
	f.mu.Lock()
	defer f.mu.Unlock()
	st := f.states[key]
	if st == nil || st.lastSell.IsZero() {
		return false
	}
	return now.Sub(st.lastSell) <= f.recentSellBlock
}

func (f *FlipGuard) MuteAfterCycles() int {
	if f == nil {
		return 2
	}
	return f.muteAfter
}

func (f *FlipGuard) MuteFor() time.Duration {
	if f == nil {
		return 30 * time.Minute
	}
	return f.muteFor
}
