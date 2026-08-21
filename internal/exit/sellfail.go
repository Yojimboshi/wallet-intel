package exit

import (
	"strings"

	"github.com/Yojimboshi/wallet-intel/internal/execute"
)

const maxExitSellTries = 4

func isUnsellableSellErr(err error) bool {
	if err == nil || execute.IsSlippageSellErr(err) || isNoBalanceSellErr(err) {
		return false
	}
	msg := strings.ToUpper(err.Error())
	return strings.Contains(msg, "TRANSFER_FROM_FAILED") ||
		strings.Contains(msg, "TRANSFERHELPER") ||
		strings.Contains(msg, "CANNOT SELL") ||
		strings.Contains(msg, "HONEYPOT") ||
		strings.Contains(msg, "UNABLE TO SELL")
}

func isNoBalanceSellErr(err error) bool {
	return execute.IsNoTokenBalanceErr(err)
}

func positionExitKey(chain, token string) string {
	return strings.ToLower(chain) + ":" + strings.ToLower(token)
}
