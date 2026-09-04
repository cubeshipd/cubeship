"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, type SetupStatus } from "@/lib/api";
import { Button, ErrorNote, Field, inputClass, message } from "@/components/ui";

export default function Login() {
  const router = useRouter();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // An unclaimed instance has nobody to sign in as.
  useEffect(() => {
    api.get<SetupStatus>("/setup").then((s) => s.needed && router.replace("/setup"));
  }, [router]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.post("/auth/login", { username, password });
      router.replace("/");
    } catch (err) {
      setError(message(err));
      setBusy(false);
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center p-6">
      <form onSubmit={submit} className="w-full max-w-sm rounded-lg border border-line bg-panel p-6">
        <h1 className="mb-5 text-lg font-semibold">Sign in to Cubeship</h1>
        <ErrorNote error={error} />
        <Field label="Username">
          <input
            className={inputClass}
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoFocus
            autoComplete="username"
          />
        </Field>
        <Field label="Password">
          <input
            className={inputClass}
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
          />
        </Field>
        <Button type="submit" variant="primary" disabled={busy} className="mt-2 w-full">
          {busy ? "Signing in…" : "Sign in"}
        </Button>
      </form>
    </div>
  );
}
