package store

import (
	"testing"
	"time"
)

func TestFlipGuard_RecentSellBlocksRebuy(t *testing.T) {
	g := NewFlipGuard(FlipGuardConfig{
		RecentSellBlock: 15 * time.Minute,
		CycleWindow:     30 * time.Minute,
		MuteAfterCycles: 2,
		MuteFor:         30 * time.Minute,
	})
	now := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)

	g.Observe("bsc", "0xw", "0xt", "buy", now)
	g.Observe("bsc", "0xw", "0xt", "sell", now.Add(5*time.Second))

	if !g.RecentlySold("bsc", "0xw", "0xt", now.Add(1*time.Minute)) {
		t.Fatal("expected recent sell within block window")
	}
	rebuy := g.Observe("bsc", "0xw", "0xt", "buy", now.Add(1*time.Minute))
	if !rebuy.RecentSell {
		t.Fatal("expected RecentSell on rebuy after flip")
	}
	if g.RecentlySold("bsc", "0xw", "0xt", now.Add(20*time.Minute)) {
		t.Fatal("recent sell should expire after block window")
	}
}

func TestFlipGuard_MuteAfterTwoCycles(t *testing.T) {
	g := NewFlipGuard(FlipGuardConfig{
		RecentSellBlock: 15 * time.Minute,
		CycleWindow:     30 * time.Minute,
		MuteAfterCycles: 2,
		MuteFor:         30 * time.Minute,
	})
	now := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)

	// cycle 1
	g.Observe("bsc", "0xw", "0xt", "buy", now)
	s1 := g.Observe("bsc", "0xw", "0xt", "sell", now.Add(10*time.Second))
	if s1.Cycles != 1 || s1.JustMuted || s1.Muted {
		t.Fatalf("cycle1: %+v", s1)
	}

	// cycle 2 → mute
	g.Observe("bsc", "0xw", "0xt", "buy", now.Add(1*time.Minute))
	s2 := g.Observe("bsc", "0xw", "0xt", "sell", now.Add(1*time.Minute+10*time.Second))
	if s2.Cycles != 2 || !s2.JustMuted || !s2.Muted {
		t.Fatalf("cycle2 mute: %+v", s2)
	}

	// further spam muted
	g.Observe("bsc", "0xw", "0xt", "buy", now.Add(2*time.Minute))
	s3 := g.Observe("bsc", "0xw", "0xt", "sell", now.Add(2*time.Minute+5*time.Second))
	if !s3.Muted || s3.JustMuted {
		t.Fatalf("should stay muted without re-notify: %+v", s3)
	}

	// after mute expires, fresh window
	later := now.Add(40 * time.Minute)
	g.Observe("bsc", "0xw", "0xt", "buy", later)
	s4 := g.Observe("bsc", "0xw", "0xt", "sell", later.Add(5*time.Second))
	if s4.Muted || s4.Cycles != 1 {
		t.Fatalf("after mute expiry expected fresh cycle1: %+v", s4)
	}
}
