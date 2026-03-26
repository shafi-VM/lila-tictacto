package main

import (
	"context"
	"database/sql"

	"github.com/heroiclabs/nakama-common/runtime"
)

// makeMatch is called by Nakama when the matchmaker finds enough compatible players.
// It creates a new server-authoritative match and returns its ID.
// Nakama automatically notifies the matched clients of the match ID via their
// matchmaker ticket, so they can call socket.joinMatch(matchId).
func makeMatch(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, entries []runtime.MatchmakerEntry) (string, error) {
	// Each entry is one matched player. We request min=max=2 from the client,
	// so this slice always has exactly 2 entries here.
	for _, entry := range entries {
		logger.Info("Matched player", "userId", entry.GetPresence().GetUserId(), "username", entry.GetPresence().GetUsername())
	}

	// Read numeric "timed" property from the first entry.
	// Both entries share the same value since the matchmaker query
	// guarantees only players with matching timed=N are paired.
	params := map[string]interface{}{"timed": false}
	if len(entries) > 0 {
		if props := entries[0].GetProperties(); props != nil {
			if v, ok := props["timed"].(float64); ok && v == 1 {
				params["timed"] = true
			}
		}
	}

	matchID, err := nk.MatchCreate(ctx, "tictactoe", params)
	if err != nil {
		logger.Error("Failed to create match", "err", err)
		return "", err
	}

	logger.Info("Match created for matched players", "matchId", matchID)
	return matchID, nil
}
