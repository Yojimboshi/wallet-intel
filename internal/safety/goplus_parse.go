package safety

import (
	"strconv"
	"strings"
)

func parseGoPlusEntry(entry map[string]any) TokenRaw {
	return TokenRaw{
		Honeypot:         goPlusBool(entry, "is_honeypot"),
		Mintable:         goPlusBool(entry, "is_mintable"),
		TransferPausable: goPlusBool(entry, "transfer_pausable"),
		CannotSell:       goPlusBool(entry, "cannot_sell"),
		UnlockedLP:       lpUnlocked(entry),
		BuyTaxPct:        goPlusPct(entry, "buy_tax"),
		SellTaxPct:       goPlusPct(entry, "sell_tax"),
	}
}

// lpUnlocked is true when most LP sits in an unlocked contract (not burn/dead lock).
func lpUnlocked(entry map[string]any) bool {
	raw, ok := entry["lp_holders"]
	if !ok {
		return false
	}
	list, ok := raw.([]any)
	if !ok || len(list) == 0 {
		return false
	}

	var topPct float64
	var topAddr string
	var topLocked bool
	var topContract bool
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		pct := goPlusFloat(m["percent"])
		if pct <= topPct {
			continue
		}
		topPct = pct
		topAddr = goPlusString(m["address"])
		topLocked = goPlusBool(m, "is_locked")
		topContract = goPlusBool(m, "is_contract")
	}
	if topPct < 0.5 {
		return false
	}
	if isBurnOrLockedLP(topAddr, topLocked) {
		return false
	}
	return topContract
}

func isBurnOrLockedLP(addr string, locked bool) bool {
	if locked {
		return true
	}
	a := strings.ToLower(strings.TrimSpace(addr))
	switch a {
	case "", "0x0000000000000000000000000000000000000000",
		"0x000000000000000000000000000000000000dead",
		"0x0000000000000000000000000000000000000001":
		return true
	}
	return strings.HasSuffix(a, "dead")
}

func goPlusString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		return ""
	}
}

func goPlusFloat(v any) float64 {
	switch t := v.(type) {
	case string:
		if t == "" {
			return 0
		}
		f, err := strconv.ParseFloat(t, 64)
		if err != nil {
			return 0
		}
		return f
	case float64:
		return t
	default:
		return 0
	}
}
