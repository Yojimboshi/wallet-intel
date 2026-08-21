package safety

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/ethereum/go-ethereum/common"
)

const goPlusBase = "https://api.gopluslabs.io/api/v1/token_security"

type GoPlus struct {
	cfg    Config
	client *http.Client
}

func (g *GoPlus) Check(ctx context.Context, chainCfg chain.Config, token common.Address) (Result, error) {
	chainID, ok := goPlusChainID(chainCfg)
	if !ok {
		if g.cfg.FailClosed {
			return Result{}, fmt.Errorf("goplus unsupported chain %s", chainCfg.ID)
		}
		return Result{OK: true, Reason: "goplus unsupported chain"}, nil
	}

	if g.client == nil {
		g.client = &http.Client{Timeout: 12 * time.Second}
	}

	url := fmt.Sprintf("%s/%d?contract_addresses=%s", goPlusBase, chainID, strings.ToLower(token.Hex()))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Result{}, err
	}

	resp, err := g.client.Do(req)
	if err != nil {
		if g.cfg.FailClosed {
			return Result{}, fmt.Errorf("goplus request: %w", err)
		}
		return Result{OK: true, Reason: "goplus unavailable"}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if g.cfg.FailClosed {
			return Result{}, fmt.Errorf("goplus status %d", resp.StatusCode)
		}
		return Result{OK: true, Reason: "goplus unavailable"}, nil
	}

	var payload struct {
		Code    int                       `json:"code"`
		Message string                    `json:"message"`
		Result  map[string]map[string]any `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		if g.cfg.FailClosed {
			return Result{}, fmt.Errorf("goplus decode: %w", err)
		}
		return Result{OK: true, Reason: "goplus decode error"}, nil
	}
	if payload.Code != 1 {
		if g.cfg.FailClosed {
			return Result{}, fmt.Errorf("goplus error: %s", payload.Message)
		}
		return Result{OK: true, Reason: "goplus error"}, nil
	}

	entry, ok := payload.Result[strings.ToLower(token.Hex())]
	if !ok || len(entry) == 0 {
		if g.cfg.FailClosed {
			return Result{}, fmt.Errorf("goplus no data for token")
		}
		return Result{OK: true, Reason: "goplus no data"}, nil
	}

	raw := parseGoPlusEntry(entry)
	return g.cfg.Evaluate(raw), nil
}

func goPlusChainID(chainCfg chain.Config) (int, bool) {
	switch chainCfg.ID {
	case chain.Ethereum:
		return 1, true
	case chain.BSC:
		return 56, true
	case chain.Base:
		return 8453, true
	default:
		return 0, false
	}
}

func goPlusBool(entry map[string]any, key string) bool {
	v, ok := entry[key]
	if !ok || v == nil {
		return false
	}
	switch t := v.(type) {
	case string:
		return t == "1" || strings.EqualFold(t, "true")
	case float64:
		return t == 1
	default:
		return false
	}
}

func goPlusPct(entry map[string]any, key string) float64 {
	v, ok := entry[key]
	if !ok || v == nil {
		return 0
	}
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
