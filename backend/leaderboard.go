package main

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/heroiclabs/nakama-common/runtime"
)

const leaderboardID = "global_wins"

// playerMeta is stored in each leaderboard record's metadata field.
// Score (wins) is tracked by Nakama's built-in incr operator.
// Losses and streak require read-modify-write so we store them in metadata.
type playerMeta struct {
	Losses     int64 `json:"losses"`
	Streak     int64 `json:"streak"`
	BestStreak int64 `json:"best_streak"`
}

// initLeaderboard creates the leaderboard on server start.
// Idempotent — safe to call on every restart.
func initLeaderboard(ctx context.Context, nk runtime.NakamaModule, logger runtime.Logger) error {
	err := nk.LeaderboardCreate(ctx, leaderboardID, false, "desc", "incr", "", nil)
	if err != nil {
		logger.Error("Failed to create leaderboard", "err", err)
		return err
	}
	logger.Info("Leaderboard ready", "id", leaderboardID)
	return nil
}

// updateLeaderboard updates stats for both winner and loser after a game ends.
// Draws are not counted — no win, loss, or streak change.
func updateLeaderboard(ctx context.Context, nk runtime.NakamaModule, logger runtime.Logger, gs *GameState) {
	if gs.Winner == "" || gs.Winner == "draw" {
		return
	}

	// Determine loser from the two players
	loser := gs.Players[0]
	if gs.Winner == gs.Players[0] {
		loser = gs.Players[1]
	}

	updateRecord(ctx, nk, logger, gs.Winner, gs.Usernames[gs.Winner], true)
	updateRecord(ctx, nk, logger, loser, gs.Usernames[loser], false)
}

// updateRecord reads the current record for a player, updates stats, writes back.
// Fire-and-forget — called from MatchLoop without blocking the tick.
func updateRecord(ctx context.Context, nk runtime.NakamaModule, logger runtime.Logger, userID, username string, won bool) {
	// Read existing record to get current metadata
	records, _, _, _, err := nk.LeaderboardRecordsList(ctx, leaderboardID, []string{userID}, 1, "", 0)
	if err != nil {
		logger.Error("Failed to read leaderboard record", "userId", userID, "err", err)
		return
	}

	var meta playerMeta
	if len(records) > 0 && records[0].Metadata != "" {
		if err := json.Unmarshal([]byte(records[0].Metadata), &meta); err != nil {
			logger.Warn("Failed to parse metadata", "userId", userID, "err", err)
		}
	}

	// Update stats based on outcome
	var scoreIncr int64
	if won {
		scoreIncr = 1
		meta.Streak++
		if meta.Streak > meta.BestStreak {
			meta.BestStreak = meta.Streak
		}
	} else {
		scoreIncr = 0 // losses don't add to score (win count)
		meta.Losses++
		meta.Streak = 0 // any loss resets the streak
	}

	metaBytes, err := json.Marshal(meta)
	if err != nil {
		logger.Error("Failed to marshal metadata", "userId", userID, "err", err)
		return
	}
	metaMap := map[string]interface{}{}
	if err := json.Unmarshal(metaBytes, &metaMap); err != nil {
		logger.Error("Failed to convert metadata", "userId", userID, "err", err)
		return
	}

	_, err = nk.LeaderboardRecordWrite(ctx, leaderboardID, userID, username, scoreIncr, 0, metaMap, nil)
	if err != nil {
		logger.Error("Failed to write leaderboard record", "userId", userID, "err", err)
		return
	}
	logger.Info("Stats updated", "userId", userID, "won", won, "streak", meta.Streak, "losses", meta.Losses)
}

// LeaderboardEntry is the shape returned by the get_leaderboard RPC.
type LeaderboardEntry struct {
	Rank       int64  `json:"rank"`
	Username   string `json:"username"`
	Wins       int64  `json:"wins"`
	Losses     int64  `json:"losses"`
	Streak     int64  `json:"streak"`
	BestStreak int64  `json:"best_streak"`
}

// getLeaderboardRpc returns top-10 players with full stats.
func getLeaderboardRpc(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	records, _, _, _, err := nk.LeaderboardRecordsList(ctx, leaderboardID, nil, 10, "", 0)
	if err != nil {
		logger.Error("Failed to list leaderboard records", "err", err)
		return "", err
	}

	entries := make([]LeaderboardEntry, 0, len(records))
	for _, r := range records {
		var meta playerMeta
		if r.Metadata != "" {
			json.Unmarshal([]byte(r.Metadata), &meta) //nolint — best effort
		}
		entries = append(entries, LeaderboardEntry{
			Rank:       r.Rank,
			Username:   r.GetUsername().GetValue(),
			Wins:       r.Score,
			Losses:     meta.Losses,
			Streak:     meta.Streak,
			BestStreak: meta.BestStreak,
		})
	}

	data, err := json.Marshal(entries)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
