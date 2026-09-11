"use client";

import { useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";
import { ErrorAlert } from "@/components/error-alert";
import { RailTabs } from "@/components/header-rail";
import { InstanceDomain } from "@/components/instance-domain";
import { Notice } from "@/components/notice";
import { SectionHeader } from "@/components/section-header";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { api, type Settings } from "@/lib/api";
import { message } from "@/lib/errors";

export default function Instance() {
  return (
    <>
      <Body />
    </>
  );
}

const TABS = ["domain", "updates"] as const;
type Tab = (typeof TABS)[number];

function Body() {
  // Linkable, like every other tab strip here, and read through
  // `useSearchParams` rather than `window` because this page renders on
  // the server first.
  const asked = useSearchParams().get("tab");
  const [tab, setTab] = useState<Tab>(() =>
    TABS.includes(asked as Tab) ? (asked as Tab) : "domain",
  );
  const [current, setCurrent] = useState<Settings | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .get<Settings>("/settings")
      .then(setCurrent)
      .catch((e) => setError(message(e)));
  }, []);

  if (!current) return <ErrorAlert error={error} />;

  return (
    <>
      <ErrorAlert error={error} />

      {!current.tls_enabled && (
        <Notice tone="warning">
          No certificates yet. Pick where this instance lives below — with a DNS provider connected,
          that is the whole of it: the records are written and the certificates follow.
        </Notice>
      )}

      {/* Where the instance lives and when it replaces itself are two
          unrelated decisions that happened to share a column. The
          warning above stays outside them: it is about the instance
          rather than about either tab, and on the Updates tab it would
          be missing exactly when somebody is about to schedule a
          replacement of something with no certificate. */}
      <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)} className="subrail-page">
        <RailTabs>
          <TabsList variant="line">
            <TabsTrigger value="domain">Domain</TabsTrigger>
            <TabsTrigger value="updates">Updates</TabsTrigger>
          </TabsList>
        </RailTabs>

        <TabsContent value="domain">
          <SectionHeader
            title="Domain"
            sub="The instance's own name. The dashboard and the API are served at it, the registry at registry.<domain>, and anything Cubeship grows later underneath — which is why a subdomain you hand over whole beats your apex."
          />
          <InstanceDomain settings={current} onSaved={setCurrent} />
        </TabsContent>

        <TabsContent value="updates">
          <SectionHeader
            title="Automatic updates"
            sub="Update this instance every day at a time you choose. It replaces the daemon, the dashboard and every other machine in this cluster; your apps and databases keep running, and nothing can be changed here for the minute or so it takes."
          />
          <AutoUpdate settings={current} onSaved={setCurrent} />
        </TabsContent>
      </Tabs>
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
    <Card>
      <CardContent>
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
            <label htmlFor="auto-update" className="text-xs leading-relaxed text-muted-foreground">
              Update this instance automatically. Only stable releases — an instance left to update
              itself should not wander onto a release candidate at three in the morning.
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
      </CardContent>
    </Card>
  );
}
