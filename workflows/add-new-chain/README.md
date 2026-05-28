# add-new-chain

Kilroy workflow for adding a new blockchain network to **atomic-swap-app** with chain-type-aware branching.

## Prerequisites

- Target repo checked out at `dev` **after** merging PR #1487 (DEV-2787) and PR #1493 (DEV-2986)
- `bun` installed
- Reference branches available: `origin/feature/solana`, `origin/feature/canton`

## Chain type taxonomy

| `chain_type` | Default reference | Pattern |
|--------------|-------------------|---------|
| `utxo` | bitcoin | HTLC-BTC, UTXO providers, CSV locktime |
| `evm` | ethereum | HTLC-EVM, viem, ERC-20 env addresses |
| `tvm` | tron | HTLC-TRON, TronGrid |
| `svm` | solana | Anchor program, RPC event decoding |
| `ledger` | canton | DAML contracts, ledger API watcher |
| `unknown` | — | Research gate before implementation |

## Launch (Cursor Composer 2.5 Fast, max mode)

Ensure `/root/workspace/arqitech/arqilroy/.env` contains:

- `CURSOR_API_KEY` — Cursor Agent SDK key
- `KILROY_CURSOR_MAX_MODE=true` — enables max mode on cursor agent runs

Build the cursor bridge once:

```bash
cd /root/workspace/arqitech/arqilroy/scripts/cursor-agent && npm install && npm run build
cd /root/workspace/arqitech/arqilroy && go build -o ./kilroy ./cmd/kilroy
```

### Zcash (UTXO, T-addresses only, all routes)

```bash
cd /root/workspace/arqitech/arqilroy

./kilroy attractor run \
  --detach \
  --tmux \
  --skip-cli-headless-warning \
  --package workflows/add-new-chain/ \
  --config workflows/add-new-chain/run.yaml \
  --workspace /root/workspace/arqitech/atomic-swap-app \
  --input workflows/add-new-chain/examples/zcash-t-addresses.json \
  --force-model cursor=composer-2.5-fast \
  --run-id add-zcash-taddr \
  --logs-root /root/workspace/arqitech/arqilroy/logs/add-zcash-taddr \
  --label workflow=add-new-chain \
  --label chain=zcash \
  --label model=composer-2.5-fast
```

All graph nodes use `llm_provider: cursor` and `llm_model: composer-2.5-fast` via the graph stylesheet; `--force-model` is a safety pin.

Routes enabled by the example input:

| Sell | Buy |
|------|-----|
| ZEC | USDC, USDT |
| USDC | ZEC, USDT |
| USDT | ZEC, USDC |

### Generic chain

```bash
./kilroy attractor run \
  --package workflows/add-new-chain/ \
  --config workflows/add-new-chain/run.yaml \
  --workspace /path/to/atomic-swap-app \
  --tmux \
  --input '{
    "chain_name": "Zcash",
    "chain_type": "utxo",
    "network_id": "zcash",
    "native_asset": "ZEC",
    "supported_routes": {"ZEC":["USDC","USDT"],"USDC":["ZEC","USDT"],"USDT":["ZEC","USDC"]},
    "reference_chain": "bitcoin",
    "constraints": "T-addresses only; no shielded z-addresses."
  }'
```

## Inputs

| Key | Required | Description |
|-----|----------|-------------|
| `chain_name` | yes | Human-readable name |
| `chain_type` | yes | utxo, evm, tvm, svm, ledger, unknown |
| `network_id` | yes | NetworkType-style key |
| `native_asset` | yes | Native token symbol |
| `supported_routes` | yes | JSON sell→buy allow-list |
| `reference_chain` | no | Existing chain to clone (defaults by type) |
| `feature_flag` | no | Build-time gate env var |

## Outputs

- `.ai/runs/<run_id>/chain-integration-spec.md`
- `.ai/runs/<run_id>/chain-checklist.md`

## Reference analysis

This workflow was derived from diff analysis of:

- **feature/solana** (259 files): SVM integration — `escrow-solana/`, `lib/solana/*`, provider adapters, widened tx id columns
- **feature/canton** (366 files): Ledger integration — `services/canton/*`, DAML harnesses, WalletChain.canton, ledger offset AppState

Both branches touch the same layer stack: shared constants → prisma → backend services → client wallet/htlc → mocknet → tests/skills.
