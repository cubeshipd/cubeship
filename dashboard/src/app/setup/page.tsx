"use client";

import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { AuthLayout } from "@/components/auth-layout";
import { ErrorAlert } from "@/components/error-alert";
import { TextField } from "@/components/text-field";
import { api, type SetupStatus } from "@/lib/api";
import { message } from "@/lib/errors";

// The one moment an account can be created without one already
// existing. If the instance is already claimed this page is a dead end,
// so it checks first and leaves.
export default function Setup() {
  const router = useRouter();
  const [ready, setReady] = useState(false);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  // Whether this instance asks for the token the installer printed. The
  // daemon writes it into the data directory, which only root can read,
  // so claiming the instance takes access to the host rather than
  // merely reaching the port first.
  const [tokenRequired, setTokenRequired] = useState(false);
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .get<SetupStatus>("/setup")
      .then((s) => {
        if (!s.needed) {
          router.replace("/login");
          return;
        }
        setTokenRequired(s.token_required);
        setReady(true);
      })
      .catch(() => setReady(true));
  }, [router]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.post("/setup", { username, password, token });
      // Setup signs you in, so there is nowhere to go but in.
      router.replace("/");
    } catch (err) {
      setError(message(err));
      setBusy(false);
    }
  }

  if (!ready) return null;

  return (
    <AuthLayout
      title="Claim this instance"
      description="This creates the only account that can be created without one. Everyone else is added from inside."
      footer="Setup closes the moment this succeeds. It cannot be run twice."
    >
      <form onSubmit={submit} className="space-y-5">
        <ErrorAlert error={error} />

        <TextField
          label="Username"
          hint="Lowercase letters, digits and dashes. Also your docker login user."
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          autoFocus
          autoComplete="username"
          spellCheck={false}
        />
        <TextField
          label="Password"
          hint="At least 12 characters."
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="new-password"
        />

        {tokenRequired && (
          <TextField
            label="Setup token"
            hint="Printed by the installer. Also in setup-token inside the data directory on the server."
            value={token}
            onChange={(e) => setToken(e.target.value)}
            autoComplete="off"
            spellCheck={false}
          />
        )}

        <ActionButton
          type="submit"
          busy={busy}
          size="lg"
          className="h-10 w-full"
          disabled={!username || !password || (tokenRequired && !token)}
        >
          {busy ? "Creating account" : "Create account"}
        </ActionButton>
      </form>
    </AuthLayout>
  );
}
