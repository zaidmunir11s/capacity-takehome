# Capacity Take-Home

A contact-list app with tiered capacity limits. GraphQL API (Go + MongoDB) and Expo mobile client.

## Run it

Needs Go 1.25+, Node 20+, Docker.

```bash
make up        # mongo on :27117 (replica set, so transactions work)
make api       # graphql on :8080, seeds ten users on first boot
make mobile    # expo — press i for the iOS simulator, w for web
make check     # go build + vet + test, and tsc on the client
```

Playground at <http://localhost:8080>. Confirm it's alive:

```bash
curl -s localhost:8080/query -H 'Content-Type: application/json' \
  -d '{"query":"{ users { id name } }"}'
```

Copy any `id` and send it as the `X-User-Id` header. That's the whole authentication story.

On a physical device set `EXPO_PUBLIC_API_URL=http://<your-lan-ip>:8080/query`.

## Tiers

| Tier | Cap |
|---|---|
| Pink | 1 |
| Blue | 3 |
| Green | 5 |
| **Shared budget** | **8** |

Caps are env vars (`CAP_BUDGET`, `CAP_PINK`, `CAP_BLUE`, `CAP_GREEN`).

## What was built

| Req | Feature |
|---|---|
| R1 | Send request to a named tier |
| R2 | Accept / decline (bilateral contacts on accept) |
| R3 | Move contact between tiers |
| R4 | Remove contact (frees both sides) |
| R5 | Mobile: contacts by tier, live used/cap, budget |
| R6 | Mobile: request inbox; failed accept shows plain error |

R7 (posts) and R8 (optimistic accept) were not implemented — stretch goals, deprioritized for time.

## Tests

Rule tests live in `api/internal/capacity/capacity_test.go` (Rules 1–3, over-cap) and `api/internal/store/race_test.go` (Rule 4 concurrency). Store integration tests in `api/internal/store/operations_test.go`.

```bash
make up
make check
```

## Decisions

1. **Pure `capacity` package + atomic `userSeats` in store.** Tier/budget decisions live in `capacity.CanSend` / `CanAdd` / `CanMove` (pure, unit-tested). Enforcement at accept time uses conditional `findOneAndUpdate` on a per-user `userSeats` document so concurrent accepts cannot overshoot the budget. Rejected: read `CountsFor` → `CanAdd` → insert (fails Rule 4).

2. **Single-screen mobile app.** Extended `App.tsx` in place with sections for capacity, contacts by tier, inbox, and send form. Rejected: React Navigation or multiple screens — unnecessary for demo scope.

3. **`CanSend` checks budget only.** Pending requests hold no seat; send is blocked when the sender has zero free budget seats (`total >= budget`). Tier sub-caps are enforced at accept via `CanAdd`. Rejected: checking tier caps at send time (would block legal pending requests).

4. **Bilateral sync in store transactions.** Accept, move, and remove always update both contact rows and both users' seat counters in one transaction. Rejected: updating only the caller's row.

## What's unfinished

- **R7 posts** — not in GraphQL schema; stretch goal.
- **R8 optimistic accept** — stretch goal.
- **Outgoing requests UI** — API query exists; mobile shows incoming only (enough for demo).
- **Tier-specific error text** — failures say "Your tier is full." without naming the tier or counts; functional for R6 but could be clearer.
- **GraphQL / resolver tests** — covered by store integration tests instead.

## Where the agent got it wrong

The agent initially wrote `TestOverBudgetIsHandled` with a move to Pink when Pink was already at 2/1, expecting the move to succeed. The implementation correctly returned `ErrTierFull`. The test scenario was wrong, not the code — caught when `go test` failed and the destination tier state was traced.

## Agent transcript

Full Cursor agent session for this exercise:

- [transcripts/cursor-session.jsonl](transcripts/cursor-session.jsonl)

(Raw JSONL export from Cursor — includes analysis, implementation, and review turns.)
