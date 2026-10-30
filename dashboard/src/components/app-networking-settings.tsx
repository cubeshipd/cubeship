"use client";

import { PlusIcon, Trash2Icon } from "lucide-react";
import { useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ErrorAlert } from "@/components/error-alert";
import { SectionHeader } from "@/components/section-header";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { type App, type AppHealthCheck, api } from "@/lib/api";
import { message } from "@/lib/errors";

const newCheck = (port: number): AppHealthCheck => ({
  path: "/healthz",
  port,
  initial_delay_seconds: 0,
  period_seconds: 10,
  timeout_seconds: 5,
  failure_threshold: 3,
  success_threshold: 1,
});

type HealthCheckDraft = AppHealthCheck & { key: string };
const draftChecks = (checks: AppHealthCheck[]): HealthCheckDraft[] =>
  checks.map((check) => ({ ...check, key: crypto.randomUUID() }));

export function AppNetworkingSettings({ app, onSaved }: { app: App; onSaved: (app: App) => void }) {
  const [ports, setPorts] = useState(app.internal_ports ?? []);
  const [checks, setChecks] = useState(draftChecks(app.health_checks ?? []));
  const [newPort, setNewPort] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    setPorts(app.internal_ports ?? []);
    setChecks(draftChecks(app.health_checks ?? []));
  }, [app.internal_ports, app.health_checks]);

  const wireChecks = checks.map(({ key: _key, ...check }) => check);
  const dirty =
    JSON.stringify(ports) !== JSON.stringify(app.internal_ports ?? []) ||
    JSON.stringify(wireChecks) !== JSON.stringify(app.health_checks ?? []);

  function addPort() {
    const port = Number(newPort);
    if (!Number.isInteger(port) || port < 1 || port > 65535 || ports.includes(port)) return;
    setPorts([...ports, port].sort((a, b) => a - b));
    setNewPort("");
    setSaved(false);
  }

  function updateCheck(index: number, patch: Partial<AppHealthCheck>) {
    setChecks(checks.map((check, i) => (i === index ? { ...check, ...patch } : check)));
    setSaved(false);
  }

  async function save() {
    setBusy(true);
    setSaved(false);
    setError(null);
    try {
      onSaved(
        await api.put<App>(`/apps/${app.reference}/networking`, {
          internal_ports: ports,
          health_checks: wireChecks,
        }),
      );
      setSaved(true);
    } catch (cause) {
      setError(message(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card>
      <CardContent className="space-y-5">
        <div>
          <SectionHeader
            title="Internal ports and health checks"
            sub="Declare the ports this container listens on. Every health check must pass before a deploy is ready."
          />
        </div>
        <ErrorAlert error={error} />
        <div className="space-y-3">
          <Label>Internal ports</Label>
          <div className="flex flex-wrap items-center gap-2">
            {ports.map((port) => (
              <span
                key={port}
                className="inline-flex h-8 items-center gap-2 rounded-md border border-border-strong px-2.5 font-mono text-xs"
              >
                {port}
                <button
                  type="button"
                  className="text-muted-foreground hover:text-destructive"
                  aria-label={`Remove internal port ${port}`}
                  disabled={checks.some((check) => check.port === port)}
                  onClick={() => {
                    setPorts(ports.filter((value) => value !== port));
                    setSaved(false);
                  }}
                >
                  <Trash2Icon className="size-3.5" />
                </button>
              </span>
            ))}
            <div className="flex items-center gap-2">
              <Input
                aria-label="Internal port"
                type="number"
                min={1}
                max={65535}
                className="h-8 w-28 font-mono"
                value={newPort}
                onChange={(event) => setNewPort(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Enter") {
                    event.preventDefault();
                    addPort();
                  }
                }}
                placeholder="4310"
              />
              <Button type="button" size="sm" variant="outline" onClick={addPort}>
                <PlusIcon /> Add port
              </Button>
            </div>
          </div>
          {ports.length === 0 && (
            <p className="text-xs text-muted-foreground">No internal ports declared.</p>
          )}
        </div>

        <div className="space-y-3">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <Label>Health checks</Label>
              <p className="mt-1 text-xs text-muted-foreground">
                Path, port and timing settings for each HTTP readiness check.
              </p>
            </div>
            <Button
              type="button"
              size="sm"
              variant="outline"
              disabled={ports.length === 0 || checks.length >= 20}
              onClick={() => {
                setChecks([...checks, { ...newCheck(ports[0]), key: crypto.randomUUID() }]);
                setSaved(false);
              }}
            >
              <PlusIcon /> Add check
            </Button>
          </div>
          {checks.map((check, index) => (
            <div key={check.key} className="rounded-lg border border-border p-4">
              <div className="mb-4 flex items-center justify-between gap-3">
                <span className="text-sm font-medium">Check {index + 1}</span>
                <Button
                  type="button"
                  size="icon-sm"
                  variant="ghost"
                  aria-label={`Remove health check ${index + 1}`}
                  onClick={() => {
                    setChecks(checks.filter((_, i) => i !== index));
                    setSaved(false);
                  }}
                >
                  <Trash2Icon />
                </Button>
              </div>
              <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
                <Field id={`health-check-${index}-path`} label="Path">
                  <Input
                    id={`health-check-${index}-path`}
                    value={check.path}
                    placeholder="/healthz"
                    onChange={(event) => updateCheck(index, { path: event.target.value })}
                  />
                </Field>
                <Field id={`health-check-${index}-port`} label="Port">
                  <select
                    id={`health-check-${index}-port`}
                    className="h-10 rounded-md border border-border-strong bg-secondary px-3 font-mono text-sm"
                    value={check.port}
                    onChange={(event) => updateCheck(index, { port: Number(event.target.value) })}
                  >
                    {ports.map((port) => (
                      <option key={port} value={port}>
                        {port}
                      </option>
                    ))}
                  </select>
                </Field>
                <NumberField
                  label="Initial delay (seconds)"
                  id={`health-check-${index}-delay`}
                  value={check.initial_delay_seconds}
                  min={0}
                  max={3600}
                  onChange={(value) => updateCheck(index, { initial_delay_seconds: value })}
                />
                <NumberField
                  label="Period (seconds)"
                  id={`health-check-${index}-period`}
                  value={check.period_seconds}
                  min={1}
                  max={300}
                  onChange={(value) => updateCheck(index, { period_seconds: value })}
                />
                <NumberField
                  label="Timeout (seconds)"
                  id={`health-check-${index}-timeout`}
                  value={check.timeout_seconds}
                  min={1}
                  max={60}
                  onChange={(value) => updateCheck(index, { timeout_seconds: value })}
                />
                <NumberField
                  label="Failure threshold"
                  id={`health-check-${index}-failure`}
                  value={check.failure_threshold}
                  min={1}
                  max={20}
                  onChange={(value) => updateCheck(index, { failure_threshold: value })}
                />
                <NumberField
                  label="Success threshold"
                  id={`health-check-${index}-success`}
                  value={check.success_threshold}
                  min={1}
                  max={20}
                  onChange={(value) => updateCheck(index, { success_threshold: value })}
                />
              </div>
            </div>
          ))}
          {checks.length === 0 && (
            <p className="text-xs text-muted-foreground">No health checks configured.</p>
          )}
        </div>

        <div className="flex items-center gap-3">
          <ActionButton type="button" busy={busy} disabled={!dirty} onClick={save}>
            Save networking
          </ActionButton>
          {saved && !dirty && <span className="text-xs text-success">Saved</span>}
        </div>
      </CardContent>
    </Card>
  );
}

function Field({ id, label, children }: { id: string; label: string; children: React.ReactNode }) {
  return (
    <div className="grid min-w-0 gap-1.5 text-xs text-muted-foreground">
      <Label htmlFor={id}>{label}</Label>
      {children}
    </div>
  );
}

function NumberField({
  label,
  id,
  value,
  min,
  max,
  onChange,
}: {
  label: string;
  id: string;
  value: number;
  min: number;
  max: number;
  onChange: (value: number) => void;
}) {
  return (
    <Field id={id} label={label}>
      <Input
        id={id}
        type="number"
        min={min}
        max={max}
        value={value}
        onChange={(event) => onChange(Number(event.target.value))}
      />
    </Field>
  );
}
