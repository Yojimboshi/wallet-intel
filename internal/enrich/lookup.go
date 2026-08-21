package enrich

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
)

// Lookup resolves token metadata for alerts and copy sizing.
type Lookup interface {
	LookupToken(ctx context.Context, token common.Address) (TokenInfo, error)
}
