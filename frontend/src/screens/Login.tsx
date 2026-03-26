import { useState } from "react";
import { authenticate } from "../nakama";

interface Props {
  onLogin: (userId: string, username: string) => void;
}

export default function Login({ onLogin }: Props) {
  const [username, setUsername] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    const name = username.trim();
    if (!name) return;
    setLoading(true);
    setError("");
    try {
      const session = await authenticate(name);
      onLogin(session.user_id!, name);
    } catch (err) {
      setError("Could not connect to server. Is Nakama running?");
      console.error(err);
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="screen center-col">
      <div className="card">
        <h2>Who are you?</h2>
        <form onSubmit={handleSubmit} className="col-gap">
          <input
            className="input"
            placeholder="Nickname"
            value={username}
            onChange={e => setUsername(e.target.value)}
            maxLength={20}
            autoFocus
            disabled={loading}
          />
          {error && <p className="error">{error}</p>}
          <button className="btn-primary" type="submit" disabled={loading || !username.trim()}>
            {loading ? "Connecting..." : "Continue"}
          </button>
        </form>
      </div>
    </div>
  );
}
