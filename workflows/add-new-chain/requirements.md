# Workflow: add-new-chain

Create a Kilroy Attractor workflow package named `add-new-chain` for the **atomic-swap-app** monorepo. The workflow adds a new blockchain network to the cross-chain atomic swap platform.

## Target repository

`/root/workspace/arqitech/atomic-swap-app` (Arqitech-Inc/atomic-swap-app)

## Baseline assumptions (post-merge)

Assume `dev` includes merged PR #1487 (DEV-2787 chronological base/match) and PR #1493 (DEV-2986 remove reversed-direction fork). The workflow MUST NOT reintroduce:
- `isReversedDirectionTrade()` or `VITE_ENABLE_REVERSED_DIRECTION`
- `match_only_*` FSM states
- Asset-role base/match encoding (seller asset → baseAsset)

All new chain work MUST use:
- `shared/src/utils/trade-lock-order.ts` for chronological column encoding
- `resolveTradeEconomicLegs(trade)` for seller/buyer/firstLock/secondLock
- `isBtcFirstLock(trade)` when routing BTC leg fields

## Reference implementations (MUST analyze before implementing)

Study these branches as canonical prior art:

### feature/solana (SVM / Solana VM — 259 files)
Chain archetype: **SVM** (Anchor program + RPC event decoding)

Key touchpoints:
- `shared/src/constants/assets.ts` — NETWORKS.SOLANA, ASSETS.SOL
- `shared/src/constants/blockchain.ts` — block time + match delay blocks
- `shared/src/utils/trade-route-validation.ts` — SOL gas token routes
- `shared/src/utils/asset-network-validation.ts` — valid combinations
- `apps/backend/prisma/schema.prisma` — AssetType.SOL, solana address fields, widened tx id columns (base58 signatures)
- `apps/backend/src/config/solana-network.ts`, `lib/solana/*` (RPC, event decoder, PDA, IDL)
- `apps/backend/src/services/providers/solana/*`, `solana-consensus.ts`
- `apps/backend/src/services/blockchain.service.ts`, `polling-scheduler.ts`, `transaction-detector.ts`, `tx-verifier.service.ts`, `tranche.service.ts`
- `apps/client/src/lib/blockchain-config.ts` — SolanaCluster types
- `apps/client/src/lib/htlc-solana/*` (or equivalent), wallet integration
- `escrow-solana/` — Anchor HTLC program
- `scripts/mocknet/seed-solana.ts`, `reset-solana.sh`, bootstrap changes

### feature/canton (Ledger/DAML — 366 files)
Chain archetype: **Ledger** (Canton Network + DAML contracts, NOT EVM/UTXO)

Key touchpoints:
- `shared/src/constants/assets.ts` — AMULET on CANTON network, `__CANTON_ENABLED__` flag
- `shared/src/utils/trade-route-validation.ts` — Canton route gating (USDC→AMULET)
- `apps/backend/prisma/schema.prisma` — WalletChain.canton, Canton leg fields on Tranche, AppState for ledger offset
- `apps/backend/src/services/canton/*` — allocation service, FSM handlers, ledger API watcher, operator command runner, gateway JWT/RPC
- Canton-specific tranche columns (allocationContractId, htlcContractId, etc.)
- `tools/canton-tier-a-harness/`, `tools/canton-tier-b-harness/` — DAML test harnesses
- Client Canton wallet verification, operator skills under `.claude/skills/user:unified-canton-*`

## Chain type taxonomy (workflow MUST branch on input)

| chain_type | Reference chain | Integration pattern |
|------------|-----------------|---------------------|
| `utxo` | bitcoin | HTLC via Bitcoin script; extend `htlc-btc`, UTXO providers, CSV locktime, address validation (bech32/base58), mocknet bitcoind |
| `evm` | ethereum | HTLC via Solidity; extend `htlc-evm`, viem chains, ERC-20 env addresses, EVM providers (Alchemy/Etherscan), preflight |
| `tvm` | tron | TronWeb HTLC; extend `htlc-tron`, TronGrid providers, TRC-20 env addresses |
| `svm` | solana | Anchor program in `escrow-{chain}/`; RPC providers; event decoding; base58 tx ids; wallet adapter |
| `ledger` | canton | DAML contracts; ledger API watcher; operator runner; party-id addresses; feature flag gating |
| `unknown` | none | Research phase: survey chain VM model, HTLC feasibility, existing SDKs; produce design doc before implementation |

Examples:
- **Zcash / Dash** → `chain_type=utxo`, reference `bitcoin` (may need shielded-address research)
- **Polygon / Base / Arbitrum** → `chain_type=evm`, reference `ethereum`
- **New SVM chain** → `chain_type=svm`, reference `solana`
- **No similar chain** → `chain_type=unknown`, mandatory research fan-out before implement

## Workflow inputs

| Input | Required | Description |
|-------|----------|-------------|
| `chain_name` | yes | Human name (e.g. "Solana", "Canton", "Zcash") |
| `chain_type` | yes | One of: utxo, evm, tvm, svm, ledger, unknown |
| `network_id` | yes | Internal network key (e.g. solana, canton, zcash) matching NetworkType enum style |
| `native_asset` | yes | Native token symbol (e.g. SOL, AMULET, ZEC) |
| `supported_routes` | yes | JSON describing allowed trade routes (sell→buy pairs) |
| `reference_chain` | no | Existing chain to clone patterns from (defaults by chain_type) |
| `feature_flag` | no | Build-time flag name if gated (e.g. CANTON_ENABLED) |

## Deliverables (per run)

1. `.ai/runs/$KILROY_RUN_ID/chain-integration-spec.md` — scoped integration plan
2. `.ai/runs/$KILROY_RUN_ID/chain-checklist.md` — file-by-file checklist derived from reference branch diff
3. Code changes across shared/backend/client/mocknet as appropriate for chain_type
4. Prisma migration(s) for new enums/columns
5. Tests: unit + integration for new network paths
6. `.claude/skills/user:*` operator skill if chain has non-trivial operator flows
7. `scripts/validate-build.sh`, `scripts/validate-test.sh`, `scripts/validate-fmt.sh` updated/created

## Topology requirements

Use template-first topology from `skills/create-dotfile/reference_template.dot`:

1. **Bootstrap**: check_toolchain → expand_spec → check_dod → build_dod (if needed)
2. **Analysis cluster** (required — porting existing patterns):
   - `analyze_fanout` → per-layer analyze nodes:
     - `analyze_shared` — constants, validation, trade routes, lock order
     - `analyze_backend` — prisma, services, providers, config
     - `analyze_client` — wallet, preflight, htlc lib, UI components
     - `analyze_mocknet` — bootstrap, seed scripts, test fixtures
     - `analyze_reference_branch` — diff against reference_chain branch
   - `merge_analysis` → synthesis doc
3. **Chain type router** (shape=diamond): branch to utxo/evm/tvm/svm/ledger/unknown paths
4. **Research gate** for `unknown` chain_type before implementation
5. **Implementation fan-out** by layer (shared → backend → client → mocknet), sequential dependency
6. **Verification cluster**: validate-build, validate-test, validate-fmt, verify_fidelity against checklist
7. **Postmortem loop** with stable failure_signature keys

## Model stylesheet

Use semicolon-separated declarations. Suggested:
- Default implementation: claude-sonnet-4.6 (anthropic)
- Analysis/reference diff: gemini-3-flash-preview (google)
- Hard/complex (DAML, Anchor programs): claude-opus-4.6 (anthropic)
- Verify nodes: claude-haiku-4.5 (anthropic)

## Non-goals

- Do not modify unrelated chains' behavior
- Do not change DEV-2787 chronological semantics
- Do not add reversed-direction flags or match_only states
- Production deployment or mainnet key management

## Goal statement for graph attr

Add a new blockchain network to atomic-swap-app following established patterns from feature/solana (SVM) and feature/canton (Ledger), with chain-type-aware branching for UTXO/EVM/TVM/SVM/Ledger/unknown chains, post DEV-2787/DEV-2986 baseline.
