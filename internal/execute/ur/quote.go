package ur

import (
	"context"
	"fmt"
	"math/big"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

var (
	selectorGetAmountsOut = [4]byte{0xd0, 0x6c, 0xa6, 0x1f}
)

var defaultV3Fees = []uint32{3000, 500, 10000, 100}

func QuoteV3SingleHop(ctx context.Context, client *ethclient.Client, chainCfg chain.Config, tokenIn, tokenOut common.Address, amountIn *big.Int, feeHint uint32) (*big.Int, uint32, error) {
	if chainCfg.V3Quoter == (common.Address{}) {
		return nil, 0, fmt.Errorf("v3 quoter not configured")
	}
	fees := defaultV3Fees
	if feeHint > 0 {
		fees = []uint32{feeHint}
	}
	var lastErr error
	for _, fee := range fees {
		path := packV3Path(tokenIn, fee, tokenOut)
		data, err := encodeQuoteExactInput(path, amountIn)
		if err != nil {
			return nil, 0, err
		}
		out, err := client.CallContract(ctx, ethereum.CallMsg{To: &chainCfg.V3Quoter, Data: data}, nil)
		if err != nil {
			lastErr = err
			continue
		}
		quoted, err := decodeQuoteExactInput(out)
		if err != nil || quoted == nil || quoted.Sign() <= 0 {
			lastErr = err
			continue
		}
		return quoted, fee, nil
	}
	if lastErr != nil {
		return nil, 0, lastErr
	}
	return nil, 0, fmt.Errorf("no v3 pool")
}

func QuoteV2SingleHop(ctx context.Context, client *ethclient.Client, chainCfg chain.Config, tokenIn, tokenOut common.Address, amountIn *big.Int) (*big.Int, error) {
	router := chainCfg.V2Router
	if router == (common.Address{}) {
		return nil, fmt.Errorf("v2 router not configured")
	}
	return quoteV2AmountOut(ctx, client, router, amountIn, []common.Address{tokenIn, tokenOut})
}

func quoteV2AmountOut(ctx context.Context, client *ethclient.Client, router common.Address, amountIn *big.Int, path []common.Address) (*big.Int, error) {
	data := encodeGetAmountsOut(amountIn, path)
	out, err := client.CallContract(ctx, ethereum.CallMsg{To: &router, Data: data}, nil)
	if err != nil {
		return nil, fmt.Errorf("getAmountsOut: %w", err)
	}
	return decodeAmountsOut(out)
}

func encodeGetAmountsOut(amountIn *big.Int, path []common.Address) []byte {
	data := make([]byte, 4+32+32)
	copy(data[:4], selectorGetAmountsOut[:])
	copy(data[4:36], padUint256(amountIn))
	copy(data[36:68], padUint256(big.NewInt(64)))
	data = append(data, encodeAddressArray(path)...)
	return data
}

func encodeAddressArray(addrs []common.Address) []byte {
	out := padUint256(big.NewInt(int64(len(addrs))))
	for _, a := range addrs {
		out = append(out, padAddress(a)...)
	}
	return out
}

func padAddress(addr common.Address) []byte {
	out := make([]byte, 32)
	copy(out[12:], addr.Bytes())
	return out
}

func padUint256(v *big.Int) []byte {
	out := make([]byte, 32)
	if v != nil {
		v.FillBytes(out)
	}
	return out
}

func decodeAmountsOut(out []byte) (*big.Int, error) {
	if len(out) < 64 {
		return nil, fmt.Errorf("bad rpc response")
	}
	offset := new(big.Int).SetBytes(out[0:32]).Uint64()
	if offset+32 > uint64(len(out)) {
		return nil, fmt.Errorf("bad rpc response")
	}
	base := int(offset)
	length := new(big.Int).SetBytes(out[base : base+32]).Uint64()
	if length == 0 {
		return big.NewInt(0), nil
	}
	last := base + 32 + (int(length)-1)*32
	if last+32 > len(out) {
		return nil, fmt.Errorf("bad rpc response")
	}
	return new(big.Int).SetBytes(out[last : last+32]), nil
}
