package chain

import (
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

type ID string

const (
	Ethereum ID = "ethereum"
	BSC      ID = "bsc"
	Base     ID = "base"
)

type Config struct {
	ID              ID
	ChainID         int64
	Name            string
	NativeSymbol    string
	ExplorerTxURL   string
	DexScreener     string
	V2Router        common.Address // Uniswap/Pancake V2 router for quotes and v2direct swaps
	UniversalRouter common.Address // Uniswap UR / Pancake Infinity UR
	Permit2         common.Address
	V3Quoter        common.Address // QuoterV2 for V3 quotes
	NativeUSDPool   common.Address // V2 wrapped-native/stable pair for on-chain USD price
	QuoteTokens     map[common.Address]QuoteToken
}

type QuoteToken struct {
	Symbol   string
	Decimals int
}

var EthereumMainnet = Config{
	ID:              Ethereum,
	ChainID:         1,
	Name:            "Ethereum",
	NativeSymbol:    "ETH",
	ExplorerTxURL:   "https://etherscan.io/tx/",
	DexScreener:     "https://dexscreener.com/ethereum/",
	V2Router:        common.HexToAddress("0x7a250d5630B4cEF539FED892c5FdE5047805D099"),
	UniversalRouter: common.HexToAddress("0x66a9893cC07D91D95644AEDD05D03f95e1dBA8Af"),
	Permit2:         common.HexToAddress("0x000000000022D473030F116dDEE9F6B43aC78BA3"),
	V3Quoter:        common.HexToAddress("0x61fFE014bA17989E743c5F6cB21bF9697530B21e"),
	NativeUSDPool:   common.HexToAddress("0xB4e16d0168e52d35CaCD2c6185b44281Ec28C9Dc"), // Uniswap V2 WETH/USDC
	QuoteTokens: map[common.Address]QuoteToken{
		common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"): {Symbol: "WETH", Decimals: 18},
		common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"): {Symbol: "USDC", Decimals: 6},
		common.HexToAddress("0xdAC17F958D2ee523a2206206994597C13D831831"): {Symbol: "USDT", Decimals: 6},
		common.HexToAddress("0x6B175474E89094C44Da98b954EedeAC495271d0F"): {Symbol: "DAI", Decimals: 18},
	},
}

var BSCMainnet = Config{
	ID:              BSC,
	ChainID:         56,
	Name:            "BNB Chain",
	NativeSymbol:    "BNB",
	ExplorerTxURL:   "https://bscscan.com/tx/",
	DexScreener:     "https://dexscreener.com/bsc/",
	V2Router:        common.HexToAddress("0x10ED43C718714eb63d5aA57B78B54704E256024E"),
	UniversalRouter: common.HexToAddress("0xd9C500DfF816a1Da21A48A732d3498Bf09dc9AEB"),
	Permit2:         common.HexToAddress("0x31c2F6fcFf4F8759b3Bd5Bf0e1084A055615c768"),
	V3Quoter:        common.HexToAddress("0xB048Bbc1Ee6b733FFfCFb9e9CeF7375518e25997"),
	NativeUSDPool:   common.HexToAddress("0xd99c7f6c65857ac913a8f880a4cb84032ab2fc5b"), // Pancake V2 WBNB/USDC
	QuoteTokens: map[common.Address]QuoteToken{
		common.HexToAddress("0xbb4CdB9CBd36B01bD1cBaEBF2De08d9173bc095c"): {Symbol: "WBNB", Decimals: 18},
		common.HexToAddress("0x55d398326f99059fF775485246999027B3197955"): {Symbol: "USDT", Decimals: 18},
		common.HexToAddress("0x8AC76a51cc950d9822D68b83fE1Ad97B32Cd580d"): {Symbol: "USDC", Decimals: 18},
		common.HexToAddress("0xe9e7CEA3DedcA5984780Bafc599bD69ADd087D56"): {Symbol: "BUSD", Decimals: 18},
	},
}

var BaseMainnet = Config{
	ID:              Base,
	ChainID:         8453,
	Name:            "Base",
	NativeSymbol:    "ETH",
	ExplorerTxURL:   "https://basescan.org/tx/",
	DexScreener:     "https://dexscreener.com/base/",
	V2Router:        common.HexToAddress("0x4752ba5DBC23f44d87826276bf6Fd6b1c372aD24"),
	UniversalRouter: common.HexToAddress("0x6fF5693b99212Da76AD316178A4027C1C3777547"),
	Permit2:         common.HexToAddress("0x000000000022D473030F116dDEE9F6B43aC78BA3"),
	V3Quoter:        common.HexToAddress("0x3d4e44Eb1374240CE5F1B871ab261CD16335B76a"),
	NativeUSDPool:   common.HexToAddress("0x88A43bbDF9D098eEC7bCEda4e2494615dfD9bB9C"), // Uniswap V2 WETH/USDC
	QuoteTokens: map[common.Address]QuoteToken{
		common.HexToAddress("0x4200000000000000000000000000000000000006"): {Symbol: "WETH", Decimals: 18},
		common.HexToAddress("0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"): {Symbol: "USDC", Decimals: 6},
		common.HexToAddress("0xfde4C96c8594986C2809F4C4159ee66913561f15"): {Symbol: "USDT", Decimals: 6},
	},
}

func ByID(id string) (Config, bool) {
	switch strings.ToLower(id) {
	case string(Ethereum), "eth", "mainnet":
		return EthereumMainnet, true
	case string(BSC), "bnb", "binance":
		return BSCMainnet, true
	case string(Base), "base-mainnet":
		return BaseMainnet, true
	default:
		return Config{}, false
	}
}

func (c Config) IsQuoteToken(addr common.Address) bool {
	_, ok := c.QuoteTokens[addr]
	return ok
}

// IsExecutionQuote is true for wrapped-native, USDC, and USDT used in V2 copy trades.
func (c Config) IsExecutionQuote(addr common.Address) bool {
	q, ok := c.QuoteTokens[addr]
	if !ok {
		return false
	}
	switch q.Symbol {
	case "WETH", "WBNB", "USDC", "USDT":
		return true
	default:
		return false
	}
}

func (c Config) QuoteTokenDecimals(addr common.Address) (int, bool) {
	q, ok := c.QuoteTokens[addr]
	if !ok {
		return 0, false
	}
	return q.Decimals, ok
}

func (c Config) QuoteSymbol(addr common.Address) string {
	q, ok := c.QuoteTokens[addr]
	if !ok {
		return ""
	}
	return q.Symbol
}

// ExecutionQuoteCandidates returns quote tokens to try for single-hop V2 routes.
// preferred is tried first when it is a supported execution quote.
func (c Config) ExecutionQuoteCandidates(preferred common.Address) []common.Address {
	seen := map[common.Address]struct{}{}
	var out []common.Address
	add := func(a common.Address) {
		if a == (common.Address{}) {
			return
		}
		if _, ok := seen[a]; ok {
			return
		}
		if !c.IsExecutionQuote(a) {
			return
		}
		seen[a] = struct{}{}
		out = append(out, a)
	}
	add(preferred)
	if wrapped, ok := c.WrappedNative(); ok {
		add(wrapped)
	}
	for _, sym := range []string{"USDC", "USDT"} {
		for addr, q := range c.QuoteTokens {
			if q.Symbol == sym {
				add(addr)
			}
		}
	}
	return out
}

// WrappedNative returns WETH/WBNB for the chain.
func (c Config) WrappedNative() (common.Address, bool) {
	for addr, q := range c.QuoteTokens {
		switch q.Symbol {
		case "WETH", "WBNB":
			return addr, true
		}
	}
	return common.Address{}, false
}
