package safety

import "testing"

func TestEvaluateTransferPausable(t *testing.T) {
	cfg := Config{BlockTransferPausable: true}
	got := cfg.Evaluate(TokenRaw{TransferPausable: true})
	if got.OK || got.Reason != "transfer pausable" {
		t.Fatalf("got %+v", got)
	}
}

func TestEvaluateUnlockedLP(t *testing.T) {
	cfg := Config{BlockUnlockedLP: true}
	got := cfg.Evaluate(TokenRaw{UnlockedLP: true})
	if got.OK || got.Reason != "unlocked lp" {
		t.Fatalf("got %+v", got)
	}
}

func TestEvaluateCannotSell(t *testing.T) {
	cfg := Config{BlockCannotSell: true}
	got := cfg.Evaluate(TokenRaw{CannotSell: true})
	if got.OK || got.Reason != "cannot sell" {
		t.Fatalf("got %+v", got)
	}
}

func TestLPUnlocked(t *testing.T) {
	entry := map[string]any{
		"lp_holders": []any{
			map[string]any{
				"address":     "0x5f3b38c0bd126c95ce427fe85ada8a762ee60bbe",
				"is_contract": "1",
				"is_locked":   "0",
				"percent":     "0.999999999999995527",
			},
			map[string]any{
				"address":     "0x0000000000000000000000000000000000000000",
				"is_contract": "0",
				"is_locked":   "1",
				"percent":     "0.000000000000004473",
			},
		},
	}
	raw := parseGoPlusEntry(entry)
	if !raw.UnlockedLP {
		t.Fatal("expected unlocked lp")
	}
}

func TestLPBurnLocked(t *testing.T) {
	entry := map[string]any{
		"lp_holders": []any{
			map[string]any{
				"address":     "0x000000000000000000000000000000000000dead",
				"is_contract": "0",
				"is_locked":   "0",
				"percent":     "1",
			},
		},
	}
	raw := parseGoPlusEntry(entry)
	if raw.UnlockedLP {
		t.Fatal("burn address should not flag unlocked lp")
	}
}
