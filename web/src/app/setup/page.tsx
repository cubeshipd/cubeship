"use client";

import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { AuthLayout } from "@/components/auth-layout";
import { ErrorAlert } from "@/components/error-alert";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { api, type SetupStatus } from "@/lib/api";
import { message } from "@/lib/errors";

// The one moment an account can be created without one already
// existing. If the instance is already claimed this page is a dead end,
// so it checks first and leaves.
//
// Two steps, one request. The account and the organization are created
// in a single transaction — a user with nowhere to work would be
// unrecoverable, since setup refuses to run twice — so the first step
// only holds what the second one submits.
export default function Setup() {
  const router = useRouter();
  const [ready, setReady] = useState(false);
  const [step, setStep] = useState<"account" | "organization">("account");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [org, setOrg] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .get<SetupStatus>("/setup")
      .then((s) => (s.needed ? setReady(true) : router.replace("/login")))
      .catch(() => setReady(true));
  }, [router]);

  function next(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setStep("organization");
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.post("/setup", { username, password, org });
      // Setup signs you in, so there is nowhere to go but in.
      router.replace("/");
    } catch (err) {
      setError(message(err));
      setBusy(false);
    }
  }

  if (!ready) return null;

  if (step === "account") {
    return (
      <AuthLayout
        title="Claim this instance"
        description="This creates the only account that can be created without one. Everyone else is added from inside."
        footer="Step 1 of 2. Nothing is created until the second step."
      >
        <form onSubmit={next} className="space-y-5">
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

          <ActionButton
            type="submit"
            busy={false}
            size="lg"
            className="h-10 w-full"
            disabled={!username || !password}
          >
            Continue
          </ActionButton>
        </form>
      </AuthLayout>
    );
  }

  return (
    <AuthLayout
      title="Your first organization"
      description="Everything you deploy lives inside one. Projects, environments and apps all hang off this name."
      footer="Setup closes the moment this succeeds. It cannot be run twice."
    >
      <form onSubmit={submit} className="space-y-5">
        <ErrorAlert error={error} />

        <TextField
          label="Organization"
          hint="Lowercase letters, digits and dashes. It is the first part of every app's image path, and cannot be changed later."
          value={org}
          onChange={(e) => setOrg(e.target.value)}
          autoFocus
          spellCheck={false}
          placeholder="acme"
        />

        <ActionButton type="submit" busy={busy} size="lg" className="h-10 w-full" disabled={!org}>
          {busy ? "Creating" : "Create account and organization"}
        </ActionButton>

        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="w-full"
          onClick={() => {
            setError(null);
            setStep("account");
          }}
        >
          Back
        </Button>
      </form>
    </AuthLayout>
  );
}
