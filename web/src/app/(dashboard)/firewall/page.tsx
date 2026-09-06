"use client";

import { PencilIcon, PlusIcon, ShieldAlertIcon, Trash2Icon, XIcon } from "lucide-react";
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
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { api, type Firewall, type FirewallPublishedPort, type FirewallRule } from "@/lib/api";
import { message } from "@/lib/errors";

// Why the SSH rule's buttons are dead. Said on both of them, because
// somebody who tried one will try the other.
const keepsYouIn =
  "This is what admits SSH. Changing or removing it would end this session — do it on the machine if you mean to.";

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
  const [editing, setEditing] = useState<FirewallRule | null>(null);
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
      // The rule admitting SSH is not offered for deletion: it is what
      // keeps the session this is being read in, and the daemon refuses
      // it anyway. Disabled with the reason rather than hidden — a
      // missing button explains nothing.
      cell: (r) => (
        <RowActions>
          <RowAction
            icon={PencilIcon}
            label={`Edit rule ${r.index}`}
            disabled={r.protected}
            title={r.protected ? keepsYouIn : undefined}
            onClick={() => setEditing(r)}
          />
          <RowAction
            icon={Trash2Icon}
            label={`Delete rule ${r.index}`}
            danger={!r.protected}
            disabled={r.protected}
            title={r.protected ? keepsYouIn : undefined}
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
            sub="Traffic to the host itself, not to anything in a container."
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
            sub="Traffic forwarded to a container. Docker routes it around ufw, so it is governed separately — and a firewall at your provider sits in front of both, which this page cannot see."
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
                      Docker writes its own rules ahead of ufw&apos;s, so <code>ufw deny</code>{" "}
                      never sees these. Turning this on routes Docker&apos;s chain through ufw first
                      — after it, <strong>a published port with no rule is closed.</strong>
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
                  <span className="text-xs text-muted-foreground">You choose which stay open.</span>
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
        scope={adding ?? editing?.scope ?? null}
        rule={editing}
        published={data?.published ?? []}
        yourIP={data?.your_ip}
        onOpenChange={(open) => {
          if (open) return;
          setAdding(null);
          setEditing(null);
        }}
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

      <AdoptDialog
        open={adopting}
        published={data?.published ?? []}
        onOpenChange={setAdopting}
        onSaved={setData}
      />
    </>
  );
}

// One dialog for both kinds of rule. They ask for the same things — the
// scope is what the caller pressed, not a field, because "which of these
// two firewalls" is a question nobody should be asked twice.
// What turning on Docker port control asks: which of the ports open
// right now should stay open.
//
// It has to ask. The stanza closes every published port no rule admits,
// so pressing this without choosing is the difference between a
// firewall and an outage — and the screen said "80 and 443 stay open
// whatever you choose", which was only true because it was quietly
// sending all of them.
function AdoptDialog({
  open,
  published,
  onOpenChange,
  onSaved,
}: {
  open: boolean;
  published: FirewallPublishedPort[];
  onOpenChange: (v: boolean) => void;
  onSaved: (f: Firewall) => void;
}) {
  // Everything starts ticked. The safe default for a firewall is the
  // state the machine is already in — somebody who wants a port closed
  // can say so here, and somebody who does not know yet does not lose a
  // service by pressing a button they were told to press.
  const [keep, setKeep] = useState<number[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setError(null);
    setKeep(published.map((p) => p.port));
  }, [open, published]);

  // 80 and 443 are Traefik, which is every app and this page. The
  // daemon allows them whatever arrives, so offering to untick them
  // would be offering something that does not happen.
  const fixed = (port: number) => port === 80 || port === 443;

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      onSaved(await api.post<Firewall>("/firewall/docker", { allow_ports: keep }));
      onOpenChange(false);
    } catch (err) {
      setError(message(err));
    }
    setBusy(false);
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>Put published ports behind the firewall</DialogTitle>
            <DialogDescription>
              Everything not ticked stops being reachable from outside this host.
            </DialogDescription>
          </DialogHeader>

          <div className="-mx-2 max-h-[60vh] space-y-2 overflow-y-auto px-2 py-5">
            <ErrorAlert error={error} />
            {published.map((p) => (
              <label
                key={`${p.port}/${p.protocol}`}
                htmlFor={`keep-${p.port}-${p.protocol}`}
                className="flex items-center gap-3 border border-border px-3 py-2 font-mono text-xs"
              >
                <Checkbox
                  id={`keep-${p.port}-${p.protocol}`}
                  checked={fixed(p.port) || keep.includes(p.port)}
                  disabled={fixed(p.port)}
                  onCheckedChange={(on) =>
                    setKeep((ports) =>
                      on ? [...ports, p.port] : ports.filter((at) => at !== p.port),
                    )
                  }
                />
                <span className="flex-1">
                  {p.port}/{p.protocol}
                </span>
                <span className="text-muted-foreground">{p.container}</span>
              </label>
            ))}
            <p className="text-xs text-muted-foreground">
              80 and 443 stay open whatever you choose — they are Traefik, which is every app and
              this page.
            </p>
          </div>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <ActionButton type="submit" busy={busy}>
              Do it
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// A source is one of three answers, and only the third is typed.
//
// It is a select because "leave it empty for anywhere" is a rule you
// have to be told, and because the address somebody almost always wants
// — their own — is one they would otherwise go and look up, and get
// wrong by pasting a private one the firewall never sees.
type Source = { kind: "any" | "mine" | "custom"; value: string };

function RuleDialog({
  scope,
  rule,
  published,
  yourIP,
  onOpenChange,
  onSaved,
}: {
  scope: "host" | "apps" | null;
  // The rule being edited, or nothing when one is being added. One
  // dialog for both because they ask for exactly the same things —
  // editing *is* a delete and an add, so it could hardly ask for less.
  rule?: FirewallRule | null;
  published: { port: number; container: string }[];
  yourIP?: string;
  onOpenChange: (v: boolean) => void;
  onSaved: (f: Firewall) => void;
}) {
  const [action, setAction] = useState("allow");
  const [protocol, setProtocol] = useState("tcp");
  const [port, setPort] = useState("");
  const [sources, setSources] = useState<Source[]>([{ kind: "any", value: "" }]);
  const [comment, setComment] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!scope) return;
    setError(null);
    setPort(rule?.ports ?? "");
    setComment(rule?.comment ?? "");
    setAction(rule?.action ?? "allow");
    setProtocol(rule?.protocol ?? "tcp");
    // A rule holds one source — ufw takes one each — so an edit starts
    // on that one and can grow from there.
    setSources([
      rule?.from
        ? { kind: rule.from === yourIP ? "mine" : "custom", value: rule.from }
        : { kind: "any", value: "" },
    ]);
  }, [scope, rule, yourIP]);

  function setSource(index: number, next: Partial<Source>) {
    setSources((rows) => rows.map((row, i) => (i === index ? { ...row, ...next } : row)));
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const body = {
        scope,
        action,
        protocol,
        port,
        // One rule per source — ufw takes one each — and an empty
        // string is anywhere, which the daemon lets absorb the rest.
        sources: sources.map((s) =>
          s.kind === "any" ? "" : s.kind === "mine" ? (yourIP ?? "") : s.value.trim(),
        ),
        comment,
      };
      onSaved(
        rule
          ? await api.put<Firewall>(
              `/firewall/rules/${rule.index}?expect=${encodeURIComponent(rule.text)}`,
              body,
            )
          : await api.post<Firewall>("/firewall/rules", body),
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
              {rule ? "Edit rule" : apps ? "Rule for a published port" : "Rule for this machine"}
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
              hint='One port, or a range like "15000:15999".'
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

            <div className="space-y-2">
              <Label>From</Label>
              {sources.map((source, i) => (
                // The index is the identity here: a row is a position in
                // a short list somebody is editing, and nothing else
                // about it is stable while they type.
                // biome-ignore lint/suspicious/noArrayIndexKey: see above
                <div key={i} className="flex items-center gap-2">
                  <SearchableSelect
                    label=""
                    fieldClassName="w-44 shrink-0"
                    choices={[
                      { value: "any", label: "Anywhere" },
                      {
                        value: "mine",
                        label: "This computer",
                        hint: yourIP,
                      },
                      { value: "custom", label: "An address…" },
                    ]}
                    value={source.kind}
                    onChange={(kind) => setSource(i, { kind: kind as Source["kind"] })}
                  />
                  {source.kind === "custom" && (
                    <TextField
                      label=""
                      fieldClassName="flex-1"
                      value={source.value}
                      spellCheck={false}
                      placeholder="203.0.113.4 or 10.0.0.0/8"
                      onChange={(e) => setSource(i, { value: e.target.value })}
                    />
                  )}
                  {source.kind === "mine" && (
                    <span className="font-mono text-xs text-muted-foreground">
                      {yourIP ?? "unknown"}
                    </span>
                  )}
                  {sources.length > 1 && (
                    <RowAction
                      icon={XIcon}
                      label={`Remove source ${i + 1}`}
                      onClick={() => setSources((rows) => rows.filter((_, at) => at !== i))}
                    />
                  )}
                </div>
              ))}
              <Button
                type="button"
                variant="ghost"
                size="xs"
                onClick={() => setSources((rows) => [...rows, { kind: "custom", value: "" }])}
              >
                <PlusIcon />
                Add a source
              </Button>
            </div>

            <TextField
              label="Note"
              value={comment}
              spellCheck={false}
              onChange={(e) => setComment(e.target.value)}
              hint="Carried into ufw's own comment, so the rule explains itself over ssh."
            />
          </div>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <ActionButton type="submit" busy={busy} disabled={!port}>
              {rule ? "Save" : "Add rule"}
            </ActionButton>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
