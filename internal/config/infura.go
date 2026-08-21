package config

type infuraNetwork struct {
	http string
	ws   string
}

// Infura multi-chain read endpoints (one API key, all networks).
var infuraNetworks = map[string]infuraNetwork{
	"ethereum": {
		http: "https://mainnet.infura.io/v3/",
		ws:   "wss://mainnet.infura.io/ws/v3/",
	},
	"bsc": {
		http: "https://bsc-mainnet.infura.io/v3/",
		ws:   "wss://bsc-mainnet.infura.io/ws/v3/",
	},
	"base": {
		http: "https://base-mainnet.infura.io/v3/",
		ws:   "wss://base-mainnet.infura.io/ws/v3/",
	},
	"arbitrum": {
		http: "https://arbitrum-mainnet.infura.io/v3/",
		ws:   "wss://arbitrum-mainnet.infura.io/ws/v3/",
	},
}

const infuraSolanaHTTP = "https://solana-mainnet.infura.io/v3/"

func infuraHTTPURL(chainID, apiKey string) (string, bool) {
	net, ok := infuraNetworks[normalizeChainKey(chainID)]
	if !ok || apiKey == "" {
		return "", false
	}
	return net.http + apiKey, true
}

func infuraWSURL(chainID, apiKey string) (string, bool) {
	net, ok := infuraNetworks[normalizeChainKey(chainID)]
	if !ok || apiKey == "" {
		return "", false
	}
	return net.ws + apiKey, true
}

func InfuraReadDialURL(chainID, apiKey string) string {
	if u, ok := infuraWSURL(chainID, apiKey); ok {
		return u
	}
	if u, ok := infuraHTTPURL(chainID, apiKey); ok {
		return u
	}
	return ""
}

func infuraSolanaURL(apiKey string) (string, bool) {
	if apiKey == "" {
		return "", false
	}
	return infuraSolanaHTTP + apiKey, true
}
