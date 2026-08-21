package ur

import (
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

var (
	executeABI abi.ABI
	permit2ABI abi.ABI
	quoterABI  abi.ABI
)

func init() {
	var err error
	executeABI, err = abi.JSON(strings.NewReader(`[{"type":"function","name":"execute","inputs":[{"name":"commands","type":"bytes"},{"name":"inputs","type":"bytes[]"},{"name":"deadline","type":"uint256"}],"outputs":[],"stateMutability":"payable"}]`))
	if err != nil {
		panic(err)
	}
	permit2ABI, err = abi.JSON(strings.NewReader(`[
		{"type":"function","name":"approve","inputs":[{"name":"token","type":"address"},{"name":"spender","type":"address"},{"name":"amount","type":"uint160"},{"name":"expiration","type":"uint48"}],"outputs":[]},
		{"type":"function","name":"allowance","inputs":[{"name":"owner","type":"address"},{"name":"token","type":"address"},{"name":"spender","type":"address"}],"outputs":[{"name":"amount","type":"uint160"},{"name":"expiration","type":"uint48"},{"name":"nonce","type":"uint48"}]}
	]`))
	if err != nil {
		panic(err)
	}
	quoterABI, err = abi.JSON(strings.NewReader(`[{"type":"function","name":"quoteExactInput","inputs":[{"name":"path","type":"bytes"},{"name":"amountIn","type":"uint256"}],"outputs":[{"name":"amountOut","type":"uint256"},{"name":"sqrtPriceX96AfterList","type":"uint160[]"},{"name":"initializedTicksCrossedList","type":"uint32[]"},{"name":"gasEstimate","type":"uint256"}],"stateMutability":"nonpayable"}]`))
	if err != nil {
		panic(err)
	}
}

func encodeExecute(commands []byte, inputs [][]byte, deadline *big.Int) ([]byte, error) {
	return executeABI.Pack("execute", commands, inputs, deadline)
}

func encodePermit2Approve(token, spender common.Address, amount *big.Int, expiration uint64) ([]byte, error) {
	return permit2ABI.Pack("approve", token, spender, amount, big.NewInt(int64(expiration)))
}

// EncodePermit2Approve builds Permit2 approve calldata for the Universal Router.
func EncodePermit2Approve(token, spender common.Address, amount *big.Int, expiration uint64) ([]byte, error) {
	return encodePermit2Approve(token, spender, amount, expiration)
}

func encodePermit2Allowance(owner, token, spender common.Address) ([]byte, error) {
	return permit2ABI.Pack("allowance", owner, token, spender)
}

// EncodePermit2Allowance builds Permit2 allowance read calldata.
func EncodePermit2Allowance(owner, token, spender common.Address) ([]byte, error) {
	return encodePermit2Allowance(owner, token, spender)
}

// DecodePermit2Allowance parses Permit2 allowance output.
func DecodePermit2Allowance(out []byte) (amount *big.Int, expiration uint64, err error) {
	return decodePermit2Allowance(out)
}

func decodePermit2Allowance(out []byte) (amount *big.Int, expiration uint64, err error) {
	vals, err := permit2ABI.Unpack("allowance", out)
	if err != nil {
		return nil, 0, err
	}
	amount = vals[0].(*big.Int)
	switch exp := vals[1].(type) {
	case uint64:
		expiration = exp
	case *big.Int:
		expiration = exp.Uint64()
	default:
		expiration = 0
	}
	return amount, expiration, nil
}

func encodeQuoteExactInput(path []byte, amountIn *big.Int) ([]byte, error) {
	return quoterABI.Pack("quoteExactInput", path, amountIn)
}

func decodeQuoteExactInput(out []byte) (*big.Int, error) {
	vals, err := quoterABI.Unpack("quoteExactInput", out)
	if err != nil {
		return nil, err
	}
	return vals[0].(*big.Int), nil
}

func encodePermit2TransferFrom(token, recipient common.Address, amount *big.Int) ([]byte, error) {
	uint160Max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 160), big.NewInt(1))
	if amount.Cmp(uint160Max) > 0 {
		return nil, errAmountTooLarge
	}
	return abi.Arguments{
		{Type: mustType("address")},
		{Type: mustType("address")},
		{Type: mustType("uint160")},
	}.Pack(token, recipient, amount)
}

func encodeWrapETH(recipient common.Address, amount *big.Int) ([]byte, error) {
	return abi.Arguments{
		{Type: mustType("address")},
		{Type: mustType("uint256")},
	}.Pack(recipient, amount)
}

func encodeUnwrapWETH(recipient common.Address, amountMin *big.Int) ([]byte, error) {
	return abi.Arguments{
		{Type: mustType("address")},
		{Type: mustType("uint256")},
	}.Pack(recipient, amountMin)
}

func encodeV3SwapExactIn(recipient common.Address, amountIn, amountOutMin *big.Int, path []byte, payerIsUser bool, pancake bool) ([]byte, error) {
	if pancake {
		return abi.Arguments{
			{Type: mustType("address")},
			{Type: mustType("uint256")},
			{Type: mustType("uint256")},
			{Type: mustType("bytes")},
			{Type: mustType("bool")},
		}.Pack(recipient, amountIn, amountOutMin, path, payerIsUser)
	}
	return abi.Arguments{
		{Type: mustType("address")},
		{Type: mustType("uint256")},
		{Type: mustType("uint256")},
		{Type: mustType("bytes")},
		{Type: mustType("bool")},
		{Type: mustType("uint256[]")},
	}.Pack(recipient, amountIn, amountOutMin, path, payerIsUser, []*big.Int{})
}

func encodeV2SwapExactIn(recipient common.Address, amountIn, amountOutMin *big.Int, path []common.Address, payerIsUser bool, pancake bool) ([]byte, error) {
	if pancake {
		return abi.Arguments{
			{Type: mustType("address")},
			{Type: mustType("uint256")},
			{Type: mustType("uint256")},
			{Type: mustType("address[]")},
			{Type: mustType("bool")},
		}.Pack(recipient, amountIn, amountOutMin, path, payerIsUser)
	}
	return abi.Arguments{
		{Type: mustType("address")},
		{Type: mustType("uint256")},
		{Type: mustType("uint256")},
		{Type: mustType("address[]")},
		{Type: mustType("bool")},
		{Type: mustType("uint256[]")},
	}.Pack(recipient, amountIn, amountOutMin, path, payerIsUser, []*big.Int{})
}

func packV3Path(tokenIn common.Address, fee uint32, tokenOut common.Address) []byte {
	out := make([]byte, 0, 43)
	out = append(out, tokenIn.Bytes()...)
	fb := make([]byte, 3)
	fb[0] = byte(fee >> 16)
	fb[1] = byte(fee >> 8)
	fb[2] = byte(fee)
	out = append(out, fb...)
	out = append(out, tokenOut.Bytes()...)
	return out
}

func mustType(s string) abi.Type {
	t, err := abi.NewType(s, "", nil)
	if err != nil {
		panic(err)
	}
	return t
}

var errAmountTooLarge = errString("permit2 amount exceeds uint160")

type errString string

func (e errString) Error() string { return string(e) }
