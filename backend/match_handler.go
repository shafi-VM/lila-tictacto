package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/heroiclabs/nakama-common/runtime"
)

// Message opcodes — the contract between client and server.
// Client cannot be trusted to send valid game state; these opcodes
// define the only actions the server will accept.
const (
	OpCodeGameState = 1 // server → clients: authoritative game state broadcast
	OpCodeMakeMove  = 2 // client → server: player declares intent to move
	OpCodeSystemMsg = 3 // server → clients: system events (forfeit notice, etc.)
)

// GameState is the single source of truth for a match.
// It lives on the server only; clients receive a copy via broadcast.
type GameState struct {
	Board          [9]string         `json:"board"`            // "" | "X" | "O"
	Turn           string            `json:"turn"`             // userId whose turn it is
	Players        [2]string         `json:"players"`          // [0]=X, [1]=O (userId)
	Usernames      map[string]string `json:"usernames"`        // userId → display name
	Status         string            `json:"status"`           // "waiting" | "playing" | "done"
	Winner         string            `json:"winner"`           // userId | "draw" | ""
	TimedMode      bool              `json:"timed_mode"`       // true = 30s per turn
	TurnDeadlineMs int64             `json:"turn_deadline_ms"` // unix ms when current turn expires (0 if classic)
	TurnLimitSec   int               `json:"turn_limit_sec"`   // seconds per turn (30)
}

// MakeMoveMsg is the only payload the server accepts from clients.
type MakeMoveMsg struct {
	Position int `json:"position"` // 0–8, row-major order
}

// MatchHandler implements runtime.Match — Nakama calls these methods
// on every match lifecycle event and on every tick.
type MatchHandler struct{}

func newMatchHandler(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule) (runtime.Match, error) {
	return &MatchHandler{}, nil
}

// MatchInit is called once when the match is created.
// Returns initial state, tick rate, and a label for match listing/discovery.
func (m *MatchHandler) MatchInit(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, params map[string]interface{}) (interface{}, int, string) {
	timed := false
	if v, ok := params["timed"].(bool); ok {
		timed = v
	}

	state := &GameState{
		Board:        [9]string{},
		Status:       "waiting",
		Usernames:    make(map[string]string),
		TimedMode:    timed,
		TurnLimitSec: 30,
	}

	// 10 ticks/second is more than sufficient for a turn-based game.
	// Higher tick rates waste CPU; lower rates add latency to move validation.
	tickRate := 10
	label := `{"mode":"tictactoe"}`
	if timed {
		label = `{"mode":"tictactoe","timed":true}`
	}
	logger.Info("Match initialised", "timed", timed)
	return state, tickRate, label
}

// MatchJoinAttempt is called before a player fully joins.
// Rejecting here prevents the player from ever receiving match state.
func (m *MatchHandler) MatchJoinAttempt(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, presence runtime.Presence, metadata map[string]string) (interface{}, bool, string) {
	gs := state.(*GameState)

	// Count current players
	filled := 0
	for _, p := range gs.Players {
		if p != "" {
			filled++
		}
	}
	if filled >= 2 {
		logger.Info("Join rejected — match full", "userId", presence.GetUserId())
		return state, false, "match is full"
	}
	return state, true, ""
}

// MatchJoin is called after a player is confirmed to have joined.
// Assign symbols and start the game once both seats are filled.
func (m *MatchHandler) MatchJoin(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, presences []runtime.Presence) interface{} {
	gs := state.(*GameState)

	for _, p := range presences {
		uid := p.GetUserId()
		username := p.GetUsername()

		if gs.Players[0] == "" {
			gs.Players[0] = uid // first joiner is X
		} else if gs.Players[1] == "" {
			gs.Players[1] = uid // second joiner is O
		}
		gs.Usernames[uid] = username
		logger.Info("Player joined", "userId", uid, "username", username)
	}

	// Both seats filled — start the game
	if gs.Players[0] != "" && gs.Players[1] != "" {
		gs.Status = "playing"
		gs.Turn = gs.Players[0] // X always goes first
		setTurnDeadline(gs)
		logger.Info("Match started", "X", gs.Players[0], "O", gs.Players[1])
	}

	broadcastState(dispatcher, gs, logger)
	return gs
}

// MatchLeave is called when a player disconnects or explicitly leaves.
// Award the win to the remaining player — abandoning a game is a forfeit.
func (m *MatchHandler) MatchLeave(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, presences []runtime.Presence) interface{} {
	gs := state.(*GameState)

	for _, p := range presences {
		uid := p.GetUserId()
		logger.Info("Player left", "userId", uid, "status", gs.Status)

		if gs.Status == "playing" {
			// Forfeit: the player who left loses
			if uid == gs.Players[0] {
				gs.Winner = gs.Players[1]
			} else {
				gs.Winner = gs.Players[0]
			}
			gs.Status = "done"
			logger.Info("Match ended by forfeit", "winner", gs.Winner, "forfeiter", uid)
			updateLeaderboard(ctx, nk, logger, gs) // write DB before broadcast so client sees fresh data
			broadcastState(dispatcher, gs, logger)
		}
	}
	return gs
}

// MatchLoop is the game tick — called at tickRate per second.
// This is where all move validation and state mutation happens.
// The server is the sole authority: no client input is applied without passing every check.
func (m *MatchHandler) MatchLoop(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, messages []runtime.MatchData) interface{} {
	gs := state.(*GameState)

	// Timer check: enforced server-side to prevent client-side tampering.
	// If the deadline has passed, the current player forfeits automatically.
	if gs.TimedMode && gs.Status == "playing" && gs.TurnDeadlineMs > 0 {
		if time.Now().UnixMilli() > gs.TurnDeadlineMs {
			forfeiter := gs.Turn
			if gs.Turn == gs.Players[0] {
				gs.Winner = gs.Players[1]
			} else {
				gs.Winner = gs.Players[0]
			}
			gs.Status = "done"
			logger.Info("Turn timeout — forfeit", "forfeiter", forfeiter, "winner", gs.Winner)
			updateLeaderboard(ctx, nk, logger, gs)
			broadcastState(dispatcher, gs, logger)
			return gs
		}
	}

	for _, msg := range messages {
		// Guard: stop processing as soon as game is done within this tick.
		// Using break (not continue) ensures no further messages in this batch
		// are processed after the game ends mid-tick.
		if gs.Status != "playing" {
			break
		}

		if msg.GetOpCode() != OpCodeMakeMove {
			continue
		}

		senderID := msg.GetUserId()

		// Anti-cheat: reject out-of-turn moves.
		// The client cannot be trusted to enforce turn order.
		if senderID != gs.Turn {
			logger.Debug("Rejected out-of-turn move", "sender", senderID, "expected", gs.Turn)
			continue
		}

		var move MakeMoveMsg
		if err := json.Unmarshal(msg.GetData(), &move); err != nil {
			logger.Warn("Failed to parse move payload", "userId", senderID, "err", err)
			continue
		}

		// Anti-cheat: bounds check — client-supplied position must be valid.
		if move.Position < 0 || move.Position > 8 {
			logger.Debug("Rejected out-of-bounds move", "userId", senderID, "position", move.Position)
			continue
		}

		// Anti-cheat: reject moves on occupied cells.
		// This also handles network retries sending the same move twice.
		if gs.Board[move.Position] != "" {
			logger.Debug("Rejected move on occupied cell", "userId", senderID, "position", move.Position)
			continue
		}

		// All checks passed — apply the move
		symbol := "X"
		if senderID == gs.Players[1] {
			symbol = "O"
		}
		gs.Board[move.Position] = symbol
		logger.Info("Move applied", "userId", senderID, "symbol", symbol, "position", move.Position)

		// Check for winner
		if winner := checkWinner(gs.Board); winner != "" {
			if winner == "X" {
				gs.Winner = gs.Players[0]
			} else {
				gs.Winner = gs.Players[1]
			}
			gs.Status = "done"
			logger.Info("Match won", "winner", gs.Winner, "symbol", winner)
			updateLeaderboard(ctx, nk, logger, gs)
			broadcastState(dispatcher, gs, logger)
			break
		}

		// Check for draw
		if isBoardFull(gs.Board) {
			gs.Winner = "draw"
			gs.Status = "done"
			logger.Info("Match ended in draw")
			broadcastState(dispatcher, gs, logger)
			break
		}

		// Switch turn and reset deadline for next player
		if gs.Turn == gs.Players[0] {
			gs.Turn = gs.Players[1]
		} else {
			gs.Turn = gs.Players[0]
		}
		setTurnDeadline(gs)
		broadcastState(dispatcher, gs, logger)
	}

	return gs
}

// MatchTerminate is called when Nakama shuts down the match forcefully.
func (m *MatchHandler) MatchTerminate(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, graceSeconds int) interface{} {
	logger.Info("Match terminated", "graceSeconds", graceSeconds)
	return state
}

// MatchSignal handles out-of-band signals sent to the match (not used yet).
func (m *MatchHandler) MatchSignal(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, data string) (interface{}, string) {
	return state, ""
}

// broadcastState sends the full authoritative game state to every player in the match.
// Always broadcasting full state (not diffs) keeps clients simple and avoids
// desync bugs from missed partial updates.
func broadcastState(dispatcher runtime.MatchDispatcher, gs *GameState, logger runtime.Logger) {
	data, err := json.Marshal(gs)
	if err != nil {
		logger.Error("Failed to marshal game state", "err", err)
		return
	}
	// nil presences = broadcast to all; reliable = true ensures delivery
	if err := dispatcher.BroadcastMessage(OpCodeGameState, data, nil, nil, true); err != nil {
		logger.Error("Failed to broadcast state", "err", err)
	}
}

// checkWinner returns "X", "O", or "" — checks all 8 winning lines.
func checkWinner(board [9]string) string {
	lines := [8][3]int{
		{0, 1, 2}, {3, 4, 5}, {6, 7, 8}, // rows
		{0, 3, 6}, {1, 4, 7}, {2, 5, 8}, // cols
		{0, 4, 8}, {2, 4, 6},             // diagonals
	}
	for _, l := range lines {
		if board[l[0]] != "" && board[l[0]] == board[l[1]] && board[l[1]] == board[l[2]] {
			return board[l[0]]
		}
	}
	return ""
}

// setTurnDeadline sets the unix-ms deadline for the current turn.
// No-op in classic mode. Called whenever the active player changes.
func setTurnDeadline(gs *GameState) {
	if !gs.TimedMode {
		gs.TurnDeadlineMs = 0
		return
	}
	gs.TurnDeadlineMs = time.Now().UnixMilli() + int64(gs.TurnLimitSec)*1000
}

// isBoardFull returns true when no empty cells remain.
func isBoardFull(board [9]string) bool {
	for _, cell := range board {
		if cell == "" {
			return false
		}
	}
	return true
}
