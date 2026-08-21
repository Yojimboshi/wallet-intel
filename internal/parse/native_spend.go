package parse

import (
	"math/big"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// Four.meme TokenManager2 TokenPurchase/TokenSale — all args in data, none indexed.
var (
	topicTokenPurchase = crypto.Keccak256Hash([]byte("TokenPurchase(address,address,uint256,uint256,uint256,uint256,uint256,uint256)"))
	topicTokenSale     = crypto.Keccak256Hash([]byte("TokenSale(address,address,uint256,uint256,uint256,uint256,uint256,uint256)"))
)

type fourMemeSpend struct {
	token   common.Address
	account common.Address
	amount  *big.Int
	cost    *big.Int
}

func ApplyTxNativeValue(trades []Trade, logs []*types.Log, chainCfg chain.Config, nativeValue *big.Int) {
	enrichNativeSpends(trades, logs, chainCfg, nativeValue)
}

func enrichNativeSpends(trades []Trade, logs []*types.Log, chainCfg chain.Config, nativeValue *big.Int) {
	buys, sells := parseFourMemeSpends(logs)
	for i := range trades {
		if trades[i].QuoteAmount != nil && trades[i].QuoteAmount.Sign() > 0 {
			continue
		}
		switch trades[i].Side {
		case sideBuy:
			if sp, ok := matchFourMeme(buys, trades[i]); ok {
				setNativeQuote(&trades[i], sp.cost, chainCfg)
			}
		case sideSell:
			if sp, ok := matchFourMeme(sells, trades[i]); ok {
				setNativeQuote(&trades[i], sp.cost, chainCfg)
			}
		}
	}

	missingBuys := 0
	for _, tr := range trades {
		if tr.Side == sideBuy && (tr.QuoteAmount == nil || tr.QuoteAmount.Sign() == 0) {
			missingBuys++
		}
	}
	if missingBuys == 1 && nativeValue != nil && nativeValue.Sign() > 0 {
		for i := range trades {
			if trades[i].Side == sideBuy && (trades[i].QuoteAmount == nil || trades[i].QuoteAmount.Sign() == 0) {
				setNativeQuote(&trades[i], nativeValue, chainCfg)
				break
			}
		}
	}
}

func parseFourMemeSpends(logs []*types.Log) (buys, sells []fourMemeSpend) {
	for _, lg := range logs {
		if lg == nil || len(lg.Topics) == 0 {
			continue
		}
		sp, ok := decodeFourMemeSpend(*lg)
		if !ok {
			continue
		}
		switch lg.Topics[0] {
		case topicTokenPurchase:
			buys = append(buys, sp)
		case topicTokenSale:
			sells = append(sells, sp)
		}
	}
	return buys, sells
}

func decodeFourMemeSpend(lg types.Log) (fourMemeSpend, bool) {
	if len(lg.Topics) < 1 {
		return fourMemeSpend{}, false
	}
	if lg.Topics[0] != topicTokenPurchase && lg.Topics[0] != topicTokenSale {
		return fourMemeSpend{}, false
	}
	if len(lg.Data) < 256 {
		return fourMemeSpend{}, false
	}
	token := common.BytesToAddress(lg.Data[0:32])
	account := common.BytesToAddress(lg.Data[32:64])
	amount := new(big.Int).SetBytes(lg.Data[96:128])
	cost := new(big.Int).SetBytes(lg.Data[128:160])
	funds := new(big.Int).SetBytes(lg.Data[224:256])
	bnb := funds
	if bnb.Sign() == 0 {
		bnb = cost
	}
	if bnb.Sign() == 0 {
		return fourMemeSpend{}, false
	}
	return fourMemeSpend{token: token, account: account, amount: amount, cost: bnb}, true
}

func matchFourMeme(list []fourMemeSpend, tr Trade) (fourMemeSpend, bool) {
	for _, sp := range list {
		if sp.account != tr.Wallet {
			continue
		}
		if ValidToken(tr.Token) && sp.token != tr.Token {
			continue
		}
		return sp, true
	}
	return fourMemeSpend{}, false
}

func setNativeQuote(tr *Trade, amount *big.Int, chainCfg chain.Config) {
	if tr == nil || amount == nil || amount.Sign() <= 0 {
		return
	}
	tr.QuoteAmount = new(big.Int).Set(amount)
	if wrapped, ok := chainCfg.WrappedNative(); ok {
		tr.QuoteToken = wrapped
		if q, ok := chainCfg.QuoteTokens[wrapped]; ok {
			tr.QuoteSymbol = q.Symbol
		}
	}
	if tr.DEX == "" {
		tr.DEX = "four-meme"
	}
}
