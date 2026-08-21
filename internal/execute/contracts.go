package execute

import (
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

var (
	selectorGetAmountsOut          = [4]byte{0xd0, 0x6c, 0xa6, 0x1f}
	selectorSwapExactETHForTokens  = [4]byte{0x7f, 0xf3, 0x6a, 0xb5}
	selectorSwapExactTokensForETH  = [4]byte{0x18, 0xcb, 0xaf, 0xe5}
	selectorSwapExactTokensForETHFee = [4]byte{0x79, 0x1a, 0xc9, 0x47}
	selectorSwapExactTokensForTokensFee = [4]byte{0x5c, 0x11, 0xd7, 0x95}
	selectorApprove                = [4]byte{0x09, 0x5e, 0xa7, 0xb3}
	selectorAllowance              = [4]byte{0xdd, 0x62, 0xed, 0x3e}
	selectorBalanceOf              = [4]byte{0x70, 0xa0, 0x82, 0x31}
	selectorTransfer               = [4]byte{0xa9, 0x05, 0x9c, 0xbb}
	maxUint256                     = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
)

func callMsg(to common.Address, data []byte) ethereum.CallMsg {
	return CallMsg(to, data)
}

// CallMsg builds an eth_call target for contract reads.
func CallMsg(to common.Address, data []byte) ethereum.CallMsg {
	return ethereum.CallMsg{To: &to, Data: data}
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

func encodeAddressArray(addrs []common.Address) []byte {
	out := padUint256(big.NewInt(int64(len(addrs))))
	for _, a := range addrs {
		out = append(out, padAddress(a)...)
	}
	return out
}

func encodeGetAmountsOut(amountIn *big.Int, path []common.Address) []byte {
	data := make([]byte, 4+32+32)
	copy(data[:4], selectorGetAmountsOut[:])
	copy(data[4:36], padUint256(amountIn))
	copy(data[36:68], padUint256(big.NewInt(64))) // offset to path
	data = append(data, encodeAddressArray(path)...)
	return data
}

func encodeSwapExactETHForTokens(amountOutMin *big.Int, path []common.Address, to common.Address, deadline *big.Int) []byte {
	head := make([]byte, 4+32*4)
	copy(head[:4], selectorSwapExactETHForTokens[:])
	copy(head[4:36], padUint256(amountOutMin))
	copy(head[36:68], padUint256(big.NewInt(128))) // offset to path
	copy(head[68:100], padAddress(to))
	copy(head[100:132], padUint256(deadline))
	out := append(head, encodeAddressArray(path)...)
	return out
}

func encodeSwapExactTokensForETH(amountIn, amountOutMin *big.Int, path []common.Address, to common.Address, deadline *big.Int) []byte {
	return encodeSwapExactTokensForETHWithSelector(selectorSwapExactTokensForETH, amountIn, amountOutMin, path, to, deadline)
}

func encodeSwapExactTokensForETHSupportingFee(amountIn, amountOutMin *big.Int, path []common.Address, to common.Address, deadline *big.Int) []byte {
	return encodeSwapExactTokensForETHWithSelector(selectorSwapExactTokensForETHFee, amountIn, amountOutMin, path, to, deadline)
}

func encodeSwapExactTokensForTokensSupportingFee(amountIn, amountOutMin *big.Int, path []common.Address, to common.Address, deadline *big.Int) []byte {
	head := make([]byte, 4+32*5)
	copy(head[:4], selectorSwapExactTokensForTokensFee[:])
	copy(head[4:36], padUint256(amountIn))
	copy(head[36:68], padUint256(amountOutMin))
	copy(head[68:100], padUint256(big.NewInt(160))) // offset to path
	copy(head[100:132], padAddress(to))
	copy(head[132:164], padUint256(deadline))
	out := append(head, encodeAddressArray(path)...)
	return out
}

func encodeSwapExactTokensForETHWithSelector(sel [4]byte, amountIn, amountOutMin *big.Int, path []common.Address, to common.Address, deadline *big.Int) []byte {
	head := make([]byte, 4+32*5)
	copy(head[:4], sel[:])
	copy(head[4:36], padUint256(amountIn))
	copy(head[36:68], padUint256(amountOutMin))
	copy(head[68:100], padUint256(big.NewInt(160))) // offset to path
	copy(head[100:132], padAddress(to))
	copy(head[132:164], padUint256(deadline))
	out := append(head, encodeAddressArray(path)...)
	return out
}

func encodeApprove(spender common.Address, amount *big.Int) []byte {
	data := make([]byte, 4+32+32)
	copy(data[:4], selectorApprove[:])
	copy(data[4:36], padAddress(spender))
	copy(data[36:68], padUint256(amount))
	return data
}

func encodeAllowance(owner, spender common.Address) []byte {
	data := make([]byte, 4+32+32)
	copy(data[:4], selectorAllowance[:])
	copy(data[4:36], padAddress(owner))
	copy(data[36:68], padAddress(spender))
	return data
}

func encodeBalanceOf(owner common.Address) []byte {
	data := make([]byte, 4+32)
	copy(data[:4], selectorBalanceOf[:])
	copy(data[4:36], padAddress(owner))
	return data
}

func encodeTransfer(to common.Address, amount *big.Int) []byte {
	data := make([]byte, 4+32+32)
	copy(data[:4], selectorTransfer[:])
	copy(data[4:36], padAddress(to))
	copy(data[36:68], padUint256(amount))
	return data
}

func decodeUint256(out []byte) *big.Int {
	if len(out) < 32 {
		return big.NewInt(0)
	}
	return new(big.Int).SetBytes(out[len(out)-32:])
}

func decodeAmountsOut(out []byte) (*big.Int, error) {
	if len(out) < 64 {
		return nil, errBadRPC
	}
	offset := new(big.Int).SetBytes(out[0:32]).Uint64()
	if offset+32 > uint64(len(out)) {
		return nil, errBadRPC
	}
	base := int(offset)
	length := new(big.Int).SetBytes(out[base : base+32]).Uint64()
	if length == 0 {
		return big.NewInt(0), nil
	}
	last := base + 32 + (int(length)-1)*32
	if last+32 > len(out) {
		return nil, errBadRPC
	}
	return new(big.Int).SetBytes(out[last : last+32]), nil
}

func applySlippage(amount *big.Int, slippageBps int) *big.Int {
	if amount == nil || amount.Sign() <= 0 {
		return big.NewInt(0)
	}
	if slippageBps <= 0 {
		return new(big.Int).Set(amount)
	}
	if slippageBps >= 10000 {
		return big.NewInt(0)
	}
	num := new(big.Int).Mul(amount, big.NewInt(int64(10000-slippageBps)))
	return num.Div(num, big.NewInt(10000))
}

var errBadRPC = errString("unexpected rpc response")
