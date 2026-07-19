# ledger-viz

A visualisation of a double-entry ledger as a **transaction matrix with playback** —
rows are transactions (steps), columns are accounts (grouped by type), cells are the
signed Dr/Cr delta posted to each account at each step, with `Starting`/`Ending`
balance rows. A movable playhead steps forward/backward, accumulating running balances.

Inspired by Fragment's "Building a Multi-currency Ledger" grid, rendered in a clean
shadcn aesthetic.

## Layout (pnpm workspace)

```
ledger-viz/
  packages/ledger-model/   # headless, framework-free TS — THE SHARED LAYER
  apps/web/                # Next.js 15 + Tailwind v4 + shadcn-style renderer
```

### `@ledger-viz/model` — the shared headless layer

Pure TypeScript, no React. A future **CLI visualiser** consumes the exact same engine.

- **Data contract** (`LedgerFilm`): `{ meta, accounts[], steps[] }` where a `Step` carries
  `Posting[]` (`{ account, side: "Dr"|"Cr", amount }`, integer minor units).
- **`buildColumns(accounts)`** → grouped column tree (assets → liabilities → income → expense).
- **`buildFrames(film)`** → precomputed playback frames. `frames[0]` is the opening
  (all balances 0); `frames[i]` (i≥1) holds cumulative `balances` after step `i-1` plus
  that step's per-account `deltas`. Signed convention: **Dr = +, Cr = −** (debit-positive).
- **`Playback`** → `frameAt / clamp / next / prev / first / last` over index `[0..steps.length]`.
- **`formatMoney`**, **`loadFilm`** (runtime validation).

Playback is a pure function of `(film, index)` — the UI only holds one number.

### Fixtures

`packages/ledger-model/src/fixtures/*.film.json` are real ledger-flow runs
(`dtc/2026-05` clean, `dtc/2026-03` break period), generated from the journal. A future
`ledger-flow` subcommand can emit this `LedgerFilm` JSON directly.

## Run

```sh
cd ledger-viz
pnpm install
pnpm -F web dev        # http://localhost:3000
# or: pnpm build       # model tsc + next build
```

## Renderer components (`apps/web/components/ledger/`)

`LedgerGrid` (matrix) = `ColumnHeader` (grouped) + `StepRow` + `BalanceRow` + `Cell`.
`LedgerViewer` (client) holds the playhead and auto-play; `PlaybackBar` scrubs;
`StepInspector` shows the active entry's Dr/Cr lines + balance check; `FilmPicker`
switches samples. All types flow from `@ledger-viz/model`.
