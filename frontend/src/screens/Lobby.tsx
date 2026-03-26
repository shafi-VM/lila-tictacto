import { useEffect, useRef, useState } from "react";
import { getSocket } from "../nakama";
import type { GameState } from "../types";

interface Props {
  userId: string;
  username: string;
  onMatchFound: (matchId: string, initialState: GameState) => void;
}

export default function Lobby({ userId: _userId, username, onMatchFound }: Props) {
  const [mode, setMode] = useState<"classic" | "timed">("classic");
  const [searching, setSearching] = useState(false);
  const [elapsed, setElapsed] = useState(0);
  const ticketRef = useRef<string | null>(null);
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    const socket = getSocket();

    // Nakama calls this when 2 players are matched and the server has
    // auto-created the match via RegisterMatchmakerMatched hook.
    socket.onmatchmakermatched = async (matched) => {
      const matchId = matched.match_id;
      if (!matchId) return;

      ticketRef.current = null;
      stopTimer();

      // Set handler BEFORE joinMatch to avoid missing the MatchJoin broadcast.
      // The server broadcasts state immediately when both players join,
      // which can arrive during the joinMatch await.
      // Wait for "playing" status — first joiner may get a "waiting" broadcast.
      socket.onmatchdata = (data) => {
        if (data.op_code !== 1) return;
        const state: GameState = JSON.parse(
          new TextDecoder().decode(data.data as ArrayBuffer)
        );
        if (state.status === "playing" || state.status === "done") {
          socket.onmatchdata = () => {};
          onMatchFound(matchId, state);
        }
      };

      try {
        await socket.joinMatch(matchId);
      } catch (err) {
        console.error("Failed to join match", err);
        socket.onmatchdata = () => {};
        setSearching(false);
      }
    };

    return () => {
      socket.onmatchmakermatched = () => {};
      socket.onmatchdata = () => {};
      stopTimer();
      setSearching(false);
      setElapsed(0);
    };
  }, [onMatchFound]);

  function startTimer() {
    setElapsed(0);
    timerRef.current = setInterval(() => setElapsed(s => s + 1), 1000);
  }

  function stopTimer() {
    if (timerRef.current) {
      clearInterval(timerRef.current);
      timerRef.current = null;
    }
  }

  async function findMatch() {
    const socket = getSocket();
    setSearching(true);
    startTimer();
    try {
      // Use numeric property so Nakama's matchmaker can filter by exact value.
      // String property queries are unreliable in Nakama's Bleve-based engine.
      // timed=1 means timed mode, timed=0 means classic.
      const timed = mode === "timed" ? 1 : 0;
      const query = `+properties.timed:${timed}`;
      const result = await socket.addMatchmaker(query, 2, 2, {}, { timed });
      ticketRef.current = result.ticket;
    } catch (err) {
      console.error("Matchmaker error", err);
      setSearching(false);
      stopTimer();
    }
  }

  async function cancelSearch() {
    if (!ticketRef.current) return;
    try {
      await getSocket().removeMatchmaker(ticketRef.current);
    } catch (err) {
      console.error("Cancel error", err);
    }
    ticketRef.current = null;
    setSearching(false);
    stopTimer();
  }

  return (
    <div className="screen center-col">
      <div className="card">
        <p className="label-small">Playing as <strong>{username}</strong></p>
        {!searching ? (
          <>
            <div className="mode-toggle">
              <button
                className={`mode-btn ${mode === "classic" ? "active" : ""}`}
                onClick={() => setMode("classic")}
              >
                Classic
              </button>
              <button
                className={`mode-btn ${mode === "timed" ? "active" : ""}`}
                onClick={() => setMode("timed")}
              >
                Timed (30s)
              </button>
            </div>
            <button className="btn-primary" onClick={findMatch}>
              Find Match
            </button>
          </>
        ) : (
          <>
            <p className="title">Finding a random player...</p>
            <p className="subtitle">Searching for {elapsed}s</p>
            <button className="btn-ghost" onClick={cancelSearch}>
              Cancel
            </button>
          </>
        )}
      </div>
    </div>
  );
}
