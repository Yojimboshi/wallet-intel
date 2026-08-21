package ur

import (
	"math/big"
	"time"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/ethereum/go-ethereum/common"
)

type ExecutePayload struct {
	Commands []byte
	Inputs   [][]byte
	Deadline *big.Int
	Value    *big.Int
}

type Venue int

const (
	VenueV3 Venue = iota
	VenueV2
)

type RoutePlan struct {
	TokenIn      common.Address
	TokenOut     common.Address
	HubToken     common.Address // set for 2-hop hub routes
	Venue        Venue
	V3Fee        uint32
	NativeIn     bool
	NativeOut    bool
	TwoHop       bool
	Hop1Venue    Venue
	Hop1V3Fee    uint32
	Hop2Venue    Venue
	Hop2V3Fee    uint32
	QuotedOut    *big.Int
	Hop1Quoted   *big.Int
}

func isPancake(chainCfg chain.Config) bool {
	return chainCfg.ID == chain.BSC
}

func urThisAddress() common.Address {
	return common.BytesToAddress(URAddressThis[:])
}

func unwrapTail(chainCfg chain.Config, tokenOut, recipient common.Address, amountOutMin *big.Int) (unwrap bool, swapRecipient common.Address, unwrapInput []byte, err error) {
	wrapped, ok := chainCfg.WrappedNative()
	if !ok {
		return false, recipient, nil, nil
	}
	if tokenOut != wrapped {
		return false, recipient, nil, nil
	}
	unw, err := encodeUnwrapWETH(recipient, amountOutMin)
	if err != nil {
		return false, recipient, nil, err
	}
	return true, urThisAddress(), unw, nil
}

func buildSingleHopPayload(chainCfg chain.Config, p RoutePlan, amountIn, amountOutMin *big.Int, recipient common.Address, deadline *big.Int) (ExecutePayload, error) {
	pancake := isPancake(chainCfg)
	if p.NativeIn {
		return buildNativeInPayload(chainCfg, p, amountIn, amountOutMin, recipient, deadline, pancake)
	}
	return buildErc20InPayload(chainCfg, p, amountIn, amountOutMin, recipient, deadline, pancake)
}

func buildErc20InPayload(chainCfg chain.Config, p RoutePlan, amountIn, amountOutMin *big.Int, recipient common.Address, deadline *big.Int, pancake bool) (ExecutePayload, error) {
	permitIn, err := encodePermit2TransferFrom(p.TokenIn, urThisAddress(), amountIn)
	if err != nil {
		return ExecutePayload{}, err
	}
	unwrap, swapRecipient, unwrapIn, err := unwrapTail(chainCfg, p.TokenOut, recipient, amountOutMin)
	if err != nil {
		return ExecutePayload{}, err
	}
	swapIn, err := encodeSwapInput(chainCfg, p, amountIn, amountOutMin, swapRecipient, false, pancake)
	if err != nil {
		return ExecutePayload{}, err
	}
	cmd := []byte{CmdPermit2TransferFrom}
	in := [][]byte{permitIn}
	cmdSwap := swapCommand(p.Venue)
	cmd = append(cmd, cmdSwap)
	in = append(in, swapIn)
	if unwrap {
		cmd = append(cmd, CmdUnwrapWETH)
		in = append(in, unwrapIn)
	}
	return ExecutePayload{Commands: packCommands(cmd), Inputs: in, Deadline: deadline, Value: big.NewInt(0)}, nil
}

func buildNativeInPayload(chainCfg chain.Config, p RoutePlan, amountIn, amountOutMin *big.Int, recipient common.Address, deadline *big.Int, pancake bool) (ExecutePayload, error) {
	wrapIn, err := encodeWrapETH(urThisAddress(), amountIn)
	if err != nil {
		return ExecutePayload{}, err
	}
	unwrap, swapRecipient, unwrapIn, err := unwrapTail(chainCfg, p.TokenOut, recipient, amountOutMin)
	if err != nil {
		return ExecutePayload{}, err
	}
	swapIn, err := encodeSwapInput(chainCfg, p, amountIn, amountOutMin, swapRecipient, false, pancake)
	if err != nil {
		return ExecutePayload{}, err
	}
	cmd := []byte{CmdWrapETH}
	in := [][]byte{wrapIn}
	cmd = append(cmd, swapCommand(p.Venue))
	in = append(in, swapIn)
	if unwrap {
		cmd = append(cmd, CmdUnwrapWETH)
		in = append(in, unwrapIn)
	}
	return ExecutePayload{Commands: packCommands(cmd), Inputs: in, Deadline: deadline, Value: new(big.Int).Set(amountIn)}, nil
}

func buildTwoHopPayload(chainCfg chain.Config, p RoutePlan, amountIn, amountOutMin *big.Int, recipient common.Address, deadline *big.Int) (ExecutePayload, error) {
	pancake := isPancake(chainCfg)
	permitIn, err := encodePermit2TransferFrom(p.TokenIn, urThisAddress(), amountIn)
	if err != nil {
		return ExecutePayload{}, err
	}
	hop1Plan := RoutePlan{TokenIn: p.TokenIn, TokenOut: p.HubToken, Venue: p.Hop1Venue, V3Fee: p.Hop1V3Fee}
	hop1OutMin := applySlippage(p.Hop1Quoted, 500) // 5% on intermediate leg
	hop1In, err := encodeSwapInput(chainCfg, hop1Plan, amountIn, hop1OutMin, urThisAddress(), false, pancake)
	if err != nil {
		return ExecutePayload{}, err
	}
	hop2Plan := RoutePlan{TokenIn: p.HubToken, TokenOut: p.TokenOut, Venue: p.Hop2Venue, V3Fee: p.Hop2V3Fee}
	hop2In, err := encodeSwapInput(chainCfg, hop2Plan, p.Hop1Quoted, amountOutMin, recipient, false, pancake)
	if err != nil {
		return ExecutePayload{}, err
	}
	cmd := []byte{CmdPermit2TransferFrom, swapCommand(p.Hop1Venue), swapCommand(p.Hop2Venue)}
	return ExecutePayload{
		Commands: packCommands(cmd),
		Inputs:   [][]byte{permitIn, hop1In, hop2In},
		Deadline: deadline,
		Value:    big.NewInt(0),
	}, nil
}

func buildTwoHopSellPayload(chainCfg chain.Config, p RoutePlan, amountIn, amountOutMin *big.Int, recipient common.Address, deadline *big.Int) (ExecutePayload, error) {
	pancake := isPancake(chainCfg)
	permitIn, err := encodePermit2TransferFrom(p.TokenIn, urThisAddress(), amountIn)
	if err != nil {
		return ExecutePayload{}, err
	}
	hop1Plan := RoutePlan{TokenIn: p.TokenIn, TokenOut: p.HubToken, Venue: p.Hop1Venue, V3Fee: p.Hop1V3Fee}
	hop1OutMin := applySlippage(p.Hop1Quoted, 500)
	hop1In, err := encodeSwapInput(chainCfg, hop1Plan, amountIn, hop1OutMin, urThisAddress(), false, pancake)
	if err != nil {
		return ExecutePayload{}, err
	}
	hop2Plan := RoutePlan{TokenIn: p.HubToken, TokenOut: p.TokenOut, Venue: p.Hop2Venue, NativeOut: p.NativeOut}
	hop2Recipient := recipient
	var unwrapIn []byte
	if p.NativeOut {
		var unwrap bool
		unwrap, hop2Recipient, unwrapIn, err = unwrapTail(chainCfg, p.TokenOut, recipient, amountOutMin)
		if err != nil {
			return ExecutePayload{}, err
		}
		if !unwrap {
			p.NativeOut = false
		}
	}
	hop2In, err := encodeSwapInput(chainCfg, hop2Plan, p.Hop1Quoted, amountOutMin, hop2Recipient, false, pancake)
	if err != nil {
		return ExecutePayload{}, err
	}
	cmd := []byte{CmdPermit2TransferFrom, swapCommand(p.Hop1Venue), swapCommand(p.Hop2Venue)}
	in := [][]byte{permitIn, hop1In, hop2In}
	if p.NativeOut && len(unwrapIn) > 0 {
		cmd = append(cmd, CmdUnwrapWETH)
		in = append(in, unwrapIn)
	}
	return ExecutePayload{Commands: packCommands(cmd), Inputs: in, Deadline: deadline, Value: big.NewInt(0)}, nil
}

func buildSellPayload(chainCfg chain.Config, p RoutePlan, amountIn *big.Int, recipient common.Address, slippageBps int) (ExecutePayload, error) {
	deadline := big.NewInt(time.Now().Add(20 * time.Minute).Unix())
	minOut := applySlippage(p.QuotedOut, slippageBps)
	if p.TwoHop {
		return buildTwoHopSellPayload(chainCfg, p, amountIn, minOut, recipient, deadline)
	}
	p.Venue = p.Venue // single-hop sell uses erc20-in pattern (meme → quote)
	single := RoutePlan{TokenIn: p.TokenIn, TokenOut: p.TokenOut, Venue: p.Venue, V3Fee: p.V3Fee, NativeOut: p.NativeOut}
	return buildErc20InPayload(chainCfg, single, amountIn, minOut, recipient, deadline, isPancake(chainCfg))
}

func buildPayload(chainCfg chain.Config, p RoutePlan, amountIn *big.Int, recipient common.Address, slippageBps int, sell bool) (ExecutePayload, error) {
	if sell {
		return buildSellPayload(chainCfg, p, amountIn, recipient, slippageBps)
	}
	deadline := big.NewInt(time.Now().Add(20 * time.Minute).Unix())
	minOut := applySlippage(p.QuotedOut, slippageBps)
	if p.TwoHop {
		return buildTwoHopPayload(chainCfg, p, amountIn, minOut, recipient, deadline)
	}
	return buildSingleHopPayload(chainCfg, p, amountIn, minOut, recipient, deadline)
}

func encodeSwapInput(chainCfg chain.Config, p RoutePlan, amountIn, amountOutMin *big.Int, recipient common.Address, payerIsUser, pancake bool) ([]byte, error) {
	switch p.Venue {
	case VenueV2:
		path := []common.Address{p.TokenIn, p.TokenOut}
		return encodeV2SwapExactIn(recipient, amountIn, amountOutMin, path, payerIsUser, pancake)
	default:
		path := packV3Path(p.TokenIn, p.V3Fee, p.TokenOut)
		return encodeV3SwapExactIn(recipient, amountIn, amountOutMin, path, payerIsUser, pancake)
	}
}

func swapCommand(v Venue) byte {
	if v == VenueV2 {
		return CmdV2SwapExactIn
	}
	return CmdV3SwapExactIn
}

// BuildBuyPayload encodes a Universal Router buy execute payload.
func BuildBuyPayload(chainCfg chain.Config, p RoutePlan, amountIn *big.Int, recipient common.Address, slippageBps int) (ExecutePayload, error) {
	return buildPayload(chainCfg, p, amountIn, recipient, slippageBps, false)
}

// BuildSellPayload encodes a Universal Router sell execute payload.
func BuildSellPayload(chainCfg chain.Config, p RoutePlan, amountIn *big.Int, recipient common.Address, slippageBps int) (ExecutePayload, error) {
	return buildPayload(chainCfg, p, amountIn, recipient, slippageBps, true)
}

// EncodeExecute ABI-encodes execute(commands, inputs, deadline).
func EncodeExecute(commands []byte, inputs [][]byte, deadline *big.Int) ([]byte, error) {
	return encodeExecute(commands, inputs, deadline)
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
