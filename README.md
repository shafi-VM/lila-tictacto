# Multiplayer Tic-Tac-Toe

A production-ready multiplayer Tic-Tac-Toe game built with [Nakama](https://heroiclabs.com/) as a server-authoritative backend and React + TypeScript on the frontend.

**Live demo:** [lila-tictacto.vercel.app](https://lila-tictacto.vercel.app)
**Nakama server:** [lila-tictacto-production.up.railway.app](https://lila-tictacto-production.up.railway.app)

---

## Stack

| Layer | Technology |
|-------|-----------|
| Game server | Nakama 3.22.0 |
| Backend plugin | Go 1.21 (compiled as `.so` shared library) |
| Database | PostgreSQL (managed by Nakama) |
| Frontend | React 18 + TypeScript + Vite |
| Backend hosting | Railway |
| Frontend hosting | Vercel |

---

## Architecture

```
Client A (React) ──┐
                   ├── WebSocket ──► Nakama Server ──► Match Handler (Go plugin)
Client B (React) ──┘                     │
                                         ▼
                                    PostgreSQL
                                (sessions + leaderboard)
```

**Server-authoritative design:** all game state lives on the server. Clients send intent (a move position). The server validates, mutates state, and broadcasts the authoritative result back to all players. Clients only render what the server says — they cannot modify game state directly.

---

## Features

- **Classic mode** — unlimited turn time
- **Timed mode** — 30 seconds per turn, enforced server-side; timeout = automatic forfeit
- **Matchmaking** — Nakama's built-in matchmaker pairs two players by mode
- **Leaderboard** — persistent wins, losses, and win streaks across sessions
- **Forfeit on disconnect** — leaving mid-game awards the win to the opponent
- **Device auth** — per-username stable accounts via localStorage; no sign-up required

---

## Anti-Cheat Design

All validation happens in `MatchLoop` on the server. The client cannot bypass any of these checks:

| Check | Why it exists |
|-------|--------------|
| Sender must be the current turn player | Prevents a player from moving on their opponent's turn |
| Position must be 0–8 | Rejects out-of-bounds payloads from malicious clients |
| Position must be empty | Prevents overwriting existing moves; also handles network retries sending the same move twice |
| `status == "playing"` guard | Rejects moves after the game ends; uses `break` (not `continue`) to stop processing all messages in the same tick once the game is over |
| Timer check runs before message processing | Prevents a player from sneaking in a move after their time has expired |
| Turn deadline set by the server | Client countdown is display-only; the server is the authority on when time expires |

Invalid inputs are **silently dropped** (not rejected with an error). This avoids leaking server state to a cheating client and keeps the message protocol simple.

---

## Message Protocol

| OpCode | Direction | Payload | Purpose |
|--------|-----------|---------|---------|
| 1 | server → clients | `GameState` JSON | Full authoritative state broadcast |
| 2 | client → server | `{"position": 0–8}` | Player declares intent to move |
| 3 | server → clients | `{"message": string}` | System events (reserved) |

The server always broadcasts **full state** (not diffs). This keeps clients simple and eliminates desync bugs from missed partial updates.

---

## Edge Cases Handled

- **Disconnect mid-game** — `MatchLeave` detects the disconnect and immediately awards the win to the remaining player
- **Both players join race** — `MatchJoinAttempt` rejects a third joiner; the match is sealed at 2 players
- **Game ends mid-tick** — if a winning move arrives in the same tick as another message, the `break` on `status != "playing"` prevents the second message from being processed
- **Leaderboard race condition** — `updateLeaderboard` is called before `broadcastState`; the client sees fresh stats on the result screen without any delay
- **Stale username** — `updateAccount` + `sessionRefresh` after every login ensures the JWT always carries the correct display name
- **Play Again** — `leaveMatch` is called before re-entering the matchmaker pool, keeping the socket clean

---

## Leaderboard

Nakama's built-in leaderboard tracks **wins** (score). Losses and streaks require read-modify-write and are stored in each record's **metadata** field as JSON:

```json
{ "losses": 2, "streak": 3, "best_streak": 5 }
```

Both winner and loser records are updated on every game end. Draws are not counted.

---

## Local Development

**Prerequisites:** Docker, Node.js 18+

```bash
# 1. Start Nakama + PostgreSQL
cd deploy
docker compose up

# Nakama console: http://localhost:7351 (admin / password)
# Nakama API:     http://localhost:7350

# 2. Start the frontend
cd frontend
npm install
npm run dev
# → http://localhost:5173
```

Open two browser tabs at `localhost:5173` and enter different usernames to play.

---

## Deployment

### Backend (Railway)

The backend is a Docker image built from `backend/Dockerfile` using a multi-stage build:

1. `heroiclabs/nakama-pluginbuilder:3.22.0` compiles the Go plugin into `backend.so`
2. `heroiclabs/nakama:3.22.0` embeds the `.so` and runs `start.sh`

`start.sh` strips the Railway `postgresql://` prefix from `DATABASE_URL` and runs migrations before starting Nakama:

```sh
DB_ADDR="${DATABASE_URL#postgresql://}"
DB_ADDR="${DB_ADDR#postgres://}"
DB_ADDR="${DB_ADDR%%\?*}"
/nakama/nakama migrate up --database.address "$DB_ADDR"
exec /nakama/nakama --database.address "$DB_ADDR" ...
```

**Environment variables required:**

| Variable | Value |
|----------|-------|
| `DATABASE_URL` | Provided by Railway Postgres (internal) |
| `NAKAMA_HTTP_KEY` | `defaultkey` (change for production) |

### Frontend (Vercel)

```bash
cd frontend
npx vercel --prod
```

**Environment variables required:**

| Variable | Value |
|----------|-------|
| `VITE_NAKAMA_HOST` | Your Railway domain (without `https://`) |
| `VITE_NAKAMA_PORT` | `443` |
| `VITE_NAKAMA_SSL` | `true` |

---

## How to Test Multiplayer

1. Open [lila-tictacto.vercel.app](https://lila-tictacto.vercel.app) in two browser windows (one incognito)
2. Enter different usernames in each window
3. Select the same mode (Classic or Timed) in both
4. Click **Find Match** in both — they will be paired automatically
5. Play the game; the result screen shows the leaderboard with W/L/STK

To test forfeit: close one tab mid-game — the other player should see an instant win.

---

## Project Structure

```
tictactoe/
├── backend/
│   ├── main.go             # InitModule — registers match handler, matchmaker, RPCs
│   ├── match_handler.go    # Server-authoritative game logic + timer
│   ├── matchmaker.go       # Auto-creates match when 2 players are paired
│   ├── leaderboard.go      # Win/loss/streak tracking + get_leaderboard RPC
│   ├── start.sh            # Production startup (migrations + server)
│   ├── Dockerfile          # Multi-stage: pluginbuilder → nakama image
│   └── go.mod
├── frontend/
│   └── src/
│       ├── nakama.ts       # Singleton Nakama client + device auth
│       ├── App.tsx         # Screen router
│       ├── types.ts        # GameState + OpCode (mirrors backend)
│       └── screens/
│           ├── Login.tsx   # Username entry
│           ├── Lobby.tsx   # Matchmaking + mode toggle
│           ├── Game.tsx    # Board + countdown timer
│           └── Result.tsx  # Winner/defeat + leaderboard
├── deploy/
│   └── docker-compose.yml  # Local dev environment
└── vercel.json             # Vercel build config
```

---

## Future Improvements

- **Reconnection** — track `expectedPlayers` in `GameState` and allow re-entry in `MatchJoinAttempt` if the userId matches a known player
- **ELO ranking** — replace win-count leaderboard with an ELO system for more meaningful matchmaking
- **Spectator mode** — read-only match presence using Nakama's `MatchJoinAttempt` allow-list
- **Room codes** — let players create a private match and share a 6-character code to invite a friend directly
- **Move history** — store moves server-side to enable game replay
