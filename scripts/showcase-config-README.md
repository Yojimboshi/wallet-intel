# Config

Create these files locally (gitignored):

| File | Purpose |
|------|---------|
| `local.json` | RPC, Telegram, rules, execution, optional MySQL |
| `watch.json` | Watched wallet addresses |
| `execution-wallets.json` | Bot wallets (EVM/SVM) |
| `collectors.json` | Sweep destination addresses |

See `internal/config/config.go` for field names and defaults.
