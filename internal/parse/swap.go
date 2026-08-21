package parse

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	sideBuy  = "buy"
	sideSell = "sell"
)

var (
	topicTransfer = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	topicSwapV2   = crypto.Keccak256Hash([]byte("Swap(address,uint256,uint256,uint256,uint256,address)"))
	topicSwapV3   = crypto.Keccak256Hash([]byte("Swap(address,address,int256,int256,uint160,uint128,int24)"))
)

type Trade struct {
	Wallet      common.Address
	WalletLabel string
	Side        string
	Token       common.Address
	TokenAmount *big.Int
	QuoteToken  common.Address
	QuoteAmount *big.Int
	QuoteSymbol string
	TxHash      common.Hash
	BlockNumber uint64
	LogIndex    uint
	Pair        common.Address
	DEX         string
}

func ParseLogs(
	logs []*types.Log,
	watched map[common.Address]string,
	chainCfg chain.Config,
	txHash common.Hash,
	blockNumber uint64,
) []Trade {
	seen := make(map[string]struct{})
	var trades []Trade

	for _, log := range logs {
		if log == nil || len(log.Topics) == 0 {
			continue
		}

		switch log.Topics[0] {
		case topicTransfer:
			t, ok := parseTransfer(*log, watched, chainCfg, txHash, blockNumber)
			if ok {
				key := tradeKey(t)
				if _, dup := seen[key]; !dup {
					seen[key] = struct{}{}
					trades = append(trades, t)
				}
			}
		case topicSwapV2:
			t, ok := parseSwapV2(*log, watched, chainCfg, txHash, blockNumber)
			if ok {
				key := tradeKey(t)
				if _, dup := seen[key]; !dup {
					seen[key] = struct{}{}
					trades = append(trades, t)
				}
			}
		case topicSwapV3:
			t, ok := parseSwapV3(*log, watched, chainCfg, txHash, blockNumber)
			if ok {
				key := tradeKey(t)
				if _, dup := seen[key]; !dup {
					seen[key] = struct{}{}
					trades = append(trades, t)
				}
			}
		}
	}

	enrichQuoteAmounts(trades, logs, watched, chainCfg)
	trades = consolidateTrades(trades, chainCfg)
	enrichNativeSpends(trades, logs, chainCfg, nil)
	enrichAggregatorQuotes(trades, logs, chainCfg)
	return trades
}

func parseTransfer(
	log types.Log,
	watched map[common.Address]string,
	chainCfg chain.Config,
	txHash common.Hash,
	blockNumber uint64,
) (Trade, bool) {
	if len(log.Topics) < 3 {
		return Trade{}, false
	}

	from := common.BytesToAddress(log.Topics[1].Bytes())
	to := common.BytesToAddress(log.Topics[2].Bytes())
	token := log.Address

	if chainCfg.IsQuoteToken(token) {
		return Trade{}, false
	}

	amount := new(big.Int).SetBytes(log.Data)

	var wallet common.Address
	var side string
	switch {
	case hasLabel(watched, to):
		wallet, side = to, sideBuy
	case hasLabel(watched, from):
		wallet, side = from, sideSell
	default:
		return Trade{}, false
	}

	return Trade{
		Wallet:      wallet,
		WalletLabel: watched[wallet],
		Side:        side,
		Token:       token,
		TokenAmount: amount,
		TxHash:      txHash,
		BlockNumber: blockNumber,
		LogIndex:    log.Index,
	}, true
}

func parseSwapV2(
	log types.Log,
	watched map[common.Address]string,
	chainCfg chain.Config,
	txHash common.Hash,
	blockNumber uint64,
) (Trade, bool) {
	if len(log.Topics) < 3 || len(log.Data) < 128 {
		return Trade{}, false
	}

	sender := common.BytesToAddress(log.Topics[1].Bytes())
	to := common.BytesToAddress(log.Topics[2].Bytes())
	wallet := to
	label, ok := watched[to]
	if !ok {
		label, ok = watched[sender]
		wallet = sender
	}
	if !ok {
		return Trade{}, false
	}

	amount0In := new(big.Int).SetBytes(log.Data[0:32])
	amount1In := new(big.Int).SetBytes(log.Data[32:64])
	amount0Out := new(big.Int).SetBytes(log.Data[64:96])
	amount1Out := new(big.Int).SetBytes(log.Data[96:128])

	// Without pair token0/token1 we record the swap venue; transfer logs fill token details.
	var quoteAmount *big.Int
	side := sideBuy
	if amount0In.Sign() > 0 {
		quoteAmount = amount0In
	} else if amount1In.Sign() > 0 {
		quoteAmount = amount1In
	}
	if amount0Out.Sign() > 0 || amount1Out.Sign() > 0 {
		side = sideBuy
	}

	return Trade{
		Wallet:      wallet,
		WalletLabel: label,
		Side:        side,
		QuoteAmount: quoteAmount,
		TxHash:      txHash,
		BlockNumber: blockNumber,
		LogIndex:    log.Index,
		Pair:        log.Address,
		DEX:         "uniswap-v2",
	}, true
}

func parseSwapV3(
	log types.Log,
	watched map[common.Address]string,
	chainCfg chain.Config,
	txHash common.Hash,
	blockNumber uint64,
) (Trade, bool) {
	if len(log.Topics) < 3 || len(log.Data) < 160 {
		return Trade{}, false
	}

	recipient := common.BytesToAddress(log.Topics[2].Bytes())
	label, ok := watched[recipient]
	if !ok {
		return Trade{}, false
	}

	amount0 := new(big.Int).SetBytes(log.Data[0:32])
	amount1 := new(big.Int).SetBytes(log.Data[32:64])

	side := sideBuy
	var quoteAmount *big.Int
	if amount0.Sign() < 0 {
		quoteAmount = new(big.Int).Abs(amount0)
	} else if amount1.Sign() < 0 {
		quoteAmount = new(big.Int).Abs(amount1)
	}

	return Trade{
		Wallet:      recipient,
		WalletLabel: label,
		Side:        side,
		QuoteAmount: quoteAmount,
		TxHash:      txHash,
		BlockNumber: blockNumber,
		LogIndex:    log.Index,
		Pair:        log.Address,
		DEX:         "uniswap-v3",
	}, true
}

func enrichQuoteAmounts(
	trades []Trade,
	logs []*types.Log,
	watched map[common.Address]string,
	chainCfg chain.Config,
) {
	for i := range trades {
		if trades[i].QuoteAmount != nil {
			continue
		}
		for _, log := range logs {
			if log == nil || len(log.Topics) < 3 || log.Topics[0] != topicTransfer {
				continue
			}
			if !chainCfg.IsQuoteToken(log.Address) {
				continue
			}
			from := common.BytesToAddress(log.Topics[1].Bytes())
			to := common.BytesToAddress(log.Topics[2].Bytes())
			amount := new(big.Int).SetBytes(log.Data)
			if !isSpendSized(amount) {
				continue
			}

			switch trades[i].Side {
			case sideBuy:
				if from == trades[i].Wallet {
					trades[i].QuoteToken = log.Address
					trades[i].QuoteAmount = amount
					if q, ok := chainCfg.QuoteTokens[log.Address]; ok {
						trades[i].QuoteSymbol = q.Symbol
					}
				}
			case sideSell:
				if to == trades[i].Wallet {
					trades[i].QuoteToken = log.Address
					trades[i].QuoteAmount = amount
					if q, ok := chainCfg.QuoteTokens[log.Address]; ok {
						trades[i].QuoteSymbol = q.Symbol
					}
				}
			}
		}
	}
}

func tradeKey(t Trade) string {
	return fmt.Sprintf("%s:%d:%s:%s", t.TxHash.Hex(), t.LogIndex, t.Side, t.Token.Hex())
}

// consolidateTrades merges transfer + swap logs from the same tx/wallet/side.
// Swap-only rows often lack Token; transfer-only rows often lack QuoteAmount on native buys.
func consolidateTrades(trades []Trade, chainCfg chain.Config) []Trade {
	if len(trades) <= 1 {
		return dropInvalidTrades(trades)
	}

	type groupKey struct {
		tx     common.Hash
		wallet common.Address
		side   string
	}
	groups := make(map[groupKey][]int)
	for i, tr := range trades {
		k := groupKey{tr.TxHash, tr.Wallet, tr.Side}
		groups[k] = append(groups[k], i)
	}

	seen := make(map[int]struct{})
	var out []Trade
	for _, idxs := range groups {
		if len(idxs) == 1 {
			tr := trades[idxs[0]]
			if ValidToken(tr.Token) {
				out = append(out, tr)
			}
			continue
		}

		var base *Trade
		for _, i := range idxs {
			tr := trades[i]
			if !ValidToken(tr.Token) {
				continue
			}
			if base == nil {
				copyTr := tr
				base = &copyTr
				seen[i] = struct{}{}
				continue
			}
			mergeTradeFields(base, &tr, chainCfg)
			seen[i] = struct{}{}
		}
		for _, i := range idxs {
			if _, ok := seen[i]; ok {
				continue
			}
			if base == nil {
				continue
			}
			mergeTradeFields(base, &trades[i], chainCfg)
			seen[i] = struct{}{}
		}
		if base != nil {
			out = append(out, *base)
		}
	}
	return out
}

func dropInvalidTrades(trades []Trade) []Trade {
	out := make([]Trade, 0, len(trades))
	for _, tr := range trades {
		if ValidToken(tr.Token) {
			out = append(out, tr)
		}
	}
	return out
}

func mergeTradeFields(base *Trade, other *Trade, chainCfg chain.Config) {
	if base.QuoteAmount == nil && other.QuoteAmount != nil {
		base.QuoteAmount = other.QuoteAmount
	}
	if base.QuoteToken == (common.Address{}) && other.QuoteToken != (common.Address{}) {
		base.QuoteToken = other.QuoteToken
	}
	if base.QuoteSymbol == "" && other.QuoteSymbol != "" {
		base.QuoteSymbol = other.QuoteSymbol
	}
	if base.Pair == (common.Address{}) && other.Pair != (common.Address{}) {
		base.Pair = other.Pair
	}
	if base.DEX == "" && other.DEX != "" {
		base.DEX = other.DEX
	}
	if (base.TokenAmount == nil || base.TokenAmount.Sign() == 0) && other.TokenAmount != nil && other.TokenAmount.Sign() > 0 {
		base.TokenAmount = other.TokenAmount
	}
	if base.QuoteAmount != nil && base.QuoteToken == (common.Address{}) {
		if wrapped, ok := chainCfg.WrappedNative(); ok {
			base.QuoteToken = wrapped
			if q, ok := chainCfg.QuoteTokens[wrapped]; ok && base.QuoteSymbol == "" {
				base.QuoteSymbol = q.Symbol
			}
		}
	}
}

// ValidToken rejects zero address and other non-tradeable placeholders.
func ValidToken(token common.Address) bool {
	return token != (common.Address{})
}

func hasLabel(watched map[common.Address]string, addr common.Address) bool {
	_, ok := watched[addr]
	return ok
}

func FormatAmount(amount *big.Int, decimals int) string {
	if amount == nil {
		return "?"
	}
	if decimals <= 0 {
		return amount.String()
	}

	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	whole := new(big.Int).Div(amount, divisor)
	frac := new(big.Int).Mod(amount, divisor)

	fracStr := fmt.Sprintf("%0*s", decimals, frac.String())
	fracStr = strings.TrimRight(fracStr, "0")
	if fracStr == "" {
		return whole.String()
	}
	return whole.String() + "." + fracStr
}
