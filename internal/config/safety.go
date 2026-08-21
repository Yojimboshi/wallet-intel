package config

import "github.com/Yojimboshi/wallet-intel/internal/safety"

func (s SafetyFile) ToSafetyConfig() safety.Config {
	return safety.Config{
		Enabled:               s.Enabled,
		MaxBuyTaxPct:          s.MaxBuyTaxPct,
		MaxSellTaxPct:         s.MaxSellTaxPct,
		BlockHoneypot:         s.BlockHoneypot,
		BlockMintable:         s.BlockMintable,
		BlockTransferPausable: s.BlockTransferPausable,
		BlockUnlockedLP:       s.BlockUnlockedLP,
		BlockCannotSell:       s.BlockCannotSell,
		FailClosed:            s.FailClosed,
	}
}
