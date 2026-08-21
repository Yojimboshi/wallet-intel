package exit

import (
	"errors"
	"testing"

	"github.com/Yojimboshi/wallet-intel/internal/execute"
)

func TestIsUnsellableSellErr(t *testing.T) {
	cases := []string{
		"execution reverted: TransferHelper: TRANSFER_FROM_FAILED",
		"cannot sell token",
		"honeypot detected",
	}
	for _, msg := range cases {
		if !isUnsellableSellErr(errors.New(msg)) {
			t.Fatalf("expected unsellable: %q", msg)
		}
	}
	if isUnsellableSellErr(errors.New("insufficient funds")) {
		t.Fatal("transient error should retry")
	}
	if !isNoBalanceSellErr(errors.New("wallet evm-1: no 0xabc balance to sell")) {
		t.Fatal("expected no-balance classification")
	}
	if isUnsellableSellErr(errors.New("wallet evm-1: no 0xabc balance to sell")) {
		t.Fatal("no balance should not be unsellable honeypot")
	}
	if isUnsellableSellErr(errors.New("PancakeRouter: INSUFFICIENT_OUTPUT_AMOUNT")) {
		t.Fatal("slippage should not be unsellable")
	}
	if !execute.IsSlippageSellErr(errors.New("PancakeRouter: INSUFFICIENT_OUTPUT_AMOUNT")) {
		t.Fatal("expected slippage classification")
	}
}
