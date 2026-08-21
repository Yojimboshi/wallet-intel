package chain

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestByID_Base(t *testing.T) {
	cfg, ok := ByID("base")
	if !ok {
		t.Fatal("expected base chain")
	}
	if cfg.ChainID != 8453 {
		t.Fatalf("chainID=%d", cfg.ChainID)
	}
	if cfg.UniversalRouter == (common.Address{}) {
		t.Fatal("missing universal router")
	}
	if cfg.V3Quoter == (common.Address{}) {
		t.Fatal("missing v3 quoter")
	}
}
