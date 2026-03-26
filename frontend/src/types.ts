// Must mirror the GameState struct in backend/match_handler.go exactly.
// If the server shape changes, update here too.
export interface GameState {
  board: string[];                   // 9 elements: "" | "X" | "O"
  turn: string;                      // userId whose turn it is
  players: string[];                 // [0]=X userId, [1]=O userId
  usernames: Record<string, string>; // userId → display name
  status: "waiting" | "playing" | "done";
  winner: string;                    // userId | "draw" | ""
  timed_mode: boolean;               // true = 30s per turn
  turn_deadline_ms: number;          // unix ms when current turn expires (0 = classic)
  turn_limit_sec: number;            // seconds per turn (30)
}

export interface LeaderboardEntry {
  rank: number;
  username: string;
  wins: number;
  losses: number;
  streak: number;
  best_streak: number;
}

// Opcodes — must match constants in backend/match_handler.go
export const OpCode = {
  GAME_STATE: 1,
  MAKE_MOVE: 2,
  SYSTEM_MSG: 3,
} as const;
