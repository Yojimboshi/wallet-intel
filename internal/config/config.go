package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	DefaultPath            = "config/local.json"
	WatchPath              = "config/watch.json"
	ExecutionWalletsPath   = "config/execution-wallets.json"
	CollectorsPath         = "config/collectors.json"
)

// Local — secrets, RPC, rules, execution limits.
type Local struct {
	RPC              RPCFile `json:"rpc"`
	RPCURL           string  `json:"rpcUrl"`
	RPCWSURL         string  `json:"rpcWsUrl"`
	TelegramBotToken string  `json:"telegramBotToken"`
	TelegramChatID   string  `json:"telegramChatId"`
	TradeLog         string  `json:"tradeLog"`

	Database Database `json:"database"`
	Rules     Rules         `json:"rules"`
	Execution ExecutionFile `json:"execution"`
}

// Database configures optional MySQL persistence for trades, events, positions, seen tokens.
type Database struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Name     string `json:"name"`
	Timezone string `json:"timezone"` // e.g. Asia/Shanghai; default local display in MySQL
}

func (d Database) Location() *time.Location {
	tz := strings.TrimSpace(d.Timezone)
	if tz == "" || strings.EqualFold(tz, "local") {
		return time.Local
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.Local
	}
	return loc
}

func (d Database) DSN() string {
	host := strings.TrimSpace(d.Host)
	if host == "" {
		host = "127.0.0.1"
	}
	port := d.Port
	if port <= 0 {
		port = 3306
	}
	name := strings.TrimSpace(d.Name)
	if name == "" {
		name = "wallet_intel"
	}
	user := strings.TrimSpace(d.User)
	if user == "" {
		user = "root"
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&loc=%s&charset=utf8mb4",
		user, d.Password, host, port, name, url.QueryEscape(d.Location().String()))
}

type RPCFile struct {
	Read       RPCEndpoints      `json:"read"`
	ReadChains map[string]string `json:"readChains"`
	Execution  map[string]string `json:"execution"`
	Solana     RPCEndpoints      `json:"solana"`
}

type RPCEndpoints struct {
	InfuraAPIKey string `json:"infuraApiKey"`
	RPCURL       string `json:"rpcUrl"`
	RPCWSURL     string `json:"rpcWsUrl"`
}

const DefaultSolanaRPC = "https://api.mainnet-beta.solana.com"

type Wallet struct {
	Address string   `json:"address"`
	Label   string   `json:"label"`
	Chains  []string `json:"chains"`
	Copy    bool     `json:"copy"`
}

type Rules struct {
	AlertOn           []string `json:"alertOn"`
	MinBuyUsd         float64  `json:"minBuyUsd"`
	MaxMarketCapUsd   float64  `json:"maxMarketCapUsd"`
	MinLiquidityUsd   float64  `json:"minLiquidityUsd"`
	BatchBuyWindowSec int      `json:"batchBuyWindowSec"`
	BatchBuyMaxLegs   int      `json:"batchBuyMaxLegs"`
	// Flip-loop bait guard (defaults applied when building FlipGuard).
	FlipRecentSellBlockSec int      `json:"flipRecentSellBlockSec"` // skip copy after recent sell (default 900)
	FlipCycleWindowSec     int      `json:"flipCycleWindowSec"`     // window to count buy→sell cycles (default 1800)
	FlipMuteAfterCycles    int      `json:"flipMuteAfterCycles"`    // mute TG after N cycles (default 2)
	FlipMuteSec            int      `json:"flipMuteSec"`            // mute duration (default 1800)
	Chains                 []string `json:"chains"`
}

type ExecutionFile struct {
	AllowLiveExecution bool              `json:"allowLiveExecution"`
	Provider           string            `json:"provider"`
	WalletsFile        string            `json:"walletsFile"`
	ActiveWallet       string            `json:"activeWallet"`
	WalletAddress      string            `json:"walletAddress"`
	PrivateKey         string            `json:"privateKey"`
	CopyUsd            float64           `json:"copyUsd"`
	GasReserveUsd      float64           `json:"gasReserveUsd"`
	GasBufferUsd       float64           `json:"gasBufferUsd"` // legacy alias for gasReserveUsd
	RotateWallets      bool              `json:"rotateWallets"`
	NativeUsdPrice     map[string]float64 `json:"nativeUsdPrice"`
	MinExecuteUsd      float64           `json:"minExecuteUsd"`
	MaxExecuteUsd      float64           `json:"maxExecuteUsd"`
	MaxDailyExecuteUsd float64           `json:"maxDailyExecuteUsd"`
	MaxOpenPositions   int               `json:"maxOpenPositions"`
	CopySell           bool              `json:"copySell"`
	SlippageBps        int               `json:"slippageBps"`
	ExitSlippageBps    int               `json:"exitSlippageBps"`
	SimulateSwaps      bool              `json:"simulateSwaps"`
	V2Routers          map[string]string `json:"v2Routers"`
	Rules              ExecutionRules    `json:"rules"`
	Exit               ExitPolicy        `json:"exit"`
	Safety             SafetyFile        `json:"safety"`
}

type FundedWallet struct {
	Label      string
	Address    common.Address
	PrivateKey string
}

type ExecutionWallet struct {
	Label      string `json:"label"`
	Address    string `json:"address"`
	PrivateKey string `json:"privateKey"`
}

type ExecutionWalletsFile struct {
	EVM []ExecutionWallet `json:"evm"`
	SVM []ExecutionWallet `json:"svm"`
}

type CollectorsFile struct {
	EVM string `json:"evm"`
	SVM string `json:"svm"`
}

type WatchedWallet struct {
	Address common.Address
	Label   string
	Chains  map[string]struct{}
	Copy    bool
}

type ExecutionConfig struct {
	AllowLiveExecution bool
	Provider           string
	ActiveWallet       string
	WalletAddress      common.Address
	PrivateKey         string
	CopyUsd            float64
	GasReserveUsd      float64
	GasBufferUsd       float64 // same as GasReserveUsd
	RotateWallets      bool
	NativeUsdPrice     float64
	MinExecuteUsd      float64
	MaxExecuteUsd      float64
	MaxDailyExecuteUsd float64
	MaxOpenPositions   int
	CopySell           bool
	SlippageBps        int
	ExitSlippageBps    int
	SimulateSwaps      bool
	V2Router           common.Address
	EVMWallets         int
	SVMWallets         int
	Wallets            []FundedWallet
	CollectorEVM       common.Address
	CollectorSVM       string
	ExecRules          ExecutionRules
	Exit               ExitPolicy
	Safety             SafetyFile
}

func Load(path string) (Local, error) {
	if path == "" {
		path = DefaultPath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Local{}, fmt.Errorf("read %s: %w", path, err)
	}

	var cfg Local
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Local{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := cfg.validate(); err != nil {
		return Local{}, err
	}
	if cfg.TradeLog == "" {
		cfg.TradeLog = "data/trades.jsonl"
	}
	return cfg, nil
}

func (c Local) validate() error {
	if strings.TrimSpace(c.InfuraAPIKey()) == "" && strings.TrimSpace(c.ReadHTTPURL("ethereum")) == "" {
		return fmt.Errorf("%s: rpc.read.infuraApiKey is required", DefaultPath)
	}
	if c.Execution.MinExecuteUsd < 0 || c.Execution.MaxExecuteUsd < 0 || c.Execution.MaxDailyExecuteUsd < 0 {
		return fmt.Errorf("execution usd limits must be >= 0")
	}
	if c.Execution.MaxExecuteUsd > 0 && c.Execution.MinExecuteUsd > c.Execution.MaxExecuteUsd {
		return fmt.Errorf("execution.minExecuteUsd cannot exceed maxExecuteUsd")
	}
	return nil
}

func (c Local) InfuraAPIKey() string {
	return strings.TrimSpace(c.RPC.Read.InfuraAPIKey)
}

func (c Local) ReadHTTPURL(chainID string) string {
	if k := c.InfuraAPIKey(); k != "" {
		if u, ok := infuraHTTPURL(chainID, k); ok {
			return u
		}
	}
	if c.RPC.ReadChains != nil {
		if u := strings.TrimSpace(c.RPC.ReadChains[normalizeChainKey(chainID)]); u != "" {
			return u
		}
	}
	if u := strings.TrimSpace(c.RPC.Read.RPCURL); u != "" {
		return u
	}
	return strings.TrimSpace(c.RPCURL)
}

func (c Local) ReadDialURLForChain(chainID string) string {
	if k := c.InfuraAPIKey(); k != "" {
		if u, ok := infuraWSURL(chainID, k); ok {
			return u
		}
		if u, ok := infuraHTTPURL(chainID, k); ok {
			return u
		}
	}
	if c.RPC.ReadChains != nil {
		if u := strings.TrimSpace(c.RPC.ReadChains[normalizeChainKey(chainID)]); u != "" {
			return u
		}
	}
	return c.ReadDialURL()
}

func (c Local) ReadDialURL() string {
	return c.ReadDialURLForChain("ethereum")
}

func (c Local) ExecutionRPCURL(chainID string) (string, error) {
	key := normalizeChainKey(chainID)
	if c.RPC.Execution == nil {
		return "", fmt.Errorf("no execution rpc configured for %s", chainID)
	}
	for _, k := range []string{key, chainID} {
		if u := strings.TrimSpace(c.RPC.Execution[strings.ToLower(k)]); u != "" {
			return u, nil
		}
	}
	return "", fmt.Errorf("no execution rpc configured for %s", chainID)
}

func (c Local) SolanaRPCURL() string {
	if u := strings.TrimSpace(c.RPC.Solana.RPCURL); u != "" {
		return u
	}
	if k := c.InfuraAPIKey(); k != "" {
		if u, ok := infuraSolanaURL(k); ok {
			return u
		}
	}
	return DefaultSolanaRPC
}

func (c Local) RPCStatusLine(chainID string) string {
	execRPC := "not set"
	if u, err := c.ExecutionRPCURL(chainID); err == nil {
		execRPC = redactRPCURL(u)
	}
	solanaRPC := redactRPCURL(c.SolanaRPCURL())
	if strings.TrimSpace(c.RPC.Solana.RPCURL) == "" && c.InfuraAPIKey() != "" {
		solanaRPC += " (infura)"
	} else if strings.TrimSpace(c.RPC.Solana.RPCURL) == "" {
		solanaRPC += " (default)"
	}
	return fmt.Sprintf(
		"rpc read[%s]=%s | exec[%s]=%s | solana=%s",
		normalizeChainKey(chainID),
		redactRPCURL(c.ReadDialURLForChain(chainID)),
		normalizeChainKey(chainID),
		execRPC,
		solanaRPC,
	)
}

func normalizeChainKey(id string) string {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "eth", "mainnet":
		return "ethereum"
	case "arb":
		return "arbitrum"
	case "bnb", "binance":
		return "bsc"
	default:
		return strings.ToLower(strings.TrimSpace(id))
	}
}

func redactRPCURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "not set"
	}
	if i := strings.Index(raw, "://"); i >= 0 {
		host := raw[i+3:]
		if j := strings.Index(host, "/"); j >= 0 {
			host = host[:j]
		}
		if j := strings.Index(host, "@"); j >= 0 {
			host = host[j+1:]
		}
		return host
	}
	return raw
}

type WatchFile struct {
	Wallets []Wallet `json:"wallets"`
}

func LoadWatch(path string) ([]WatchedWallet, error) {
	if path == "" {
		path = WatchPath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var file WatchFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(file.Wallets) == 0 {
		return nil, fmt.Errorf("%s: no wallets configured", path)
	}

	out := make([]WatchedWallet, 0, len(file.Wallets))
	for _, w := range file.Wallets {
		addr := common.HexToAddress(w.Address)
		if addr == (common.Address{}) {
			return nil, fmt.Errorf("%s: invalid address %s", path, w.Address)
		}
		chains := make(map[string]struct{}, len(w.Chains))
		for _, ch := range w.Chains {
			chains[strings.ToLower(ch)] = struct{}{}
		}
		label := w.Label
		if label == "" {
			label = shortAddr(addr)
		}
		out = append(out, WatchedWallet{
			Address: addr,
			Label:   label,
			Chains:  chains,
			Copy:    w.Copy,
		})
	}
	return out, nil
}

func LoadExecutionWallets(path string) (ExecutionWalletsFile, error) {
	if path == "" {
		path = ExecutionWalletsPath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ExecutionWalletsFile{}, fmt.Errorf("read %s: %w", path, err)
	}

	var file ExecutionWalletsFile
	if err := json.Unmarshal(data, &file); err != nil {
		return ExecutionWalletsFile{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := file.validate(); err != nil {
		return ExecutionWalletsFile{}, fmt.Errorf("%s: %w", path, err)
	}
	return file, nil
}

func LoadCollectors(path string) (CollectorsFile, error) {
	if path == "" {
		path = CollectorsPath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return CollectorsFile{}, fmt.Errorf("read %s: %w", path, err)
	}

	var file CollectorsFile
	if err := json.Unmarshal(data, &file); err != nil {
		return CollectorsFile{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := file.validate(); err != nil {
		return CollectorsFile{}, fmt.Errorf("%s: %w", path, err)
	}
	return file, nil
}

func (f CollectorsFile) validate() error {
	evm := strings.TrimSpace(f.EVM)
	if evm != "" {
		if common.HexToAddress(evm) == (common.Address{}) {
			return fmt.Errorf("invalid evm collector address: %s", evm)
		}
	}
	if strings.TrimSpace(f.SVM) == "" {
		return fmt.Errorf("svm collector address is required")
	}
	if evm == "" {
		return fmt.Errorf("evm collector address is required")
	}
	return nil
}

func (f ExecutionWalletsFile) validate() error {
	seen := make(map[string]struct{})
	for _, pool := range []struct {
		name string
		list []ExecutionWallet
	}{
		{"evm", f.EVM},
		{"svm", f.SVM},
	} {
		for _, w := range pool.list {
			label := strings.TrimSpace(w.Label)
			if label == "" {
				return fmt.Errorf("%s wallet missing label", pool.name)
			}
			if _, dup := seen[label]; dup {
				return fmt.Errorf("duplicate wallet label %q", label)
			}
			seen[label] = struct{}{}

			addr := common.HexToAddress(w.Address)
			if pool.name == "evm" {
				if addr == (common.Address{}) {
					return fmt.Errorf("evm wallet %q: invalid address", label)
				}
				if err := validatePrivateKey(w.PrivateKey); err != nil {
					return fmt.Errorf("evm wallet %q: %w", label, err)
				}
				if err := verifyKeyMatchesAddress(w.PrivateKey, addr); err != nil {
					return fmt.Errorf("evm wallet %q: %w", label, err)
				}
			} else if strings.TrimSpace(w.Address) == "" || strings.TrimSpace(w.PrivateKey) == "" {
				return fmt.Errorf("svm wallet %q: address and privateKey required", label)
			}
		}
	}
	return nil
}

func (f ExecutionWalletsFile) Pick(label, chainID string) (ExecutionWallet, error) {
	label = strings.TrimSpace(label)
	list := f.EVM
	if strings.EqualFold(chainID, "solana") || strings.EqualFold(chainID, "svm") {
		list = f.SVM
	}
	if len(list) == 0 {
		return ExecutionWallet{}, fmt.Errorf("no %s execution wallets configured", chainID)
	}
	if label == "" {
		return list[0], nil
	}
	for _, w := range list {
		if strings.EqualFold(w.Label, label) {
			return w, nil
		}
	}
	return ExecutionWallet{}, fmt.Errorf("execution wallet %q not found", label)
}

func (c Local) ExecutionConfig(chainID string) (ExecutionConfig, error) {
	ex := c.Execution
	provider := strings.ToLower(strings.TrimSpace(ex.Provider))
	if provider == "" {
		provider = "direct"
	}
	cfg := ExecutionConfig{
		AllowLiveExecution: ex.AllowLiveExecution,
		Provider:           provider,
		CopyUsd:            ex.CopyUsd,
		GasReserveUsd:      ex.GasReserveUsd,
		GasBufferUsd:       ex.GasBufferUsd,
		RotateWallets:      ex.RotateWallets,
		MinExecuteUsd:      ex.MinExecuteUsd,
		MaxExecuteUsd:      ex.MaxExecuteUsd,
		MaxDailyExecuteUsd: ex.MaxDailyExecuteUsd,
		MaxOpenPositions:   ex.MaxOpenPositions,
		CopySell:           ex.CopySell,
		SlippageBps:        ex.SlippageBps,
		ExitSlippageBps:    ex.ExitSlippageBps,
		SimulateSwaps:      ex.SimulateSwaps,
	}
	if cfg.CopyUsd <= 0 {
		cfg.CopyUsd = ex.MaxExecuteUsd
	}
	if cfg.GasReserveUsd <= 0 {
		cfg.GasReserveUsd = ex.GasBufferUsd
	}
	if cfg.GasReserveUsd <= 0 {
		cfg.GasReserveUsd = 2 // ~40 ETH txs at $0.05, or hundreds on BSC
	}
	cfg.GasBufferUsd = cfg.GasReserveUsd
	if !ex.RotateWallets && ex.WalletsFile != "" {
		cfg.RotateWallets = true // default on when using wallet pool file
	}
	if ex.RotateWallets {
		cfg.RotateWallets = true
	}
	if ex.NativeUsdPrice != nil {
		if p, ok := ex.NativeUsdPrice[strings.ToLower(chainID)]; ok {
			cfg.NativeUsdPrice = p
		}
		if cfg.NativeUsdPrice <= 0 {
			if p, ok := ex.NativeUsdPrice[normalizeChainKey(chainID)]; ok {
				cfg.NativeUsdPrice = p
			}
		}
	}
	if cfg.SlippageBps <= 0 {
		cfg.SlippageBps = 500
	}
	if cfg.ExitSlippageBps <= 0 {
		cfg.ExitSlippageBps = cfg.SlippageBps * 5
	}
	if cfg.ExitSlippageBps < cfg.SlippageBps {
		cfg.ExitSlippageBps = cfg.SlippageBps
	}
	if ex.V2Routers != nil {
		if addr, ok := ex.V2Routers[strings.ToLower(chainID)]; ok {
			cfg.V2Router = common.HexToAddress(addr)
		}
	}
	if cfg.V2Router == (common.Address{}) {
		if c, ok := chain.ByID(chainID); ok && c.V2Router != (common.Address{}) {
			cfg.V2Router = c.V2Router
		}
	}

	addrStr := strings.TrimSpace(ex.WalletAddress)
	privKey := strings.TrimSpace(ex.PrivateKey)
	walletsFile, walletsLoaded := ExecutionWalletsFile{}, false

	if addrStr == "" || privKey == "" {
		path := strings.TrimSpace(ex.WalletsFile)
		if path == "" {
			path = ExecutionWalletsPath
		}
		var err error
		walletsFile, err = LoadExecutionWallets(path)
		if err != nil {
			return ExecutionConfig{}, err
		}
		walletsLoaded = true
		picked, err := walletsFile.Pick(ex.ActiveWallet, chainID)
		if err != nil {
			return ExecutionConfig{}, err
		}
		addrStr = picked.Address
		privKey = picked.PrivateKey
		cfg.ActiveWallet = picked.Label
	}

	if walletsLoaded {
		cfg.EVMWallets = len(walletsFile.EVM)
		cfg.SVMWallets = len(walletsFile.SVM)
		cfg.Wallets = fundedWalletsFromFile(walletsFile.EVM)
	} else if path := strings.TrimSpace(ex.WalletsFile); path != "" {
		if walletsFile, err := LoadExecutionWallets(path); err == nil {
			cfg.EVMWallets = len(walletsFile.EVM)
			cfg.SVMWallets = len(walletsFile.SVM)
			cfg.Wallets = fundedWalletsFromFile(walletsFile.EVM)
		}
	}

	if addrStr != "" {
		cfg.WalletAddress = common.HexToAddress(addrStr)
		if cfg.WalletAddress == (common.Address{}) {
			return ExecutionConfig{}, fmt.Errorf("invalid execution wallet address: %s", addrStr)
		}
	}

	cfg.PrivateKey = privKey
	if cfg.PrivateKey != "" {
		if err := validatePrivateKey(cfg.PrivateKey); err != nil {
			return ExecutionConfig{}, fmt.Errorf("execution private key: %w", err)
		}
		if cfg.WalletAddress != (common.Address{}) {
			if err := verifyKeyMatchesAddress(cfg.PrivateKey, cfg.WalletAddress); err != nil {
				return ExecutionConfig{}, err
			}
		}
	}

	collectors, err := LoadCollectors(CollectorsPath)
	if err != nil {
		return ExecutionConfig{}, err
	}
	cfg.CollectorEVM = common.HexToAddress(collectors.EVM)
	cfg.CollectorSVM = collectors.SVM

	cfg.ExecRules = ex.Rules.withDefaults(c.Rules)
	cfg.Exit = ex.Exit.WithDefaults()
	cfg.Safety = applySafetyDefaults(ex.Safety)

	return cfg, nil
}

func applySafetyDefaults(s SafetyFile) SafetyFile {
	if !s.Enabled {
		return s
	}
	if s.MaxBuyTaxPct <= 0 {
		s.MaxBuyTaxPct = 10
	}
	if s.MaxSellTaxPct <= 0 {
		s.MaxSellTaxPct = 10
	}
	if !s.BlockHoneypot && !s.BlockMintable {
		s.BlockHoneypot = true
		s.BlockMintable = true
	}
	if !s.BlockTransferPausable && !s.BlockUnlockedLP && !s.BlockCannotSell {
		s.BlockTransferPausable = true
		s.BlockUnlockedLP = true
		s.BlockCannotSell = true
	}
	if !s.FailClosed {
		s.FailClosed = true
	}
	return s
}

func (r Rules) AlertsOn(side string) bool {
	side = strings.ToLower(side)
	for _, s := range r.AlertOn {
		if strings.ToLower(s) == side {
			return true
		}
	}
	return false
}

func fundedWalletsFromFile(list []ExecutionWallet) []FundedWallet {
	out := make([]FundedWallet, 0, len(list))
	for _, w := range list {
		out = append(out, FundedWallet{
			Label:      w.Label,
			Address:    common.HexToAddress(w.Address),
			PrivateKey: w.PrivateKey,
		})
	}
	return out
}

func (c ExecutionConfig) CanExecute(usdAmount float64) (bool, string) {
	return c.CanExecuteFor(FundedWallet{Address: c.WalletAddress, PrivateKey: c.PrivateKey}, usdAmount)
}

func (c ExecutionConfig) CanExecuteFor(w FundedWallet, usdAmount float64) (bool, string) {
	if !c.AllowLiveExecution {
		return false, "execution.allowLiveExecution is false"
	}
	addr := w.Address
	if addr == (common.Address{}) {
		addr = c.WalletAddress
	}
	privKey := w.PrivateKey
	if privKey == "" {
		privKey = c.PrivateKey
	}
	if addr == (common.Address{}) {
		return false, "execution wallet address is not set"
	}
	if privKey == "" {
		return false, "execution private key is not set"
	}
	if usdAmount <= 0 {
		return false, "execution amount must be positive"
	}
	if c.MinExecuteUsd > 0 && usdAmount < c.MinExecuteUsd {
		return false, fmt.Sprintf("blocked: $%.2f below minExecuteUsd $%.2f", usdAmount, c.MinExecuteUsd)
	}
	if c.MaxExecuteUsd > 0 && usdAmount > c.MaxExecuteUsd {
		return false, fmt.Sprintf("blocked: $%.2f exceeds maxExecuteUsd $%.2f", usdAmount, c.MaxExecuteUsd)
	}
	return true, ""
}

func (c ExecutionConfig) verifyKeyMatchesAddress() error {
	return verifyKeyMatchesAddress(c.PrivateKey, c.WalletAddress)
}

func verifyKeyMatchesAddress(privateKey string, walletAddress common.Address) error {
	key, err := crypto.HexToECDSA(strings.TrimPrefix(privateKey, "0x"))
	if err != nil {
		return fmt.Errorf("invalid private key: %w", err)
	}
	derived := crypto.PubkeyToAddress(key.PublicKey)
	if derived != walletAddress {
		return fmt.Errorf("private key does not match address (derived %s)", derived.Hex())
	}
	return nil
}

func validatePrivateKey(key string) error {
	key = strings.TrimPrefix(strings.TrimSpace(key), "0x")
	if len(key) != 64 {
		return fmt.Errorf("expected 64 hex characters")
	}
	if _, err := crypto.HexToECDSA(key); err != nil {
		return fmt.Errorf("invalid hex: %w", err)
	}
	return nil
}

func (c ExecutionConfig) StatusLine() string {
	mode := "off"
	if c.AllowLiveExecution {
		mode = "armed"
	}
	addr := "not set"
	if c.WalletAddress != (common.Address{}) {
		addr = c.WalletAddress.Hex()
	}
	key := "not set"
	if c.PrivateKey != "" {
		key = "set"
	}
	v2Router := "default"
	if c.V2Router != (common.Address{}) {
		v2Router = c.V2Router.Hex()
	}
	return fmt.Sprintf(
		"execution %s | provider=%s | wallet=%s (%s) | key=%s | pool evm=%d svm=%d | collector evm=%s svm=%s | v2=%s | per-tx=$%.0f–$%.0f | daily=$%.0f",
		mode, c.Provider, addr, c.ActiveWallet, key, c.EVMWallets, c.SVMWallets, c.CollectorEVM.Hex(), shortSVM(c.CollectorSVM), v2Router, c.MinExecuteUsd, c.MaxExecuteUsd, c.MaxDailyExecuteUsd,
	)
}

func shortSVM(addr string) string {
	if len(addr) <= 12 {
		return addr
	}
	return addr[:6] + "…" + addr[len(addr)-4:]
}

func shortAddr(addr common.Address) string {
	s := addr.Hex()
	return s[:6] + "…" + s[len(s)-4:]
}
