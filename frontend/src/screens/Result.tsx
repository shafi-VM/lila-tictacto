import { useEffect, useState } from "react";
import { client, getSession } from "../nakama";
import type { GameState, LeaderboardEntry } from "../types";

interface Props {
  userId: string;
  finalState: GameState;
  onPlayAgain: () => void;
}

export default function Result({ userId, finalState, onPlayAgain }: Props) {
  const [leaderboard, setLeaderboard] = useState<LeaderboardEntry[]>([]);
  const [loadingLb, setLoadingLb] = useState(true);

  const isDraw = finalState.winner === "draw";
  const iWon = finalState.winner === userId;
  const winnerName = isDraw
    ? null
    : finalState.usernames[finalState.winner] ?? "Unknown";

  useEffect(() => {
    async function fetchLeaderboard() {
      try {
        const session = getSession();
        const result = await client.rpc(session, "get_leaderboard", "");
        const payload = typeof result.payload === "string"
          ? JSON.parse(result.payload)
          : result.payload;
        setLeaderboard(Array.isArray(payload) ? payload : []);
      } catch (err) {
        console.error("Failed to fetch leaderboard", err);
      } finally {
        setLoadingLb(false);
      }
    }
    fetchLeaderboard();
  }, []);

  return (
    <div className="screen center-col">
      <div className="card result-card">
        {isDraw ? (
          <h2 className="result-draw">Draw!</h2>
        ) : iWon ? (
          <h2 className="result-win">Winner! +200 pts</h2>
        ) : (
          <h2 className="result-loss">Defeat</h2>
        )}

        {!isDraw && (
          <p className="subtitle">{winnerName} wins</p>
        )}

        <div className="leaderboard">
          <p className="label-small">Leaderboard</p>
          <div className="lb-header">
            <span>Player</span>
            <span>W</span>
            <span>L</span>
            <span title="Current streak">STK</span>
          </div>
          {loadingLb ? (
            <p className="subtitle">Loading...</p>
          ) : leaderboard.length === 0 ? (
            <p className="subtitle">No records yet</p>
          ) : (
            leaderboard.map((entry) => (
              <div key={entry.rank} className={`lb-row ${finalState.usernames[userId] === entry.username ? "lb-me" : ""}`}>
                <span>{entry.rank}. {entry.username}</span>
                <span>{entry.wins}</span>
                <span>{entry.losses}</span>
                <span>{entry.streak}</span>
              </div>
            ))
          )}
        </div>

        <button className="btn-primary" onClick={onPlayAgain}>
          Play Again
        </button>
      </div>
    </div>
  );
}
