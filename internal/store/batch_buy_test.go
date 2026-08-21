package store

import (
	"testing"
	"time"
)

func TestBatchBuyTrackerFiresOnCumulative(t *testing.T) {
	b := NewBatchBuyTracker()
	now := time.Now()
	window := 2 * time.Minute
	wallet := "0xw1"

	r1 := b.Add("bsc", "0xtoken", wallet, "w1", 120, "0x1", window, 5, 500, now)
	if r1.ShouldFire || r1.Legs != 1 || r1.TotalUsd != 120 {
		t.Fatalf("leg1: %+v", r1)
	}

	r2 := b.Add("bsc", "0xtoken", wallet, "w1", 150, "0x2", window, 5, 500, now.Add(time.Second))
	if r2.ShouldFire || r2.TotalUsd != 270 {
		t.Fatalf("leg2: %+v", r2)
	}

	r3 := b.Add("bsc", "0xtoken", wallet, "w1", 250, "0x3", window, 5, 500, now.Add(2*time.Second))
	if !r3.ShouldFire || r3.TotalUsd != 520 || r3.Legs != 3 {
		t.Fatalf("leg3: %+v", r3)
	}

	r4 := b.Add("bsc", "0xtoken", wallet, "w1", 100, "0x4", window, 5, 500, now.Add(3*time.Second))
	if r4.ShouldFire {
		t.Fatalf("should not fire again: %+v", r4)
	}
}

func TestBatchBuyTrackerDoesNotSumAcrossWallets(t *testing.T) {
	b := NewBatchBuyTracker()
	now := time.Now()
	window := 2 * time.Minute

	r1 := b.Add("bsc", "0xtoken", "0xw1", "w1", 200, "0x1", window, 5, 500, now)
	r2 := b.Add("bsc", "0xtoken", "0xw2", "w2", 200, "0x2", window, 5, 500, now.Add(time.Second))
	r3 := b.Add("bsc", "0xtoken", "0xw1", "w1", 200, "0x3", window, 5, 500, now.Add(2*time.Second))

	if r1.TotalUsd != 200 || r2.TotalUsd != 200 || r3.TotalUsd != 400 {
		t.Fatalf("expected per-wallet totals 200,200,400 got %+v %+v %+v", r1, r2, r3)
	}
	if r3.ShouldFire {
		t.Fatalf("expected no fire at $400 for one wallet: %+v", r3)
	}
}

func TestBatchBuyTrackerExpiresAtMaxLegs(t *testing.T) {
	b := NewBatchBuyTracker()
	now := time.Now()
	window := 2 * time.Minute

	for i := 0; i < 5; i++ {
		r := b.Add("bsc", "0xtoken", "0xw1", "w1", 50, "0x1", window, 5, 500, now.Add(time.Duration(i)*time.Second))
		if r.Expired {
			t.Fatalf("unexpected expire on leg %d: %+v", i+1, r)
		}
	}

	r := b.Add("bsc", "0xtoken", "0xw1", "w1", 50, "0x6", window, 5, 500, now.Add(6*time.Second))
	if !r.Expired {
		t.Fatalf("expected expire at 6th leg: %+v", r)
	}
}
