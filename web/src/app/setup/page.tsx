"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, type SetupStatus } from "@/lib/api";
import { Button, ErrorNote, Field, inputClass, message } from "@/components/ui";

// The one moment an account can be created without one already
// existing. If the instance is already claimed this page is a dead end,
// so it checks first and leaves.
export default function Setup() {
  const router = useRouter();
  const [ready, setReady] = useState(false);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .get<SetupStatus>("/setup")
      .then((s) => (s.needed ? setReady(true) : router.replace("/login")))
      .catch(() => setReady(true));
  }, [router]);

  if (!ready) return null;

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.post("/setup", { username, password });
      // Setup signs you in, so there is nowhere to go but in.
      router.replace("/");
    } catch (err) {
      setError(message(err));
      setBusy(false);
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center p-6">
      <form onSubmit={submit} className="w-full max-w-sm rounded-lg border border-line bg-panel p-6">
        <h1 className="text-lg font-semibold">Claim this instance</h1>
        <p className="mt-1 mb-5 text-sm text-muted">
          This creates the only account that can be created without one. Everyone else is added
          from inside.
        </p>
        <ErrorNote error={error} />
        <Field label="Username" hint="Lowercase letters, digits and dashes. Also your docker login user.">
          <input
            className={inputClass}
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoFocus
            autoComplete="username"
          />
        </Field>
        <Field label="Password" hint="At least 12 characters.">
          <input
            className={inputClass}
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="new-password"
          />
        </Field>
        <Button type="submit" variant="primary" disabled={busy} className="mt-2 w-full">
          {busy ? "Creating…" : "Create account"}
        </Button>
      </form>
    </div>
  );
}
