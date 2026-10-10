# Games Expansion — Design Spec

**Status:** draft for review (2026-10-06)
**Goal:** Grow GameMatch from three games to twelve, each with its own round shape, so two strangers get
varied, low-pressure ways to reveal how they think — and are told plainly why they clicked.
**Spec type:** architectural. This spec is followed by an implementation plan
(`docs/superpowers/plans/`), not by code directly.

## Context

Games already work as content plus a small amount of shape. Every game reads prompts from one
`prompt_bank` table, and the "why you two" story is already a pipeline:

```
option tags → personality / lifestyle / interest sets → plain-language reason chips
```

That pipeline is the mechanism this spec extends. What does *not* scale is the shape: rounds, timer,
roles, reveal rule, and the House bot's answer live in switch statements across `engine.go` (5),
`stats.go` (2), plus `catalog.go`, the client's Play list, and the queue/invite defaults. Adding nine
games that way means editing six places each and silently breaking the ones you miss.

## Goal and non-goals

**Goal:** twelve games, each with a distinct round shape, all live 1v1, all deterministic, all feeding
new ways to show why two people fit.

**Non-goals:**

- No free-text answers. Live play means the House bot must answer without a language model, so every
  round has a fixed option set. (This is the reason the roster avoids "write your own".)
- No asynchronous or turn-at-your-own-pace play. The session model stays as it is.
- No psychology vocabulary anywhere a player can see it: no type codes, and no *trait, attachment,
  introvert, extrovert, cognitive, compatibility score, personality type*.
- No user-generated prompts. Content stays founder-seeded.
- No groups. Sessions remain two-sided.

## Success criteria

1. A player can start any of the twelve games from Play, play it live against another person or the
   practice opponent, and see a reveal each round and a summary at the end.
2. Every game produces at least one plain-language reason chip when the two answers align.
3. Adding a thirteenth game requires exactly: one registry entry, one content migration, one client
   renderer — and nothing else.
4. A registry coverage test fails the build if a game is half-added.

## The roster

Twelve games. Three exist; nine are new. `House answer` is the practice opponent's reply, and is the
reason every round shape here is closed-set.

| # | Kind | Game | Rounds | Timer | Round shape and answer | Reveal rule | House answer | Axis it feeds |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | `this_or_that` | This or That *(exists)* | 8 | 8s | Pick left or right. `{choice}` | Identical pick | left | interest, lifestyle |
| 2 | `twenty_questions` | 20 Questions *(exists)* | 6 | 20s | Pick one of four. `{optionId}` | Identical pick | first option | interest |
| 3 | `guess_my_answer` | Guess My Answer *(exists)* | 6 | 8s + 12s | One picks, the other predicts. `{optionId}` | Guess equals secret | first option | (accuracy) |
| 4 | `where_do_you_land` | Where Do You Land | 8 | 10s | Slide to one of five stops on a line, e.g. *Planner ⸺ Spontaneous*. `{stop:0-4}` | Within one stop | stop 2 (middle) | values |
| 5 | `rank_your_top_3` | Rank Your Top 3 | 5 | 12s | Put three things in order. `{order:[id,id,id]}` | Same first item; full order is a bonus | given order | priorities |
| 6 | `hot_take` | Hot Take | 6 | 10s | Agree / Disagree / It depends on a statement. `{stance}` | Identical stance | agree | values |
| 7 | `same_page` | Same Page | 6 | 8s | Pick one of four **deliberately trying to match**. `{optionId}` | Identical pick (this is the win, not just a signal) | first option | mindset |
| 8 | `speed_round` | Speed Round | 12 | 5s | Rapid pick of one of four. `{optionId}` | Identical pick | first option | energy |
| 9 | `one_free_evening` | One Free Evening | 5 | 12s | Spend one scarce thing on one of four. `{optionId}` | Identical pick | first option | priorities |
| 10 | `odd_one_out` | Odd One Out | 6 | 10s | Tap the one that does not belong. `{optionId}` | Identical pick | first option | mindset |
| 11 | `rate_the_night` | Rate the Night | 5 | 10s | Rate a scene one to five. `{rating:1-5}` | Within one point | 3 | energy |
| 12 | `either_way` | Either Way | 3 | 15s | Three linked either/or choices; each answer reshapes the next. `{path:[3]}` | Identical path | left, left, left | mindset |

Games 1–3 keep their existing axes. Games 4–12 introduce the four new tag-derived axes below.

### Why these shapes

Each new game varies something real rather than reskinning a pick:

- **Distance** instead of equality (`where_do_you_land`, `rate_the_night`) — being one step apart is a
  different, friendlier story than being identical.
- **Ordering** (`rank_your_top_3`) — reveals what someone puts *first*, which a single pick cannot.
- **Deliberate agreement** (`same_page`) — the only cooperative game; matching is the goal.
- **Scarcity** (`one_free_evening`) — what you protect when you cannot have everything.
- **Speed** (`speed_round`) — how bold and quick you are, feeding the behaviour tempo counter.
- **Categorising** (`odd_one_out`) — how you sort the world.
- **Sequence** (`either_way`) — whether your next decision follows from your last.
- **Stance** (`hot_take`) — where you draw lines.

## Axes and tag vocabulary

Three content axes exist: **personality**, **lifestyle**, **interest**. Add four, all derived from
picked tags rather than from user traits, so the `traits` table is untouched:

| Axis | Prefix | Feeds from | Example chip |
| --- | --- | --- | --- |
| values | `values.*` | `where_do_you_land`, `hot_take` | "Agree where it counts" |
| priorities | `priorities.*` | `rank_your_top_3`, `one_free_evening` | "Same number one" |
| mindset | `mindset.*` | `same_page`, `odd_one_out`, `either_way` | "On the same page" |
| energy | `energy.*` | `speed_round`, `rate_the_night` | "Same speed" |

Chips are declared per game in the registry (see below) so the copy can be specific — `odd_one_out`
offers "Sort things the same way", `either_way` offers "Think in the same steps". The reason table
still ranks every candidate by its contribution and keeps three.

The behaviour counters (risk, tempo, rematch, guess) are unchanged. `speed_round` and `rate_the_night`
feed tags into the energy axis; tempo continues to live in the behaviour vector, so the same answer is
never counted twice.

## The protocol registry

One declaration replaces the six scattered switch statements:

```go
type Protocol string

const (
    ProtocolPick2    Protocol = "pick2"    // two options
    ProtocolPick4    Protocol = "pick4"    // four options
    ProtocolPhased   Protocol = "phased"   // answer then predict
    ProtocolSpectrum Protocol = "spectrum" // five stops on a line
    ProtocolOrder    Protocol = "order"    // rank three items
    ProtocolStance   Protocol = "stance"   // agree / disagree / depends
    ProtocolCoop     Protocol = "coop"     // match on purpose
    ProtocolRapid    Protocol = "rapid"    // short timer, many rounds
    ProtocolRate     Protocol = "rate"     // rate one to five
    ProtocolBranch   Protocol = "branch"   // linked choices
)

type Game struct {
    Kind        string
    Label       string
    Protocol    Protocol
    Rounds      int
    Timer       time.Duration
    GuessWindow time.Duration        // phased only
    HouseAnswer func(prompt []byte) json.RawMessage
    TagPrefixes []string             // prefixes this game contributes to the sets
    Chips       []string             // plain-language reasons this game can unlock
    Ready       bool
}

var Registry = map[string]Game{ /* twelve entries */ }
```

Consumers:

- `game.StartSession` reads `Rounds` and `Timer` instead of switching on the kind.
- `game.openRound` drives phases from `Protocol` (`phased` is the only multi-phase shape).
- `game.view` reports the phase and role from the round's `extra`, as it does today.
- `game.stats.roundWindow` and `pickedTags` read `TagPrefixes` and the protocol instead of listing kinds.
- `game.scorer` builds one set per axis prefix and reads each game's `Chips`.
- `httpapi.catalog` serves the registry, so the client stops hardcoding a three-item list.

## Data model

**No schema changes.** The existing tables already carry everything:

- `prompt_bank(kind, payload, tags)` — one row per prompt; `payload` holds the options, stops, items,
  statements, or scenes for that protocol.
- `game_sessions(kind, mode, config)` — `config` carries the round count and timers from the registry.
- `rounds(prompt_id, extra)` — `extra` carries protocol state (roles, phase, and the chosen path so far).
- `round_answers(payload)` — the answer shapes listed in the roster.

The new axes need no column: they are derived from `prompt_bank.tags` on the options a player picked.

## API changes

`GET /v1/games` returns the registry, with enough for the client to render without a hardcoded list:

```json
{ "items": [ { "kind": "hot_take", "label": "Hot Take", "protocol": "stance",
               "rounds": 6, "timerSec": 10, "ready": true } ] }
```

Everything else is unchanged. Queue and invite accept any registered kind; the Home queue depth and
the default game stay `this_or_that`.

## Client

- The Play screen renders the list from `/v1/games` instead of three literals.
- One renderer per protocol: two-button pick, four-option pick, answer-then-guess, five-stop slider,
  drag-to-order, three-way stance, rating scale, linked either/or, and the rapid picker.
- Reveal copy is protocol-specific: distance for slider and rating, shared first item for ranking,
  "we matched" for Same Page, identical path for Either Way.
- A compile-time exhaustiveness map (`Record<Protocol, Component>`) means a new protocol cannot ship
  without a renderer.

## Content

12–20 founder-written prompts per new game, leaning to 12 (about 110 rows total), added as one content
migration. Content rules:

- Prompts are things two people can answer in under ten seconds.
- Every option carries at least one tag from its game's axis, plus an existing taste tag where it fits.
- No prompts that invite someone to judge their partner; the games are for showing, not testing.
- Anything a player reads follows the copy rule: no psychology vocabulary, chips ≤4 words.

## Scoring

Nine components now, renormalized to sum to 1.0:

| Component | Weight |
| --- | --- |
| personality | 0.1500 |
| interests | 0.1500 |
| values | 0.1500 |
| behavior | 0.1500 |
| lifestyle | 0.1000 |
| priorities | 0.1000 |
| mindset | 0.1000 |
| energy | 0.0500 |
| location | 0.0500 |

The four headline signals (personality, interests, values, behavior) carry 0.15 each, the three
secondary ones (lifestyle, priorities, mindset) carry 0.10 each, and energy and location round the
sum to 1.0 with 0.05 each.

Existing rules hold: Jaccard of empty against empty is 0, the percent stays hidden until both players
have eight tagged answers, and chips are ranked and capped at three.

## Testing

- **Registry coverage (the guard):** for every registry entry — at least twelve active prompts exist,
  `HouseAnswer` returns a payload that parses for every seeded prompt, `TagPrefixes` are non-empty, and
  the client's protocol map has the protocol.
- **Per protocol:** round count and timer come from the registry; the reveal comparator reports
  same / close / different correctly, including the boundary cases (one stop apart, one rating apart);
  the House answer is valid; a missed round is skipped without recording signals.
- **Scoring:** one test per new axis asserting its set is built from picked tags and that its chip can
  appear; the percent gate and the existing chips still behave after renormalization.
- **Regression:** the existing this-or-that, 20-questions, and guess-my-answer tests must pass with
  their assertions unchanged.

## Risks

| Risk | Mitigation |
| --- | --- |
| Nine bespoke protocols drift or get half-added | Registry coverage test fails the build; one registry entry is the only way to add a game |
| Renormalized weights shift the chips existing games produce | Regression tests pin the current ToT/20Q/GMA chips before any weight change |
| `speed_round`'s 5s rounds outpace 1s client polling, so a player can miss a round | Server deadlines already skip missed rounds safely; the reveal reports what happened. Accepted for v1 |
| Bespoke renderers are the long pole, not the engine | Client work is sequenced by protocol, so each game ships playable on its own |
| Closed-set rounds limit expression | Deliberate: it is what keeps practice mode honest without a language model |

## Rollout

Each game ships independently: registry entry, content migration, renderer, tests. The registry
refactor lands first because every later game depends on it, and the three existing games must keep
working across it.
