package safety

import (
	"context"
	"fmt"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/ethereum/go-ethereum/common"
)

type Config struct {
	Enabled               bool
	MaxBuyTaxPct          float64
	MaxSellTaxPct         float64
	BlockHoneypot         bool
	BlockMintable         bool
	BlockTransferPausable bool
	BlockUnlockedLP       bool
	BlockCannotSell       bool
	FailClosed            bool
}

type Result struct {
	OK         bool
	Reason     string
	BuyTaxPct  float64
	SellTaxPct float64
	Honeypot   bool
	Mintable   bool
}

type Checker interface {
	Check(ctx context.Context, chainCfg chain.Config, token common.Address) (Result, error)
}

func New(cfg Config) Checker {
	if !cfg.Enabled {
		return Nop{}
	}
	if cfg.MaxBuyTaxPct <= 0 {
		cfg.MaxBuyTaxPct = 10
	}
	if cfg.MaxSellTaxPct <= 0 {
		cfg.MaxSellTaxPct = 10
	}
	return &GoPlus{cfg: cfg}
}

type Nop struct{}

func (Nop) Check(context.Context, chain.Config, common.Address) (Result, error) {
	return Result{OK: true}, nil
}

func (c Config) Evaluate(raw TokenRaw) Result {
	base := Result{BuyTaxPct: raw.BuyTaxPct, SellTaxPct: raw.SellTaxPct, Honeypot: raw.Honeypot, Mintable: raw.Mintable}
	if raw.Honeypot && c.BlockHoneypot {
		base.OK = false
		base.Reason = "honeypot"
		return base
	}
	if raw.CannotSell && c.BlockCannotSell {
		base.OK = false
		base.Reason = "cannot sell"
		return base
	}
	if raw.TransferPausable && c.BlockTransferPausable {
		base.OK = false
		base.Reason = "transfer pausable"
		return base
	}
	if raw.UnlockedLP && c.BlockUnlockedLP {
		base.OK = false
		base.Reason = "unlocked lp"
		return base
	}
	if raw.Mintable && c.BlockMintable {
		base.OK = false
		base.Reason = "mintable"
		base.Mintable = true
		return base
	}
	if c.MaxBuyTaxPct > 0 && raw.BuyTaxPct > c.MaxBuyTaxPct {
		base.OK = false
		base.Reason = fmt.Sprintf("buy tax %.1f%% > max %.1f%%", raw.BuyTaxPct, c.MaxBuyTaxPct)
		return base
	}
	if c.MaxSellTaxPct > 0 && raw.SellTaxPct > c.MaxSellTaxPct {
		base.OK = false
		base.Reason = fmt.Sprintf("sell tax %.1f%% > max %.1f%%", raw.SellTaxPct, c.MaxSellTaxPct)
		return base
	}
	base.OK = true
	return base
}

type TokenRaw struct {
	Honeypot         bool
	Mintable         bool
	TransferPausable bool
	CannotSell       bool
	UnlockedLP       bool
	BuyTaxPct        float64
	SellTaxPct       float64
}
