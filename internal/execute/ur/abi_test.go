package ur

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestEncodePermit2Approve(t *testing.T) {
	token := common.HexToAddress("0x55d398326f99059fF775485246999027B3197955")
	spender := common.HexToAddress("0xd9C500DfF816a1Da21A48A732d3498Bf09dc9AEB")
	data, err := EncodePermit2Approve(token, spender, big.NewInt(1000), 9999999999)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(data) < 4 {
		t.Fatalf("calldata too short: %x", data)
	}
}
