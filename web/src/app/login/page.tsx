"use client";

import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { AuthLayout } from "@/components/auth-layout";
import { ErrorAlert } from "@/components/error-alert";
import { TextField } from "@/components/text-field";
import { api, type SetupStatus } from "@/lib/api";
import { message } from "@/lib/errors";

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
    <AuthLayout
      title="Sign in"
      description="Use the account this instance was claimed with, or one an admin made for you."
      footer={
        <>
          Signing in from a terminal instead?{" "}
          <code className="text-muted-foreground">cubeship login</code> uses an API key.
        </>
      }
    >
      <form onSubmit={submit} className="space-y-5">
        <ErrorAlert error={error} />

        <TextField
          label="Username"
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          autoFocus
          autoComplete="username"
          spellCheck={false}
        />
        <TextField
          label="Password"
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="current-password"
        />

        <ActionButton type="submit" busy={busy} size="lg" className="h-10 w-full">
          {busy ? "Signing in" : "Sign in"}
        </ActionButton>
      </form>
    </AuthLayout>
  );
}
