# no-js-arcade

A single-player **HTMX arcade** (Snake, 2048, Minesweeper) with **zero custom JavaScript**, built as an experiment in optimizing the agentic-coding harness.

## Why

Frontend testing is a nightmare for AI agents. Modern stacks need a browser to verify anything that touches the UI: headless Chrome, a JS runtime, async event loops, flaky DOM resolution, mocks for everything network-shaped. Agents iterate slowly and humans end up babysitting.

This project pushes UI state to the server. Every user-visible change is an HTTP request returning HTML — parseable in pure Go in milliseconds, no browser. Games and step-form wizards are the *harder* version of this challenge: composed state machines, board state, scoring, leaderboards, multi-step user flows, and (for Snake) a real-time loop. If the harness can model them without a browser, it can model anything in this style.

## Core constraints

| Rule | Why it pays for itself |
|---|---|
| **Zero custom JavaScript.** HTMX CDN only. No Alpine, hyperscript, jQuery, or HTMX extensions. | No JS to debug = no JS runtime needed to verify behavior. |
| **No JSON responses.** Handlers return rendered HTML fragments. | The response body IS the assertion target. |
| **Native Go FSMs** (small `switch` methods on string types). | State invariants are pure-function-testable; handler-level enforcement closes TOCTOU races. |
| **Classless CSS** (Pico v2, vendored). | Templates stay semantic. Tests assert on `button[hx-put]`, not utility classes. |
| **`httptest` + `goquery`** for all tests. | Pure Go, ~1ms per request, no browser, no Node. |

## What's in the arcade

A 5-step wizard (Name → Game → Difficulty → Play → Leaderboard) orchestrates the three games. Backward navigation is server-driven, enforced by the Wizard FSM.

| Game | FSM | Difficulty knob | Score |
|---|---|---|---|
| **2048** | `playing → won → continued → lost` | 5×5 / 4×4 / 3×3 grid | tile merges |
| **Minesweeper** | `playing → won → lost` | 9×9/10 / 16×16/40 / 24×24/99 | cells revealed |
| **Snake** | `idle → playing → game_over` | tick 250ms / 150ms / 80ms | snake length |

Snake is the only game with a server-side loop: each session gets a goroutine that ticks the snake, the client long-polls `/game/snake/board` for the next frame, arrow-key POSTs push into the goroutine's input channel. Zero extensions, just HTTP cycling. 2048 and Minesweeper are turn-based: click → POST → fragment swap.

## Architecture: FSMs + pure functions + a thin imperative shell

The harness is fast because the test surface is narrow. The *only* impure code in this repo is:

| File | What it touches |
|---|---|
| `handlers.go` | HTTP, DB, session cookies, render |
| `game_snake_runtime.go` | Goroutine + ticker + waiter notification |
| `cmd/server/main.go` | `sql.Open`, `NewApp(...).Start(":8080")` |

Everything else — every FSM, every board mutation, every win/loss check, every direction validation, every flood-fill — is a pure function over a strongly-typed struct. RNG and time are *parameters*, never globals. Tests pass `rand.New(rand.NewSource(1))` and behave deterministically; the same `ApplyMove` runs in production and tests, no mocks.

The FSMs are also enforced at the database via optimistic `UPDATE … WHERE state = ?` clauses. Stale or invalid transitions yield `rowsAffected == 0` → the handler returns an OOB error banner. No transactions, no in-memory locks.

**The empirical payoff:** each game's pure layer ships at **85-100% coverage on the first commit**, with no mocks and no setup. FSM tests are table-driven matrices — every `(from, to)` pair covered in ~15 lines. The full ~50-test suite runs in under two seconds.

Deep discipline lives in `.claude/rules/fsm.md` and `.claude/rules/pure_functions.md`, which auto-load when Claude touches `wizard.go` or `game_*.go`.

## Running

```bash
make run                   # starts the server on :8080
make test                  # full suite (~2s)
make test-unit             # white-box only
make test-e2e              # black-box HTTP user stories only
make cover                 # writes coverage.html + per-function table
make check                 # fmt + lint + govulncheck + test
make build                 # single static binary at ./no-js-arcade
```

Identity is a session-cookie UUID — no auth, name is a display string. SQLite is created on first run; migrations execute on startup.

## Stack

Go + Echo + SQLite (WAL, `_sync=NORMAL`) + Goose (embedded migrations) + sqlc (typed queries) + `html/template` + HTMX 2.0 (CDN, core only) + Pico.css v2 (vendored).

Lint stack: `goimports` + `golangci-lint` with CLI flags only (`errcheck`, `staticcheck`, `govet`, `ineffassign`) + `govulncheck`. No `.golangci.yml`.

## Accepted trade-offs

- **DOM-level HTMX bugs aren't caught in CI.** Browser-only behaviors slip past Go tests. The harness doesn't add a browser — it adds a *rule* per bug. The cross-reference test catches typoed `hx-target` IDs at the contract level; the long-poll/interactive-trigger rule (added after a real Snake control bug) catches another shape via static template analysis.
- **Real-time has a ceiling.** Snake's long-polling reaches ~100ms latency without extensions. Sub-50ms push or collaborative editing (Google Docs, Figma) needs SSE / WebSockets / CRDTs — wrong stack.
- **Single-process by design.** SQLite + auto-migrate on startup + in-memory Snake goroutines. Horizontal scaling out of scope.

## More

- `CLAUDE.md` — folder structure, tech stack rationale, non-goals.
- `.claude/rules/*.md` — path-scoped rules for handlers, FSMs, pure functions, views, database, tests, e2e, tooling.

## License

MIT.
