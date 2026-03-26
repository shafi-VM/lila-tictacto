import { useState } from "react";
import Login from "./screens/Login";
import Lobby from "./screens/Lobby";
import Game from "./screens/Game";
import Result from "./screens/Result";
import { getSocket } from "./nakama";
import type { GameState } from "./types";

type Screen = "login" | "lobby" | "game" | "result";

export default function App() {
  const [screen, setScreen] = useState<Screen>("login");
  const [userId, setUserId] = useState("");
  const [username, setUsername] = useState("");
  const [matchId, setMatchId] = useState("");
  const [gameState, setGameState] = useState<GameState | null>(null);
  const [finalState, setFinalState] = useState<GameState | null>(null);

  function handleLogin(uid: string, uname: string) {
    setUserId(uid);
    setUsername(uname);
    setScreen("lobby");
  }

  function handleMatchFound(mid: string, state: GameState) {
    setMatchId(mid);
    setGameState(state);
    setScreen("game");
  }

  function handleGameOver(state: GameState) {
    setFinalState(state);
    setScreen("result");
  }

  async function handlePlayAgain() {
    // Explicitly leave the finished match so the socket is clean
    // before the player enters the matchmaker pool again.
    if (matchId) {
      try { await getSocket().leaveMatch(matchId); } catch (_) {}
    }
    setMatchId("");
    setGameState(null);
    setFinalState(null);
    setScreen("lobby");
  }

  return (
    <>
      {screen === "login" && (
        <Login onLogin={handleLogin} />
      )}
      {screen === "lobby" && (
        <Lobby
          userId={userId}
          username={username}
          onMatchFound={handleMatchFound}
        />
      )}
      {screen === "game" && gameState && (
        <Game
          matchId={matchId}
          userId={userId}
          initialState={gameState}
          onGameOver={handleGameOver}
        />
      )}
      {screen === "result" && finalState && (
        <Result
          userId={userId}
          finalState={finalState}
          onPlayAgain={handlePlayAgain}
        />
      )}
    </>
  );
}
