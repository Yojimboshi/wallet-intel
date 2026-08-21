package enrich

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
)

// StaticLookup returns fixed token info (for dry-run / tests).
type StaticLookup struct {
	Info TokenInfo
	Err  error
}

func (s StaticLookup) LookupToken(_ context.Context, _ common.Address) (TokenInfo, error) {
	if s.Err != nil {
		return TokenInfo{}, s.Err
	}
	return s.Info, nil
}
