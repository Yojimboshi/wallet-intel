package execute

import (
	"context"
	"fmt"

	"github.com/Yojimboshi/wallet-intel/internal/pool"
)

func nativeUSD(ctx context.Context, oracle *pool.NativeOracle, fallback float64) (float64, error) {
	if oracle != nil {
		return oracle.USD(ctx)
	}
	if fallback > 0 {
		return fallback, nil
	}
	return 0, fmt.Errorf("native usd price unavailable (no rpc oracle or config fallback)")
}
