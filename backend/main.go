package main

import (
	"context"
	"database/sql"

	"github.com/heroiclabs/nakama-common/runtime"
)

// InitModule is the single entry point Nakama calls when loading this plugin.
// Registration order matters: match handler first, then matchmaker hook, then RPCs.
func InitModule(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, initializer runtime.Initializer) error {
	// Register the server-authoritative match handler.
	// "tictactoe" is the name clients use when creating matches manually;
	// makeMatch also references it when auto-creating from matchmaker.
	if err := initializer.RegisterMatch("tictactoe", newMatchHandler); err != nil {
		logger.Error("Failed to register match handler", "err", err)
		return err
	}

	// Hook into Nakama's matchmaker — when 2 players are paired,
	// makeMatch auto-creates a server-authoritative match for them.
	if err := initializer.RegisterMatchmakerMatched(makeMatch); err != nil {
		logger.Error("Failed to register matchmaker hook", "err", err)
		return err
	}

	// RPC: client can fetch the top-10 leaderboard at any time
	if err := initializer.RegisterRpc("get_leaderboard", getLeaderboardRpc); err != nil {
		logger.Error("Failed to register leaderboard RPC", "err", err)
		return err
	}

	// RPC: smoke test — remove before production
	if err := initializer.RegisterRpc("ping", pingRpc); err != nil {
		logger.Error("Failed to register ping RPC", "err", err)
		return err
	}

	// Ensure leaderboard exists before any match can write to it.
	// Idempotent — safe on every server restart.
	if err := initLeaderboard(ctx, nk, logger); err != nil {
		return err
	}

	logger.Info("Tic-Tac-Toe module loaded successfully")
	return nil
}

func pingRpc(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	return `{"message":"pong"}`, nil
}
