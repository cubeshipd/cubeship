"use client";

import { useEffect, useState } from "react";
import { InstanceBackups } from "@/components/backups";
import { ErrorAlert } from "@/components/error-alert";
import { InstanceDomain } from "@/components/instance-domain";
import { Notice } from "@/components/notice";
import { PageLoading } from "@/components/page-loading";
import { SectionHeader } from "@/components/section-header";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Switch } from "@/components/ui/switch";
import { api, type Settings } from "@/lib/api";
import { message } from "@/lib/errors";

export default function SettingsPage() {
  const [current, setCurrent] = useState<Settings | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .get<Settings>("/settings")
      .then(setCurrent)
      .catch((e) => setError(message(e)));
  }, []);

  return (
    <>
      <SectionHeader
        page
        title="Settings"
        sub="Manage this instance's domain, updates and backups."
      />
      {!current ? (
        error ? (
          <ErrorAlert error={error} />
        ) : (
          <PageLoading kind="form" label="Loading instance settings" />
        )
      ) : (
        <>
          <ErrorAlert error={error} />

          {!current.tls_enabled && (
            <Notice tone="warning">
              No certificates yet. Set a domain to request them; a connected DNS provider can create
              the records automatically.
            </Notice>
          )}

          <div className="space-y-6">
            <InstanceDomain settings={current} onSaved={setCurrent} />

            <Card>
              <CardContent className="space-y-6 [&>.dashboard-section-header]:mb-0">
                <SectionHeader
                  title="Updates"
                  sub="Schedule stable updates and choose whether to receive beta versions."
                />
                <AutoUpdate settings={current} onSaved={setCurrent} />
                <div className="border-t border-border pt-6">
                  <BetaVersions settings={current} onSaved={setCurrent} />
                </div>
              </CardContent>
            </Card>
          </div>

          {/* Instance backups belong here: database and bucket backups
              have their own screens. */}
          <div className="mt-8">
            <InstanceBackups />
          </div>
        </>
      )}
    </>
  );
}

// BetaVersions saves as it is flipped: one switch with a Save
// button under it is a second click that confirms nothing.
function BetaVersions({
  settings,
  onSaved,
}: {
  settings: Settings;
  onSaved: (s: Settings) => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  return (
    <>
      <ErrorAlert error={error} />
      <div className="flex items-start gap-3">
        <Switch
          id="beta-versions"
          checked={!!settings.beta_versions}
          disabled={busy}
          onCheckedChange={async (v) => {
            setBusy(true);
            setError(null);
            try {
              onSaved(await api.put<Settings>("/settings", { beta_versions: v === true }));
            } catch (err) {
              setError(message(err));
            } finally {
              setBusy(false);
            }
          }}
          className="mt-0.5"
        />
        <label htmlFor="beta-versions" className="space-y-1 leading-relaxed">
          <span className="block text-sm font-medium">Beta versions</span>
          <span className="block text-xs text-muted-foreground">
            Receive betas and release candidates. Turning this off waits for the next stable
            release; it does not downgrade the instance.
          </span>
        </label>
      </div>
    </>
  );
}

// AutoUpdate is the time of day this instance replaces itself.
//
// **A time rather than an interval**, because what is being chosen is
// when the instance may be briefly unusable — and "every 24 hours from
// whenever you turned it on" is not something anybody can plan around.
function AutoUpdate({ settings, onSaved }: { settings: Settings; onSaved: (s: Settings) => void }) {
  const [on, setOn] = useState(!!settings.auto_update_at);
  const [at, setAt] = useState(settings.auto_update_at ?? "03:00");
  // The browser knows where the person is, and the server's clock does
  // not. Offered rather than assumed, because an instance and the
  // person running it are routinely in different places.
  const [zone, setZone] = useState(
    settings.auto_update_timezone ?? Intl.DateTimeFormat().resolvedOptions().timeZone ?? "UTC",
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const next = on ? { auto_update_at: at, auto_update_timezone: zone } : { auto_update_at: "" };
  const dirty =
    (on ? at : "") !== (settings.auto_update_at ?? "") ||
    (on && zone !== (settings.auto_update_timezone ?? ""));

  return (
    <form
      className="space-y-4"
      onSubmit={async (e) => {
        e.preventDefault();
        setBusy(true);
        setError(null);
        try {
          onSaved(await api.put<Settings>("/settings", next));
        } catch (err) {
          setError(message(err));
        } finally {
          setBusy(false);
        }
      }}
    >
      <ErrorAlert error={error} />

      <div className="flex items-start gap-3">
        <Switch
          id="auto-update"
          checked={on}
          onCheckedChange={(v) => setOn(v === true)}
          className="mt-0.5"
        />
        <label htmlFor="auto-update" className="space-y-1 leading-relaxed">
          <span className="block text-sm font-medium">Automatic updates</span>
          <span className="block text-xs text-muted-foreground">
            Install stable releases at the time you choose.
          </span>
        </label>
      </div>

      {on && (
        <div className="grid gap-4 sm:grid-cols-2">
          <TextField
            label="At"
            type="time"
            value={at}
            onChange={(e) => setAt(e.target.value)}
            hint="On a 24-hour clock. Pick an hour when nobody is deploying."
          />
          <TextField
            label="Timezone"
            value={zone}
            onChange={(e) => setZone(e.target.value)}
            hint="An IANA name. Empty is UTC, which is the server's clock rather than your night."
          />
        </div>
      )}

      <Button type="submit" disabled={busy || !dirty}>
        {busy ? "Saving..." : "Save"}
      </Button>
    </form>
  );
}
