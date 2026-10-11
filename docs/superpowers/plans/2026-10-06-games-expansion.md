# Games Expansion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expand GameMatch from three games to twelve (nine new), each with its own round shape, feeding four new tag-derived scoring axes with plain-language reason chips, behind a protocol registry that makes adding a game a one-entry job.

**Architecture:** One `registry` package declares every game (kind → protocol, rounds, timers, House answer, tag prefixes, chips). The engine, stats, scorer, catalog, and client read it instead of six scattered switch statements. No schema changes: `prompt_bank` carries the options/stops/items/steps, `rounds.extra` carries phase/role/path state, and the new axes are derived from picked option tags. Content ships as one idempotent migration.

**Tech Stack:** Existing Go service (`apps/server`) and React PWA (`apps/web`). No new dependencies.

**Spec:** `docs/superpowers/specs/2026-10-06-games-design.md` — this plan argues from it, so executors read both.

## Global Constraints

- Every round is closed-set and deterministic: the House bot answers with the first option, the middle stop (5), the middle rating (3), or all-left path. **No free text.**
- Six types of round: `pick2`, `pick4`, `phased`, `spectrum`, `order`, `stance`, `coop`, `rapid`, `rate`, `branch`.
- Axes and tag prefixes: `personality` (traits), `interests` (`interest.*`), `lifestyle` (`lifestyle.*`), `values` (`values.*`), `priorities` (`priorities.*`), `mindset` (`mindset.*`), `energy` (`energy.*`); `behavior` and `location` stay as-is.
- Weights (sum 1.0): personality .15, interests .15, values .15, behavior .15, lifestyle .10, priorities .10, mindset .10, energy .05, location .05. Jaccard empty∪empty = 0. Percent hidden until both players have ≥8 tagged answers.
- Copy rule: no psychology vocabulary anywhere player-facing; chips ≤4 words. Game labels and chips come from the registry.
- `GET /v1/games` serves the registry incl. `protocol`, `rounds`, `timerSec`, `ready`. Home queue depth and all defaults stay `this_or_that`.
- Adding a game touches exactly: `game/registry.go` (one entry), one content migration (12+ prompts), and one client renderer keyed by protocol.
- **Either Way interpretation (deviation, pinned here):** one round = one prompt with 3 linked either/or steps; the steps are content-linked (copy follows from the previous choice), not data-linked; the answer is `{path:["left"|"right" x3]}` and a "same" is an identical path. House answers `left,left,left`.
- **Speed Round:** 12 rounds at 5s, four options, answer `{optionId}`. Missed rounds are skipped without signals (existing timeout behavior).

## Review Focus

| Risk | Where the test lives |
| --- | --- |
| A half-added game (registry entry without prompts, or without a client renderer) | Task 1/8: `TestRegistryCompleteness` fails the build |
| Reveal comparators report "same" on a near-miss (one stop off on spectrum/rate) | Task 6: per-protocol comparator tests |
| Renormalized weights silently break the existing ToT/20Q/GMA chips | Task 7: `TestExistingGamesKeepTheirChips` |
| Tags from the new games never reach the scorer sets | Task 7: `TestNewAxesFeedTheScore` |
| A branch leader submits a partial path | Task 6: `TestBranchRequiresFullPath` |
| `speed_round` house answer parses for every seeded prompt | Task 3: registry coverage loops seeded prompts |

---

### Task 1: Introduce the game registry

**Files:**
- Create: `apps/server/internal/game/registry.go`
- Modify: `apps/server/internal/httpapi/catalog.go`
- Test: `apps/server/internal/game/registry_test.go`

**Interfaces:**
- Produces: `type Protocol string` with the ten constants; `type Game struct { Kind, Label string; Protocol Protocol; Rounds int; Timer, GuessWindow time.Duration; HouseAnswer func(payload []byte) json.RawMessage; TagPrefixes []string; Chips []string; Ready bool }`; `var Registry map[string]Game` with the twelve entries; `var Draft bool` — no, `Registry` only.
- Produces helpers: `func RoundsFor(kind string) int` (8 default), `func TimerFor(kind string) time.Duration` (default 8s), `func HouseAnswerFor(kind string, payload []byte) json.RawMessage` (default first-option from a parsed `options`/`left`/`right`/`stops`/`steps` payload).
- Consumes: nothing.

- [ ] **Step 1: Write the failing test** for the registry invariants: twelve kinds; every entry `Ready`; `TagPrefixes` non-empty (except `guess_my_answer`); `Chips` non-empty; `Rounds > 0`; `Timer > 0`.
- [ ] **Step 2: Run it to verify it fails** (`go test ./internal/game/ -run Registry` → FAIL, undefined).
- [ ] **Step 3: Implement** `registry.go` with all twelve entries and answers per the spec's roster table (numbers from the roster rows).
- [ ] **Step 4: Make `GET /v1/games` serve the registry** (label, kind, protocol, rounds, timerSec, ready).
- [ ] **Step 5: Verify** `go test ./internal/game/ ./internal/httpapi/` passes (catalog golden test adjusted to the new shape).

### Task 2: Drive the engine from the registry

**Files:**
- Modify: `apps/server/internal/game/engine.go`
- Test: `apps/server/internal/game/engine_test.go` (extend)

**Interfaces:**
- Consumes: `RoundsFor`, `TimerFor`, `HouseAnswerFor` (Task 1).
- Produces: `func AnswerForPath(payload []byte) (ok bool)` — no; instead engine `openRound` handles `branch` prompts with 3 steps under 15s and `phased` keeps its two-phase; `houseAnswer` deleted in favor of `HouseAnswerFor`.

- [ ] **Step 1: Write the failing tests** — `TestRoundsComeFromTheRegistry` (start `either_way` → `sessionRounds==3`), `TestBranchAcceptsFullPath` (answer `{path:[...3]}` stores the payload; a 2-item path is an error), `TestSpeedRoundTimerIsShort` (`TimerFor("speed_round")==5s`).
- [ ] **Step 2: Run to verify failure.**
- [ ] **Step 3: Implement** `StartSession` rounds from `RoundsFor`; `openRound` timer from `TimerFor`, with `phased` setting `answer_by=now+Timer` then `GuessWindow`; delete `houseAnswer` and call `HouseAnswerFor`; register each protocol's answer write unchanged (payload is opaque).
- [ ] **Step 4: Verify** `go test ./internal/game/` passes (existing timed tests unchanged because ToT/20Q/GMA timers are unchanged).

### Task 3: Registry-driven stats collection

**Files:**
- Modify: `apps/server/internal/game/stats.go`
- Test: `apps/server/internal/game/stats_test.go`

**Interfaces:**
- Consumes: `Registry` (Task 1).
- Produces: none new.

- [ ] **Step 1: Write the failing tests** — `TestPickedTagsIncludeNewGames` (a `hot_take` answer with a `values.*` tag lands in picked tags; a `guess_my_answer` guess does not), `TestRoundWindowUsesRegistry` (`roundWindow("rate_the_night") == 10s`).
- [ ] **Step 2: Run to verify failure.**
- [ ] **Step 3: Implement** `roundWindow` from `TimerFor`/`GuessWindow`; tag collection includes every round whose kind's protocol is not `phased`.
- [ ] **Step 4: Verify.**

### Task 4: Reward cooperative Same Page explicitly

**Files:** `apps/server/internal/game/scorer.go` (same-energy rule)
**Test:** extend `scorer_test.go`

- [ ] **Step 1: Failing test** — `TestSamePageMatchingEarnsChip`: a fully matching `same_page` session produces the registry chip "On the same page".
- [ ] **Step 2: Run to verify failure.**
- [ ] **Step 3: Implement** the scorer reads each game's `Chips` and the axis Jaccards, mapping thresholds: values/priorities/mindset/energy ≥ 0.5 → that game's chip; keep the existing lifestyle/game-style/humor/interests/reads-the-room rules; keep "Same energy" for ≥3 identical regular rounds. Rank by component weight, keep 3.
- [ ] **Step 4: Verify.**

### Task 5: Content — 108 prompts across nine games

**Files:**
- Create: `apps/server/internal/db/migrations/0007_games.sql`
- Test: extension of the registry completeness test (Task 1), plus a seed-time count query.

**Interfaces:** none — data only.

- [ ] **Step 1: Write the failing test** — after `db.Migrate`, `prompt_bank` has ≥12 active rows per new kind.
- [ ] **Step 2: Implement the migration** with INSERT OR IGNORE rows (deterministic UUIDs), 12 per game: `where_do_you_land` (5 stops with `values.*` tags on the outer stops), `rank_your_top_3` (3 items with `priorities.*` tags), `hot_take` (stance statements with `values.*` tags), `same_page` (4 options with `mindset.*` tags), `speed_round` (4 options with `energy.*` tags), `one_free_evening` (4 options with `priorities.*` tags), `odd_one_out` (4 items with `mindset.*` tags), `rate_the_night` (scene + 5 rating choices with `energy.*` tags per option), `either_way` (3 linked steps with `mindset.*` tags).
- [ ] **Step 3: Verify** the count test passes and `go run ./cmd/gamematch seed` still succeeds on top of a migrated DB.

### Task 6: Reveal comparators

**Files:** `apps/server/internal/game/view.go`
**Test:** `apps/server/internal/game/comparator_test.go`

**Interfaces:**
- Produces: `func revealsSame(protocol Protocol, a, b []byte) (same bool, close bool)` — spectrum/rate: `same = |a-b|<=1 && !=`; order: `same = first equal`, `close = full order equal`; branch: `same = identical path`; stance: `same = equal stance`; all pick protocols: `same = equal choice`.

- [ ] **Step 1: Failing tests** — boundary cases: spectrum stops 2 vs 3 → same; 2 vs 4 → not; rate 3 vs 4 → same; order first-item equal → same; branch partial path → error.
- [ ] **Step 2: Run to verify failure.**
- [ ] **Step 3: Implement** and wire into `buildView`'s reveal `same`/`close` computation via the registry protocol.
- [ ] **Step 4: Verify.**

### Task 7: Scoring — nine components and the new chips

**Files:** `apps/server/internal/game/scorer.go`, `apps/server/internal/game/stats.go`
**Test:** `apps/server/internal/game/scorer_test.go`

- [ ] **Step 1: Failing tests** — `TestNewAxesFeedTheScore` (values/priorities/mindset/energy sets reflect picked tags and the weights sum to 1.0), `TestExistingGamesKeepTheirChips` (a full ToT session still shows "Same energy", a GMA session still shows the behavior component).
- [ ] **Step 2: Run to verify failure.**
- [ ] **Step 3: Implement** the nine-component score per the weight table; sets: `values.*` → values, `priorities.*` → priorities, `mindset.*` → mindset, `energy.*` → energy; axis Jaccard ≥ 0.5 unlocks the registry chip.
- [ ] **Step 4: Verify** full `go test ./...` green after weight changes.

### Task 8: Client — roster-driven Play and per-protocol renderers

**Files:**
- Modify: `apps/web/src/App.tsx` (Play list from `/v1/games`), `apps/web/src/session.ts` (new renderers)
- Test: `pnpm --filter web build`, `pnpm --filter web lint`

**Interfaces:** `Protocol` renderer map keyed by the string from `/v1/games`.

- [ ] **Step 1: Renderers** — spectrum (5-tap slider), order (tap-to-order 3 items), stance (3 buttons), coop (4 options, "pick what they'd pick"), rapid (4 options, big timer), rate (5 dots), branch (three sequential either/or taps).
- [ ] **Step 2: Play screen** — render `items` from `/v1/games`; the session page switches on `round.protocol` from the view's `round.kind` via the catalog mapping (cache the map client-side).
- [ ] **Step 3: Verify** `pnpm --filter web build` and `pnpm --filter web lint` pass.
- [ ] **Step 4: Commit** (client and server land together so a half-landed protocol cannot ship).

### Task 9: Registry completeness guard + full verification

**Files:** `apps/server/internal/game/registry_test.go` (extend), `scripts/dev.sh` untouched.

- [ ] **Step 1: Failing test** — for every registry entry with `Protocol != phased`: every seeded prompt in `prompt_bank` parses under `HouseAnswerFor`; every entry's `Protocol` has a client counterpart (shared protocol constant list exported from a small `apps/web/src/protocols.ts`).
- [ ] **Step 2: Run to verify failure** (client map incomplete initially).
- [ ] **Step 3: Finish the client map, run the full suite** — `go test -race ./...`, `go vet`, `pnpm --filter web build` all green; run `bash scripts/deploy.sh` once to prove nothing else broke.
- [ ] **Step 4: Commit.**

## Self-Review

- **Spec coverage:** roster table → Task 1 entries; registry refactor → Tasks 1-3; axes → Task 5 content + Task 7 scoring; chips/copy → Tasks 4/7 + the registry `Chips`; client → Task 8; testing/guard → Tasks 1/5/6/9. Weights sum to 1.0 (Task 7 test pins it).
- **Honest deviations, all pinned above:** Either Way is content-linked, not data-linked; "on the same page" and tuning thresholds are the only new scorer logic beyond the axis Jaccards; `speed_round` at 5s may be missed by slow polls (accepted in the spec's risks).
- **Type consistency:** `Protocol` constants are the same strings in Go and in `apps/web/src/protocols.ts`; `Game.{Kind,Label,Protocol,Rounds,TimerSec}` is the exact `/v1/games` item shape.
- **Proportion:** the spec is 242 lines, this plan ~200. Content copy lives in the migration, not here.