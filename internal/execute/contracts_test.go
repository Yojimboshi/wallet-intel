package execute

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestApplySlippage(t *testing.T) {
	in := big.NewInt(10000)
	out := applySlippage(in, 500) // 5%
	if out.Cmp(big.NewInt(9500)) != 0 {
		t.Fatalf("got %s want 9500", out)
	}
}

func TestEncodeGetAmountsOut(t *testing.T) {
	path := []common.Address{
		common.HexToAddress("0xbb4CdB9CBd36B01bD1cBaEBF2De08d9173bc095c"),
		common.HexToAddress("0x218BB5a617d9fd91fe249E8729c42E4745927777"),
	}
	data := encodeGetAmountsOut(big.NewInt(1e16), path)
	if len(data) < 4+32+32+32+64 {
		t.Fatalf("short calldata: %d bytes", len(data))
	}
	if data[0] != 0xd0 || data[1] != 0x6c {
		t.Fatalf("unexpected selector: %x", data[:4])
	}
}

func TestDecodeAmountsOut(t *testing.T) {
	// offset=32, len=2, amt0, amt1
	out := append([]byte{}, padUint256(big.NewInt(32))...)
	out = append(out, padUint256(big.NewInt(2))...)
	out = append(out, padUint256(big.NewInt(1000))...)
	out = append(out, padUint256(big.NewInt(5000))...)
	amt, err := decodeAmountsOut(out)
	if err != nil {
		t.Fatal(err)
	}
	if amt.Cmp(big.NewInt(5000)) != 0 {
		t.Fatalf("got %s", amt)
	}
}
