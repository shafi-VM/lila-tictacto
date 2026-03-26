# Multiplayer Tic-Tac-Toe — Build Spec

## Project Structure

```
tictactoe/
├── backend/                  # Go Nakama plugin (.so)
│   ├── go.mod
│   ├── main.go               # InitModule — registers everything
│   ├── match_handler.go      # Core server-authoritative game logic
│   ├── matchmaker.go         # Auto-pair players hook
│   ├── leaderboard.go        # Win tracking + RPC
│   └── Dockerfile            # Multi-stage: build .so → embed in Nakama image
├── frontend/                 # React + TypeScript
│   ├── src/
│   │   ├── main.tsx
│   │   ├── App.tsx
│   │   ├── nakama.ts         # Singleton client/socket
│   │   ├── screens/
│   │   │   ├── Login.tsx     # Nickname entry → device auth
│   │   │   ├── Lobby.tsx     # Matchmaking UI
│   │   │   ├── Game.tsx      # Board + turn indicator
│   │   │   └── Result.tsx    # Winner + leaderboard
│   │   └── types.ts          # Shared GameState type
│   ├── package.json
│   └── vite.config.ts
├── deploy/
│   ├── docker-compose.yml    # Local dev: Nakama + PostgreSQL
│   └── docker-compose.prod.yml  # Production overrides
└── SPEC.md                   # This file
```

---

## Architecture

```
Client A (React) ──┐
                   ├──WebSocket──► Nakama Server ──► Match Handler (Go plugin)
Client B (React) ──┘                    │
                                        ▼
                                   PostgreSQL
                                 (storage + leaderboard)
```

**Server-authoritative:** all game state lives on the server. The client sends
intent (move position). The server validates, mutates state, and broadcasts
truth back to all players. The client only renders what the server says.

---

## Design Principles

**1. Silent ignore over error rejection.**
Invalid inputs (wrong turn, bad position, move on finished game) are silently
dropped rather than returning errors to the client. This prevents unnecessary
client-server chatter, avoids leaking server state to a cheating client, and
keeps state progression deterministic. Errors are logged server-side for
observability.

**2. Deterministic tick-based processing.**
All player inputs are processed in `MatchLoop`, which Nakama calls on a fixed
tick rate. This guarantees inputs are handled sequentially — no race conditions,
no concurrent state mutation. Every client converges to the same truth because
the server processes one tick at a time and broadcasts a single authoritative
state after each.

**3. Match isolation as the unit of scalability.**
Each match is an independent state machine within Nakama. Matches share no
memory. This means hundreds of concurrent games run safely without locks or
shared state, and the architecture scales horizontally without redesign.

**4. Observability at every lifecycle boundary.**
Key events — player join, move applied, game won, player disconnected — are
logged via Nakama's runtime logger. This is non-negotiable in production game
servers: silent failures are untraceable at scale.

**5. Reconnection is not implemented; here is how it would be.**
If a player disconnects and reconnects within a match, Nakama can re-associate
their `userId` with the existing match presence. The server would need to track
`expectedPlayers []string` in `GameState` and allow re-entry in
`MatchJoinAttempt` if the userId matches a known player. Skipped for now to
keep scope focused.

---

## Message Protocol

| OpCode | Direction         | Payload              | Purpose                        |
|--------|-------------------|----------------------|--------------------------------|
| 1      | server → clients  | `GameState` JSON     | Full state broadcast           |
| 2      | client → server   | `{position: 0-8}`    | Player makes a move            |
| 3      | server → clients  | `{message: string}`  | System events (forfeit, etc.)  |

---

## GameState Shape

```typescript
type GameState = {
  board: string[];       // 9 elements: "" | "X" | "O"
  turn: string;          // userId of player whose turn it is
  players: string[];     // [0]=X userId, [1]=O userId
  usernames: Record<string, string>; // userId → display name
  status: "waiting" | "playing" | "done";
  winner: string;        // userId | "draw" | ""
}
```

---

## Build Phases

---

### Phase 1 — Local Environment (Day 1 morning)

**Goal:** Nakama running locally, plugin compiling, hello-world RPC working.

Steps:
1. Write `deploy/docker-compose.yml` with Nakama + PostgreSQL
2. Run `docker compose up` — verify console at `localhost:7351`
3. Write `backend/go.mod` and `backend/main.go` with a single test RPC:
   ```go
   initializer.RegisterRpc("ping", func(...) (string, error) { return "pong", nil })
   ```
4. Write `backend/Dockerfile` (multi-stage plugin build)
5. Mount compiled `.so` into Nakama container, call `/v2/rpc/ping` from curl
6. **Done when:** curl returns `"pong"` from running Nakama

**Blocker to anticipate:** Plugin must be compiled with exact same Go version as
Nakama's builder image. Use `heroiclabs/nakama-pluginbuilder:3.22.0` — do not
use local Go toolchain.

---

### Phase 2 — Match Handler (Day 1 afternoon → Day 2)

**Goal:** Two browser tabs can play a complete game through Nakama.

Steps:
1. `backend/match_handler.go` — implement all 6 lifecycle methods:
   - `MatchInit` → initialize empty board, status="waiting"
   - `MatchJoinAttempt` → reject if 2 players already present
   - `MatchJoin` → assign X/O, set status="playing" when full, broadcast
   - `MatchLeave` → if mid-game, forfeit to remaining player, broadcast
   - `MatchLoop` → process moves (full validation), check win/draw, broadcast
   - `MatchTerminate` → no-op for now
   - `MatchSignal` → no-op for now

2. Validation checklist inside `MatchLoop`:
   - [ ] `status == "playing"` guard (break, not continue — covers mid-tick finish)
   - [ ] `opcode == OpCodeMakeMove` check
   - [ ] sender is current turn player (anti-cheat: not your turn)
   - [ ] position is 0-8 (anti-cheat: bounds check)
   - [ ] position is empty (anti-cheat: replay/retry protection)
   - After valid move: check winner → check draw → switch turn

3. `backend/matchmaker.go` — `RegisterMatchmakerMatched` hook:
   - Receives 2 matched presences
   - Calls `nk.MatchCreate("tictactoe", params)`
   - Returns match ID (Nakama automatically notifies clients)

4. Test manually:
   - Open Nakama console → create 2 test users
   - Use console socket tool or write a quick test client
   - Play through a full game including resign/disconnect

**Done when:** complete game plays end-to-end through Nakama in local docker.

---

### Phase 3 — Leaderboard (Day 2)

**Goal:** Wins tracked persistently, top-10 queryable via RPC.

Steps:
1. `backend/leaderboard.go`
2. In `InitModule`: call `nk.LeaderboardCreate("global_wins", false, "desc", "incr", "", false)`
   - Must be idempotent — safe to call on every server start
3. In `MatchLoop`, after status becomes "done":
   - If winner is a userId (not "draw"): `nk.LeaderboardRecordWrite("global_wins", winnerUserId, score=1)`
   - Fire-and-forget is acceptable — leaderboard is eventually consistent by design
4. Register RPC `get_leaderboard`:
   - Calls `nk.LeaderboardRecordsList("global_wins", nil, 10, "", 0)`
   - Returns JSON array of `{username, score, rank}`

**Done when:** play 3 games, RPC returns correct winner with incremented score.

---

### Phase 4 — Frontend (Day 3)

**Goal:** Working React UI connected to local Nakama end-to-end.

Stack: React + TypeScript + Vite. No UI library — plain CSS. Keep it close to
the sample implementation (dark theme, clean grid).

Steps:

1. `frontend/src/nakama.ts` — singleton:
   - `Client` instance pointing at Nakama host
   - `authenticate(username)` → device auth (deviceId in localStorage)
   - Exports `socket`, `session`, `client`

2. **Login screen** (`screens/Login.tsx`):
   - Input for nickname
   - On submit: `authenticate(nickname)` → navigate to Lobby

3. **Lobby screen** (`screens/Lobby.tsx`):
   - Button: "Find Match"
   - Calls `socket.addMatchmaker("*", 2, 2, {})`
   - Listen `socket.onmatchmakermatched` → `socket.joinMatch(ticket.match_id)`
   - Show "Finding a random player..." + elapsed time while waiting
   - Button: "Cancel" → `socket.removeMatchmaker(ticket)`

4. **Game screen** (`screens/Game.tsx`):
   - Render 3x3 board from `gameState.board`
   - Highlight whose turn (you / opponent label)
   - On cell click: if `gameState.turn === session.user_id` → send OpCode 2
   - Listen `socket.onmatchdata`:
     - OpCode 1 → update gameState
   - On `gameState.status === "done"` → navigate to Result

5. **Result screen** (`screens/Result.tsx`):
   - Show winner / draw
   - Fetch leaderboard via `client.rpc("get_leaderboard", null)`
   - Show top-10 table: rank, username, wins
   - Button: "Play Again" → back to Lobby

**Done when:** two browser tabs complete a full game flow start to finish.

---

### Phase 5 — Bonus: Timer Mode (Day 4 — if time allows)

**Goal:** 30-second turn timer; auto-forfeit on timeout.

Approach — purely server-side, no client trust:

1. Add to `GameState`:
   ```go
   TurnStartedAt int64 // tick number when current turn began
   TurnLimit     int64 // ticks before forfeit (tickRate * 30)
   ```
2. In `MatchLoop`, before processing messages:
   ```go
   if gs.Status == "playing" && tick - gs.TurnStartedAt > gs.TurnLimit {
       // forfeit current player, other player wins
   }
   ```
3. Broadcast remaining seconds in `GameState` so client can show countdown
4. In Lobby: add mode selector "Classic" / "Timed" — pass as matchmaker metadata
5. `makeMatch` reads mode from metadata, passes to `MatchInit` params

---

### Phase 6 — Deployment (Day 4–5)

**Goal:** Public URL for frontend, public Nakama endpoint, both working together.

#### Nakama Backend — DigitalOcean Droplet ($6/mo)

1. Create Ubuntu 22.04 droplet
2. Install Docker + Docker Compose
3. Copy `deploy/docker-compose.prod.yml` + built Nakama image to server
4. Configure environment:
   - `NAKAMA_SERVER_KEY` — change from default "defaultkey"
   - Point domain or use raw IP
5. Open ports: 7350 (HTTP API), 7349 (gRPC), 7351 (console — restrict to your IP)
6. Run: `docker compose -f docker-compose.prod.yml up -d`
7. Verify: `curl http://<droplet-ip>:7350/healthcheck`

#### Frontend — Vercel (free)

1. `cd frontend && vercel deploy`
2. Set env var: `VITE_NAKAMA_HOST=<droplet-ip>` or domain
3. Vercel gives public HTTPS URL automatically

#### Domain (optional but looks professional)
- Buy a cheap `.dev` domain (~$12/yr)
- Point A record to droplet IP
- Use Caddy on the droplet for automatic HTTPS + reverse proxy to port 7350

---

## Validation Checklist (before submission)

### Backend
- [ ] Move rejected if not your turn
- [ ] Move rejected if position already taken
- [ ] Move rejected if game is already done
- [ ] Disconnect mid-game → opponent wins by forfeit
- [ ] Two games can run simultaneously (test with 4 tabs)
- [ ] Leaderboard updates correctly after each game
- [ ] `LeaderboardCreate` is called in `InitModule` (idempotent)

### Frontend
- [ ] Login persists across refresh (localStorage deviceId)
- [ ] Matchmaking cancel works
- [ ] Board is unclickable when it is not your turn
- [ ] Result screen shows correct winner
- [ ] "Play Again" works without page refresh

### Deployment
- [ ] Frontend accessible over HTTPS
- [ ] Nakama healthcheck returns 200
- [ ] Full game playable between two different devices

---

## README Sections to Write

1. **Setup & Installation** — docker compose up, frontend npm dev
2. **Architecture & Design Decisions** — why server-authoritative, why Nakama, why tick-based loop
3. **Anti-Cheat Design** — list every validation and why it exists
4. **Edge Cases Handled** — disconnect forfeit, duplicate moves, mid-tick game end
5. **Observability** — what is logged and where; how to debug a live match
6. **Deployment Process** — exact commands used
7. **API / Server Config** — Nakama server key, ports, environment vars
8. **How to Test Multiplayer** — open two incognito windows, steps to play a game
9. **Future Improvements** — spectator mode, reconnection, ELO ranking, room codes

---

## Key Reference Links

- Nakama Go runtime docs: https://heroiclabs.com/docs/nakama/server-framework/go-runtime/
- Match handler interface: https://heroiclabs.com/docs/nakama/concepts/server-authoritative-multiplayer/
- JS client SDK: https://heroiclabs.com/docs/nakama/client-libraries/javascript/
- Plugin builder image: `heroiclabs/nakama-pluginbuilder:3.22.0`
- Nakama server image: `heroiclabs/nakama:3.22.0`
