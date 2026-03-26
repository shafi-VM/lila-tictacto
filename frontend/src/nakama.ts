import { Client, Session, Socket } from "@heroiclabs/nakama-js";

const NAKAMA_HOST = import.meta.env.VITE_NAKAMA_HOST || "localhost";
const NAKAMA_PORT = import.meta.env.VITE_NAKAMA_PORT || "7350";
const NAKAMA_KEY  = import.meta.env.VITE_NAKAMA_KEY  || "defaultkey";
const USE_SSL     = import.meta.env.VITE_NAKAMA_SSL  === "true";

// Singleton client — one per browser session
export const client = new Client(NAKAMA_KEY, NAKAMA_HOST, NAKAMA_PORT, USE_SSL);

let _session: Session | null = null;
let _socket: Socket | null = null;

export function getSession(): Session {
  if (!_session) throw new Error("Not authenticated");
  return _session;
}

export function getSocket(): Socket {
  if (!_socket) throw new Error("Socket not connected");
  return _socket;
}

export async function authenticate(username: string): Promise<Session> {
  // Device auth: stable ID stored in localStorage so the same user
  // reconnects to the same account across page refreshes.
  // Per-username device ID: each name gets its own stable account.
  // This prevents a returning user's history from polluting a new username.
  const deviceKey = `deviceId_${username}`;
  let deviceId = localStorage.getItem(deviceKey);
  if (!deviceId) {
    deviceId = crypto.randomUUID();
    localStorage.setItem(deviceKey, deviceId);
  }

  // create=true creates the account if it doesn't exist.
  // The username param only applies on first creation — existing accounts
  // keep their old name, so we force-update and refresh the session token.
  _session = await client.authenticateDevice(deviceId, true, username);

  // Update the username on the account record.
  await client.updateAccount(_session, { username });

  // Refresh the session so the new username is baked into the JWT.
  // Without this, socket presence still reports the old name from the old token.
  _session = await client.sessionRefresh(_session);

  // Create and connect the WebSocket
  _socket = client.createSocket(USE_SSL, false);
  await _socket.connect(_session, true);

  return _session;
}

export async function disconnect(): Promise<void> {
  if (_socket) {
    _socket.disconnect(true);
    _socket = null;
  }
  _session = null;
}
