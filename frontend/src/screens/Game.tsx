import { useEffect, useRef, useState } from "react";
import { getSocket } from "../nakama";
import type { GameState } from "../types";
import { OpCode } from "../types";

interface Props {
  matchId: string;
  userId: string;
  initialState: GameState;
  onGameOver: (finalState: GameState) => void;
}

export default function Game({ matchId, userId, initialState, onGameOver }: Props) {
  const [gs, setGs] = useState<GameState>(initialState);

  const [secondsLeft, setSecondsLeft] = useState<number | null>(null);
  const countdownRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const mySymbol = gs.players[0] === userId ? "X" : "O";
  const oppId = gs.players[0] === userId ? gs.players[1] : gs.players[0];
  const myName = gs.usernames[userId] ?? "You";
  const oppName = gs.usernames[oppId] ?? "Opponent";
  const isMyTurn = gs.turn === userId;

  // Countdown ticker: runs client-side based on server-provided deadline.
  // The server is the authority on when time expires — this is display only.
  useEffect(() => {
    if (countdownRef.current) clearInterval(countdownRef.current);
    if (!gs.timed_mode || gs.turn_deadline_ms === 0 || gs.status !== "playing") {
      setSecondsLeft(null);
      return;
    }
    function update() {
      const remaining = Math.max(0, Math.ceil((gs.turn_deadline_ms - Date.now()) / 1000));
      setSecondsLeft(remaining);
    }
    update();
    countdownRef.current = setInterval(update, 200);
    return () => { if (countdownRef.current) clearInterval(countdownRef.current); };
  }, [gs.turn_deadline_ms, gs.timed_mode, gs.status]);

  useEffect(() => {
    const socket = getSocket();

    socket.onmatchdata = (data) => {
      if (data.op_code !== OpCode.GAME_STATE) return;
      const state: GameState = JSON.parse(
        new TextDecoder().decode(data.data as ArrayBuffer)
      );
      setGs(state);
      if (state.status === "done") {
        socket.onmatchdata = () => {};
        onGameOver(state);
      }
    };

    return () => {
      socket.onmatchdata = () => {};
    };
  }, [matchId, onGameOver]);

  async function handleCellClick(index: number) {
    // Client-side guard: block clicks when it's not your turn or cell is taken.
    // Server will also reject these — this just prevents sending pointless messages.
    if (!isMyTurn || gs.board[index] !== "" || gs.status !== "playing") return;

    try {
      await getSocket().sendMatchState(
        matchId,
        OpCode.MAKE_MOVE,
        JSON.stringify({ position: index })
      );
    } catch (err) {
      console.error("Failed to send move", err);
    }
  }

  return (
    <div className="screen center-col">
      <div className="players-row">
        <div className={`player-info ${isMyTurn ? "active" : ""}`}>
          <span className="player-name">{myName} (you)</span>
          <span className="symbol">{mySymbol}</span>
        </div>
        <div className="turn-indicator">
          {isMyTurn ? "Your Turn" : "Their Turn"}
          {secondsLeft !== null && (
            <div className={`countdown ${secondsLeft <= 5 ? "countdown-urgent" : ""}`}>
              {secondsLeft}s
            </div>
          )}
        </div>
        <div className={`player-info ${!isMyTurn ? "active" : ""}`}>
          <span className="symbol">{mySymbol === "X" ? "O" : "X"}</span>
          <span className="player-name">{oppName} (opp)</span>
        </div>
      </div>

      <div className="board">
        {gs.board.map((cell, i) => (
          <button
            key={i}
            className={`cell ${cell} ${isMyTurn && cell === "" && gs.status === "playing" ? "clickable" : ""}`}
            onClick={() => handleCellClick(i)}
            disabled={!isMyTurn || cell !== "" || gs.status !== "playing"}
          >
            {cell}
          </button>
        ))}
      </div>

      {gs.status === "waiting" && (
        <p className="subtitle">Waiting for opponent...</p>
      )}
    </div>
  );
}
