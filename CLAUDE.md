# CLAUDE.md

Behavioral guidelines to reduce common LLM coding mistakes. Merge with project-specific instructions as needed.

**Tradeoff:** These guidelines bias toward caution over speed. For trivial tasks, use judgment.

## 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:
- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them - don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

## 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

## 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:
- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it - don't delete it.

When your changes create orphans:
- Remove imports/variables/functions that YOUR changes made unused.
- Don't remove pre-existing dead code unless asked.

The test: Every changed line should trace directly to the user's request.

## 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:
- "Add validation" → "Write tests for invalid inputs, then make them pass"
- "Fix the bug" → "Write a test that reproduces it, then make it pass"
- "Refactor X" → "Ensure tests pass before and after"

For multi-step tasks, state a brief plan:
```
1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
```

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.

---

**These guidelines are working if:** fewer unnecessary changes in diffs, fewer rewrites due to overcomplication, and clarifying questions come before implementation rather than after mistakes.

---

## Project: zero-JavaScript Todo List

A deliberately constrained Todo List. The entire premise is that **the frontend ships zero custom JavaScript** — every state mutation is a server-rendered HTML response handled by HTMX. Because HTML *is* the API, the whole system can be tested end-to-end with `httptest` and HTML parsing: no browser, no JS runtime, no headless Chrome.

The native Go finite state machine (`Pending → InProgress → Completed`) lives at the handler boundary; invalid transitions are rejected by the database via an optimistic `WHERE … AND status = ?` clause and surfaced to the UI through an out-of-band error banner swap.

## Core Constraints — non-negotiable

- **ZERO custom JavaScript.** No `<script>` tags except the HTMX CDN. **Specifically banned: Alpine.js, hyperscript, htmx-extensions that ship JS, jQuery, inline event handlers (`onclick=`, `onsubmit=`).** If a feature seems to need any of these, the design is wrong — push it to the server.
- No JSON responses. Handlers return rendered HTML only.
- No third-party state-machine library. The FSM is a small `switch` on `type TodoState string`.
- No headless browser in tests. No JSDOM, no Node. Pure Go test toolchain.
- No CSS utility classes (Tailwind/Bootstrap). Classless CSS via Pico keeps templates semantic.

## Tech Stack

| Layer | Choice | Why |
|---|---|---|
| Language | Go | Single static binary, embeds everything. |
| HTTP router | Echo (`labstack/echo/v4`) | User decision (OpenAPI future). |
| Database | SQLite (`mattn/go-sqlite3`) | Single-file, WAL mode handles HTMX's small concurrent requests. |
| Migrations | Goose (`pressly/goose/v3`) | SQL migrations embedded via `go:embed`, run on startup. |
| Queries | sqlc | Typed Go from raw SQL; no ORM. |
| Templates | `html/template` (stdlib) | Auto-escaping, no second codegen step on top of sqlc. |
| Frontend lib | HTMX (CDN) | The only client-side library. No Alpine, no hyperscript. |
| Styling | Pico.css v2 (vendored, `go:embed`) | Classless — templates stay semantic, tests assert on tags not utility classes. |
| Tests | `net/http/httptest` + `goquery` | Pure Go. No browser, no Node. |
| Lint / static | `golangci-lint` (CLI flags, no config) + `govulncheck` | One binary covers `errcheck` + `staticcheck` + `govet` + `ineffassign` + `goimports`. |
| Build target | `Makefile` with `fmt`, `lint`, `test`, `cover`, `check` | `make check` before commit. |

## Folder Structure

```
no-js-todolist/
├── CLAUDE.md
├── .claude/
│   └── rules/             # path-scoped detailed rules — load on demand
│       ├── handlers.md    # paths: handlers.go, main.go
│       ├── fsm.md         # paths: fsm.go
│       ├── views.md       # paths: views/**, render.go, static/**
│       ├── database.md    # paths: query.sql, sqlc.yaml, migrations/**, db/**
│       └── tests.md       # paths: **/*_test.go
├── go.mod
├── go.sum
├── main.go                # entry: embed migrations, goose.Up, wire Echo, start server
├── main_test.go           # httptest + goquery — FSM + HTML-contract tests
├── handlers.go            # Echo HTTP handlers (one per route)
├── fsm.go                 # TodoState type + CanTransitionTo (the FSM)
├── render.go              # template parsing + Render helper
├── sqlc.yaml              # sqlc config (engine: sqlite)
├── query.sql              # sqlc query definitions
├── db/                    # sqlc-generated — DO NOT EDIT BY HAND
├── migrations/            # Goose SQL migrations, embedded
│   └── 001_init.sql
├── views/                 # html/template files
│   ├── layout.html        # base shell — includes #error-banner OOB target
│   ├── index.html
│   ├── todo_item.html     # single <li> partial — used for list AND PUT response
│   └── error_banner.html  # OOB error banner partial
└── static/
    └── pico.css           # vendored Pico.css v2 — embedded
```

Flat by design. No `internal/` or `pkg/` nesting for a 4-route app.

## How the rules are split

Detailed per-area rules live in `.claude/rules/*.md` with `paths:` frontmatter, so they only load into context when Claude touches matching files. This keeps the always-loaded portion (this file) under the recommended size while still encoding extensive guidance for each subsystem.

| File | Loads when Claude touches |
|------|---------------------------|
| `.claude/rules/handlers.md` | `handlers.go`, `main.go` |
| `.claude/rules/fsm.md` | `fsm.go` |
| `.claude/rules/views.md` | `views/**`, `render.go`, `static/**` |
| `.claude/rules/database.md` | `query.sql`, `sqlc.yaml`, `migrations/**`, `db/**` |
| `.claude/rules/tests.md` | `**/*_test.go` |
| `.claude/rules/tooling.md` | `Makefile`, `.github/**`, `.golangci.yml`, `.gitignore` |

## Non-Goals (explicit "don't add this")

- **No custom JS / Alpine.js / hyperscript / jQuery.** HTMX CDN only. Even "just a sprinkle" of Alpine for a dropdown is forbidden — push interactions to the server. This is the project's identity, not a preference.
- **No JSON endpoints.** Server returns HTML or empty body.
- **No state-machine library.** The switch is the FSM.
- **No CSS framework with utility classes.** Pico classless only.
- **No headless browser tests.** Not now, not "just one."
- **No `templ`.** `html/template` is sufficient and avoids a second codegen step on top of sqlc.
- **No CSRF middleware.** Local educational app. Revisit if this ever runs publicly.
- **No logging framework.** `log` from stdlib if anything. Don't introduce zap/zerolog speculatively.
- **No config library.** A handful of env vars read with `os.Getenv` is fine.
- **No OpenAPI scaffolding yet.** Echo stays for OpenAPI flexibility, but don't add swaggo or oapi-codegen until there's a concrete consumer.
- **No `internal/` package nesting.** Flat structure for a 4-route app.
