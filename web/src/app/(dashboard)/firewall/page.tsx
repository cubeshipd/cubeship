"use client";

import { PlusIcon, ShieldAlertIcon, Trash2Icon } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { type Column, DataTable } from "@/components/data-table";
import { ErrorAlert } from "@/components/error-alert";
import { LoadingNote } from "@/components/loading";
import { Notice } from "@/components/notice";
import { PageHeader, SectionHeader } from "@/components/page-header";
import { RowAction, RowActions } from "@/components/row-actions";
import { SearchableSelect } from "@/components/searchable-select";
import { StatusBadge } from "@/components/status-badge";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { api, type Firewall, type FirewallRule } from "@/lib/api";
import { message } from "@/lib/errors";

// The host's firewall.
//
// The screen is built around the one thing that is not obvious and that
// gets people hurt: **UFW does not govern a published container port.**
// Every port this instance opens is one of those, so a page that just
// listed `ufw status` would be describing a firewall that is not in
// front of anything you deployed. So the rules are shown in two groups,
// named for what they are actually about, and the group that covers
// containers says plainly when it is inert.
export default function FirewallPage() {
  const [data, setData] = useState<Firewall | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [adding, setAdding] = useState<"host" | "apps" | null>(null);
  const [deleting, setDeleting] = useState<FirewallRule | null>(null);
  const [adopting, setAdopting] = useState(false);

  const reload = useCallback(() => {
    api
      .get<Firewall>("/firewall")
      .then((next) => {
        setData(next);
        setError(null);
      })
      .catch((e) => setError(message(e)));
  }, []);
  useEffect(reload, [reload]);

  // Every write answers with the firewall as it now stands, so nothing
  // here follows a change with a read.
  const act = useCallback(async (run: () => Promise<Firewall>) => {
    setBusy(true);
    setError(null);
    try {
      setData(await run());
    } catch (e) {
      setError(message(e));
    }
    setBusy(false);
  }, []);

  // The v6 twins are folded away. UFW writes one decision as two lines,
  // and a list that shows both reads as a firewall with twice as many
  // rules as it has.
  const rules = (data?.rules ?? []).filter((r) => !r.v6);
  const hostRules = rules.filter((r) => r.scope === "host");
  const appRules = rules.filter((r) => r.scope === "apps");
  const exposed = (data?.published ?? []).filter((p) => !p.allowed);

  const columns: Column<FirewallRule>[] = [
    {
      id: "text",
      header: "Rule",
      width: 62,
      wrap: true,
      cell: (r) => <span className="font-mono text-xs">{r.text}</span>,
    },
    {
      id: "action",
      header: "",
      width: 20,
      // StatusBadge is the one place a state becomes a colour, and
      // these are states: an allow is green because it is working as
      // intended, a deny red because it is stopping something.
      cell: (r) => (r.action ? <StatusBadge value={r.action} /> : null),
    },
    {
      id: "delete",
      header: "",
      width: 18,
      align: "right",
      cell: (r) => (
        <RowActions>
          <RowAction
            icon={Trash2Icon}
            label={`Delete rule ${r.index}`}
            danger
            onClick={() => setDeleting(r)}
          />
        </RowActions>
      ),
    },
  ];

  return (
    <>
      <PageHeader
        title="Firewall"
        actions={
          data?.installed ? (
            <ActionButton
              busy={busy}
              variant={data.enabled ? "outline" : "default"}
              onClick={() =>
                act(() => api.post<Firewall>(`/firewall/${data.enabled ? "disable" : "enable"}`))
              }
            >
              {data.enabled ? "Turn off" : "Turn on"}
            </ActionButton>
          ) : undefined
        }
      />

      <ErrorAlert error={error} />

      {!data && !error && <LoadingNote>Reading the host&apos;s firewall</LoadingNote>}

      {data && !data.available && (
        <Notice>
          This daemon is running as a host process rather than as a container, so it has no way to
          read the machine&apos;s firewall. An installed instance does.
        </Notice>
      )}

      {data?.available && !data.installed && (
        <Notice>
          This host has no <code>ufw</code> installed. Installing it is yours to do —{" "}
          <code>apt install ufw</code> on a Debian or Ubuntu — and this page picks it up once it is
          there. Cubeship does not install software on your machine uninvited.
        </Notice>
      )}

      {data?.installed && (
        <>
          {!data.enabled && (
            <Notice tone="warning">
              The firewall is off, so the rules below are waiting rather than applying. Everything
              this machine listens on is offered to the network — whether anything reaches it
              depends on what is in front, which this page cannot see —{" "}
              {data.ssh_allowed || (data.ssh_ports ?? []).length > 0
                ? "turning it on puts them in force."
                : "and this daemon could not find out which port sshd is on, so turning it on is refused: add the rule for the port you connect on first, or you would not get back in."}
            </Notice>
          )}
          {data.enabled && !data.ssh_allowed && (
            <Notice tone="warning">
              Nothing here admits SSH
              {(data.ssh_ports ?? []).length > 0 && ` on ${data.ssh_ports?.join(" or ")}`}. If this
              session ends you may not get another.
            </Notice>
          )}

          <SectionHeader
            title="This machine"
            sub="Traffic to the host itself — SSH, and anything else not running in a container."
            actions={
              <Button variant="outline" size="sm" onClick={() => setAdding("host")}>
                <PlusIcon />
                Add rule
              </Button>
            }
          />
          {/* Rules exist while the firewall is off — ufw keeps them and
              applies them when it starts — so they are shown either
              way, and the banner above says they are not in force. */}
          <DataTable
            columns={columns}
            rows={hostRules}
            rowKey={(r) => `host-${r.index}`}
            className="mb-4"
            empty={
              data.enabled
                ? "No rules. With the default policy denying incoming, that means nothing reaches this host."
                : "No rules yet. Add the one for SSH before turning the firewall on — with none, turning it on would end this session."
            }
          />

          <SectionHeader
            title="Published ports"
            sub="Traffic forwarded to a container — Traefik, an exposed database, anything you published. Docker routes this around ufw, so it is governed separately or not at all. A firewall at your provider sits in front of both and is not visible here."
            actions={
              data.docker_adopted ? (
                <Button variant="outline" size="sm" onClick={() => setAdding("apps")}>
                  <PlusIcon />
                  Add rule
                </Button>
              ) : undefined
            }
          />

          {!data.docker_adopted ? (
            <Card className="mb-4">
              <CardContent className="space-y-4">
                <div className="flex items-start gap-3">
                  <ShieldAlertIcon className="mt-0.5 size-4 shrink-0 text-warning" />
                  <div className="space-y-2 text-sm text-muted-foreground">
                    <p className="text-foreground">
                      Published container ports are not behind this firewall.
                    </p>
                    <p>
                      Docker opens a port by writing its own rules ahead of ufw&apos;s, and that
                      traffic is forwarded rather than delivered to the host — so it never passes
                      the chain <code>ufw deny</code> governs. On this instance that is Traefik on
                      80 and 443, every database you exposed, and the daemon itself.
                    </p>
                    <p>
                      Turning this on adds a stanza to the host&apos;s{" "}
                      <code>/etc/ufw/after.rules</code> that sends Docker&apos;s own chain through
                      ufw first. After it, a rule here governs a published port —{" "}
                      <strong>and a published port with no rule is closed.</strong>
                    </p>
                  </div>
                </div>

                {data.published.length > 0 && (
                  <div className="border border-border">
                    <div className="border-border border-b px-3 py-2 text-[11px] tracking-[0.12em] text-muted-foreground uppercase">
                      Open right now
                    </div>
                    <ul className="divide-y divide-border">
                      {data.published.map((p) => (
                        <li
                          key={`${p.port}/${p.protocol}`}
                          className="flex items-center justify-between px-3 py-2 font-mono text-xs"
                        >
                          <span>
                            {p.port}/{p.protocol}
                          </span>
                          <span className="text-muted-foreground">{p.container}</span>
                        </li>
                      ))}
                    </ul>
                  </div>
                )}

                <div className="flex items-center gap-3">
                  <Button onClick={() => setAdopting(true)}>Put them behind the firewall</Button>
                  <span className="text-xs text-muted-foreground">
                    80 and 443 stay open whatever you choose — they are Traefik.
                  </span>
                </div>
              </CardContent>
            </Card>
          ) : (
            <>
              {exposed.length > 0 && (
                <Notice tone="warning">
                  {exposed.map((p) => `${p.port}/${p.protocol} (${p.container})`).join(", ")}{" "}
                  {exposed.length === 1 ? "is" : "are"} published and admitted by no rule, so
                  nothing outside this host can reach {exposed.length === 1 ? "it" : "them"} any
                  more.
                </Notice>
              )}
              <DataTable
                columns={columns}
                rows={appRules}
                rowKey={(r) => `apps-${r.index}`}
                className="mb-4"
                empty="No rules, so every published port is closed to the outside."
              />
              <div className="mb-6 flex items-center gap-3">
                <Button
                  variant="ghost"
                  size="xs"
                  className="text-muted-foreground hover:text-destructive"
                  onClick={() => act(() => api.del<Firewall>("/firewall/docker"))}
                >
                  Stop governing published ports
                </Button>
                <span className="text-xs text-subtle-foreground">
                  They go back to being reachable regardless of any rule.
                </span>
              </div>
            </>
          )}
        </>
      )}

      <RuleDialog
        scope={adding}
        published={data?.published ?? []}
        onOpenChange={(open) => !open && setAdding(null)}
        onSaved={setData}
      />

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title="Delete this rule?"
        confirmLabel="Delete"
        description={
          <>
            <code className="text-foreground">{deleting?.text}</code> stops applying immediately.
            Whatever it admitted falls back to the default policy, which is to deny.
          </>
        }
        onConfirm={async () => {
          if (!deleting) return;
          // The rule's own text goes with the index: ufw deletes by
          // position, and a position shifts as soon as anything above it
          // goes. Without this a stale page deletes a different rule.
          const next = await api.del<Firewall>(
            `/firewall/rules/${deleting.index}?expect=${encodeURIComponent(deleting.text)}`,
          );
          setDeleting(null);
          setData(next);
        }}
      />

      <ConfirmDialog
        open={adopting}
        onOpenChange={setAdopting}
        title="Put published ports behind the firewall?"
        confirmLabel="Do it"
        description={
          <>
            Everything published and not allowed stops being reachable from outside this host. Ports
            80 and 443 are allowed for you, and so is everything currently published — you can
            remove those rules afterwards, one at a time, and see what breaks before the next one.
          </>
        }
        onConfirm={async () => {
          const next = await api.post<Firewall>("/firewall/docker", {
            allow_ports: (data?.published ?? []).map((p) => p.port),
          });
          setAdopting(false);
          setData(next);
        }}
      />
    </>
  );
}

// One dialog for both kinds of rule. They ask for the same things — the
// scope is what the caller pressed, not a field, because "which of these
// two firewalls" is a question nobody should be asked twice.
function RuleDialog({
  scope,
  published,
  onOpenChange,
  onSaved,
}: {
  scope: "host" | "apps" | null;
  published: { port: number; container: string }[];
  onOpenChange: (v: boolean) => void;
  onSaved: (f: Firewall) => void;
}) {
  const [action, setAction] = useState("allow");
  const [protocol, setProtocol] = useState("tcp");
  const [port, setPort] = useState("");
  const [from, setFrom] = useState("");
  const [comment, setComment] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!scope) return;
    setError(null);
    setPort("");
    setFrom("");
    setComment("");
    setAction("allow");
  }, [scope]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      onSaved(
        await api.post<Firewall>("/firewall/rules", {
          scope,
          action,
          protocol,
          port,
          from,
          comment,
        }),
      );
      onOpenChange(false);
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  const apps = scope === "apps";

  return (
    <Dialog open={scope !== null} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>
              {apps ? "Rule for a published port" : "Rule for this machine"}
            </DialogTitle>
            <DialogDescription>
              {apps
                ? "Traffic forwarded to a container. This is what governs a port you published — a database, an app Traefik does not front."
                : "Traffic to the host itself. This does not touch anything running in a container."}
            </DialogDescription>
          </DialogHeader>

          <div className="-mx-2 max-h-[60vh] space-y-4 overflow-y-auto px-2 py-5">
            <ErrorAlert error={error} />

            <div className="grid gap-4 sm:grid-cols-2">
              <SearchableSelect
                label="Action"
                choices={[
                  { value: "allow", label: "Allow" },
                  { value: "deny", label: "Deny", hint: "drops silently" },
                  { value: "reject", label: "Reject", hint: "answers" },
                ]}
                value={action}
                onChange={setAction}
              />
              <SearchableSelect
                label="Protocol"
                choices={[
                  { value: "tcp", label: "TCP" },
                  { value: "udp", label: "UDP" },
                ]}
                value={protocol}
                onChange={setProtocol}
              />
            </div>

            <TextField
              label="Port"
              value={port}
              spellCheck={false}
              placeholder={apps ? "15432" : "22"}
              onChange={(e) => setPort(e.target.value)}
              hint='One port, or a range written as "15000:15999".'
            />

            {apps && published.length > 0 && (
              <div className="flex flex-wrap gap-1">
                {published.map((p) => (
                  <Button
                    key={p.port}
                    type="button"
                    variant="outline"
                    size="xs"
                    className="font-mono"
                    onClick={() => setPort(String(p.port))}
                  >
                    {p.port}
                  </Button>
                ))}
              </div>
            )}

            <TextField
              label="From"
              value={from}
              spellCheck={false}
              placeholder="anywhere"
              onChange={(e) => setFrom(e.target.value)}
              hint="An address or a range, like 203.0.113.4 or 10.0.0.0/8. Leave it empty for anywhere — which is what most rules mean."
            />

            <TextField
              label="Note"
              value={comment}
              spellCheck={false}
              onChange={(e) => setComment(e.target.value)}
              hint="Carried into ufw's own comment, so this rule explains itself to whoever runs `ufw status` over ssh."
            />
          </div>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <ActionButton type="submit" busy={busy} disabled={!port}>
              Add rule
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
