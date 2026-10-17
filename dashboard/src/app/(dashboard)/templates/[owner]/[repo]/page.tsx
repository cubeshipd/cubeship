"use client";

import { useQuery } from "@tanstack/react-query";
import {
  ArrowLeftIcon,
  BadgeCheckIcon,
  BoxIcon,
  DatabaseIcon,
  ExternalLinkIcon,
  HardDriveIcon,
  PlusIcon,
} from "lucide-react";
import { useRouter } from "next/navigation";
import { type ReactNode, use, useEffect, useMemo, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { DomainInput, type PendingRecord, writeRecord } from "@/components/domain-input";
import { ErrorAlert } from "@/components/error-alert";
import { GlideSelect } from "@/components/glide-select";
import { LoadingList } from "@/components/loading";
import Link from "@/components/navigation-link";
import { SectionHeader } from "@/components/section-header";
import { TemplateMark } from "@/components/template-card";
import { TextField } from "@/components/text-field";
import { Card, CardContent } from "@/components/ui/card";
import {
  api,
  type Environment,
  type ObjectStore,
  type Project,
  type Settings,
  type TemplateDetail,
  type TemplateInput,
  type TemplateInstallStarted,
  type TemplateManifest,
} from "@/lib/api";
import { message } from "@/lib/errors";
import { handOverSecrets } from "@/lib/install-secrets";

// NEW is the choice of a project or environment that does not exist yet.
const NEW = "__new__";

// One template: what it creates, and the form that installs it.
//
// **Everything is decided here, before anything exists.** The daemon
// checks the names and the answers and refuses without creating a thing,
// so a mistake is a message under the button rather than half an install.
export default function TemplatePage({ params }: PageProps<"/templates/[owner]/[repo]">) {
  const { owner, repo } = use(params);
  const template = useQuery({
    queryKey: ["templates", owner, repo],
    queryFn: () => api.get<TemplateDetail>(`/templates/${owner}/${repo}`),
  });
  if (template.error) return <ErrorAlert error={message(template.error)} />;
  if (!template.data) return <LoadingList rows={5} />;
  const t = template.data;
  const manifest = t.manifest;

  return (
    <>
      <Link
        href="/templates"
        className="mb-5 inline-flex items-center gap-2 text-sm text-muted-foreground transition-colors hover:text-foreground"
      >
        <ArrowLeftIcon className="size-4" />
        Back to templates
      </Link>
      <div className="mb-8 flex flex-wrap items-start gap-4">
        <TemplateMark src={t.icon_url} className="size-14 rounded-xl" />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <h1 className="text-2xl font-semibold tracking-tight">{t.title}</h1>
            {t.verified && (
              <BadgeCheckIcon className="size-5 text-primary" aria-label="Verified publisher" />
            )}
          </div>
          <p className="mt-1 max-w-3xl text-sm leading-relaxed text-muted-foreground">
            {t.description}
          </p>
          <div className="mt-3 flex flex-wrap items-center gap-2">
            {t.tags.slice(0, 5).map((tag) => (
              <span
                key={tag}
                className="rounded-full bg-secondary px-2.5 py-1 text-xs text-muted-foreground"
              >
                {tag}
              </span>
            ))}
            <span className="font-mono text-xs text-subtle-foreground">{t.release.tag}</span>
            <a
              href={t.source_url}
              target="_blank"
              rel="noreferrer"
              className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
            >
              Source <ExternalLinkIcon className="size-3" />
            </a>
          </div>
        </div>
      </div>
      {manifest ? (
        <div className="space-y-6">
          <Creates manifest={manifest} />
          <InstallForm key={t.name} template={t} manifest={manifest} />
        </div>
      ) : (
        <ErrorAlert error="The catalog has no manifest for this template." />
      )}
    </>
  );
}

function Creates({ manifest }: { manifest: TemplateManifest }) {
  const rows: { key: string; icon: ReactNode; name: string; detail: string }[] = [
    ...manifest.apps.map((a) => ({
      key: `app-${a.key}`,
      icon: <BoxIcon />,
      name: a.name,
      detail: "Application",
    })),
    ...manifest.databases.map((d) => ({
      key: `db-${d.key}`,
      icon: <DatabaseIcon />,
      name: d.name,
      detail: d.version ? `${d.engine} ${d.version}` : d.engine,
    })),
    ...manifest.stores.map((s) => ({
      key: `store-${s.key}`,
      icon: <HardDriveIcon />,
      name: s.name,
      detail: "Object storage",
    })),
  ];
  return (
    <Card>
      <CardContent className="[&>.dashboard-section-header]:mb-4">
        <SectionHeader title="Included" sub="Resources this template will create." />
        <div className="divide-y divide-border">
          {rows.map((r) => (
            <div key={r.key} className="flex min-w-0 items-center gap-3 py-3 last:pb-0">
              <span className="text-primary [&_svg]:size-4">{r.icon}</span>
              <span className="min-w-0 flex-1 truncate font-mono text-sm">{r.name}</span>
              <span className="shrink-0 text-xs text-muted-foreground">{r.detail}</span>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  );
}

function InstallForm({
  template,
  manifest,
}: {
  template: TemplateDetail;
  manifest: TemplateManifest;
}) {
  const router = useRouter();
  // Where the apps go: a project that exists, picked, or a new one, named —
  // and inside an existing one, an environment it has or a new one.
  const [projectPick, setProjectPick] = useState(NEW);
  const [newProject, setNewProject] = useState(manifest.project);
  const [envPick, setEnvPick] = useState(NEW);
  const [newEnvironment, setNewEnvironment] = useState(manifest.environment);
  const projects = useQuery({
    queryKey: ["projects"],
    queryFn: () => api.get<Project[]>("/projects"),
  });
  const environments = useQuery({
    queryKey: ["projects", projectPick, "environments"],
    queryFn: () => api.get<Environment[]>(`/projects/${projectPick}/environments`),
    enabled: projectPick !== NEW,
  });
  // Picking a project lands on the template's environment when it has one.
  useEffect(() => {
    if (!environments.data) return;
    setEnvPick(
      environments.data.some((e) => e.slug === manifest.environment) ? manifest.environment : NEW,
    );
  }, [environments.data, manifest.environment]);
  const project = projectPick === NEW ? newProject.trim() : projectPick;
  const environment = projectPick === NEW || envPick === NEW ? newEnvironment.trim() : envPick;
  const [names, setNames] = useState<Record<string, string>>({});
  const [inputs, setInputs] = useState<Record<string, string>>({});
  // The DNS records a domain input chose to have written, by input key.
  const [records, setRecords] = useState<Record<string, PendingRecord>>({});
  const [error, setError] = useState<string | null>(null);

  const settings = useQuery({
    queryKey: ["settings"],
    queryFn: () => api.get<Settings>("/settings"),
  });
  const needsStores = manifest.inputs.some((i) => i.type === "store");
  const stores = useQuery({
    queryKey: ["objectstores"],
    queryFn: () => api.get<ObjectStore[]>("/objectstores"),
    enabled: needsStores,
  });

  // A domain input is offered a name under the instance's own domain when
  // every name there already resolves here — the app's name, its
  // environment and its project, the way an app's own suggestion is built.
  const suggested = useMemo(() => {
    const out: Record<string, string> = {};
    const domain = settings.data?.domain;
    if (!domain || !settings.data?.wildcard_domain) return out;
    for (const a of manifest.apps) {
      for (const d of a.domains) {
        const key = /^\$\{input\.([A-Za-z][\w-]*)\}$/.exec(d.host)?.[1];
        const name = names[`apps.${a.key}`] || a.name;
        if (key) out[key] = `${name}.${environment}.${project}.${domain}`;
      }
    }
    return out;
  }, [settings.data, manifest.apps, names, environment, project]);

  useEffect(() => {
    setInputs((current) => {
      const next = { ...current };
      for (const [key, value] of Object.entries(suggested)) {
        if (!(key in current)) next[key] = value;
      }
      return next;
    });
  }, [suggested]);

  const nameFields = [
    ...manifest.apps.map((a) => ({ id: `apps.${a.key}`, label: `App ${a.key}`, fallback: a.name })),
    ...manifest.databases.map((d) => ({
      id: `databases.${d.key}`,
      label: `Database ${d.key}`,
      fallback: d.name,
    })),
    ...manifest.stores.map((s) => ({
      id: `stores.${s.key}`,
      label: `Object store ${s.key}`,
      fallback: s.name,
    })),
  ];

  async function install() {
    setError(null);
    const pick = (kind: string) =>
      Object.fromEntries(
        Object.entries(names)
          .filter(([id, value]) => id.startsWith(`${kind}.`) && value.trim() !== "")
          .map(([id, value]) => [id.slice(kind.length + 1), value.trim()]),
      );
    try {
      // The records first, the way adding an app's domain does it: a name
      // the install then routes that resolves nowhere is an app answering
      // at nothing.
      const ip = settings.data?.public_ip ?? "";
      for (const [key, record] of Object.entries(records)) {
        if (inputs[key] !== record.host) continue;
        if (!ip) {
          throw new Error(
            "This instance does not know its own public address, so no DNS record can point at it. Set it under Settings.",
          );
        }
        await writeRecord(record, ip);
      }
      const started = await api.post<TemplateInstallStarted>(
        `/templates/${template.owner}/${template.name}/installs`,
        {
          project,
          environment,
          names: { apps: pick("apps"), databases: pick("databases"), stores: pick("stores") },
          inputs,
        },
      );
      handOverSecrets(started.install.id, started.secrets);
      router.push(`/templates/installs/${started.install.id}`);
    } catch (e) {
      setError(message(e));
    }
  }

  // Domains take a whole row, so they go first and the rest pair up after
  // them; one left over takes the row to itself rather than leave a hole.
  const domainInputs = manifest.inputs.filter((i) => i.type === "domain");
  const otherInputs = manifest.inputs.filter((i) => i.type !== "domain");
  const ordered = [...domainInputs, ...otherInputs];
  const lonely = otherInputs.length % 2 === 1 ? otherInputs[otherInputs.length - 1].key : "";

  return (
    <Card>
      <CardContent className="space-y-6 [&>.dashboard-section-header]:mb-0">
        <SectionHeader
          title="Install template"
          sub="The project and environment are created if needed. Failed installs are rolled back."
        />
        {/* Two fields or three, one row either way: a third that wrapped
              left a hole beside it. */}
        <div
          className={`grid gap-4 ${projectPick === NEW || envPick === NEW ? "sm:grid-cols-3" : "sm:grid-cols-2"}`}
        >
          <GlideSelect
            label="Project"
            value={projectPick}
            busy={projects.isLoading}
            searchable
            onChange={(v) => {
              setProjectPick(v);
              setEnvPick(NEW);
            }}
            options={[
              {
                value: NEW,
                label: "New project",
                icon: <PlusIcon className="size-4" />,
                tag: "New",
              },
              ...(projects.data ?? []).map((p) => ({ value: p.slug, label: p.slug })),
            ]}
            triggerClassName="flex h-10 w-full cursor-pointer items-center justify-between gap-2 rounded-lg border border-border-strong bg-secondary px-3 text-left text-sm font-medium text-foreground transition-colors hover:border-primary/40 hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
          />
          {projectPick === NEW ? (
            <TextField
              label="Project name"
              spellCheck={false}
              value={newProject}
              onChange={(e) => setNewProject(e.target.value)}
            />
          ) : (
            <GlideSelect
              label="Environment"
              value={envPick}
              busy={environments.isLoading}
              onChange={setEnvPick}
              searchable
              options={[
                {
                  value: NEW,
                  label: "New environment",
                  icon: <PlusIcon className="size-4" />,
                  tag: "New",
                },
                ...(environments.data ?? []).map((e) => ({ value: e.slug, label: e.slug })),
              ]}
              triggerClassName="flex h-10 w-full cursor-pointer items-center justify-between gap-2 rounded-lg border border-border-strong bg-secondary px-3 text-left text-sm font-medium text-foreground transition-colors hover:border-primary/40 hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
            />
          )}
          {(projectPick === NEW || envPick === NEW) && (
            <TextField
              label={projectPick === NEW ? "Environment" : "Environment name"}
              spellCheck={false}
              value={newEnvironment}
              onChange={(e) => setNewEnvironment(e.target.value)}
            />
          )}
        </div>

        {manifest.inputs.length > 0 && (
          <div className="grid gap-4 sm:grid-cols-2">
            {ordered.map((input) =>
              input.type === "domain" ? (
                <DomainInput
                  key={input.key}
                  label={input.required ? input.label : `${input.label} (optional)`}
                  hint={input.help}
                  value={inputs[input.key] ?? ""}
                  onChange={(value) => setInputs((current) => ({ ...current, [input.key]: value }))}
                  onRecord={(record) =>
                    setRecords((current) => {
                      const next = { ...current };
                      if (record) next[input.key] = record;
                      else delete next[input.key];
                      return next;
                    })
                  }
                  suggested={suggested[input.key]}
                  settings={settings.data}
                  address={settings.data?.public_ip}
                />
              ) : (
                <div key={input.key} className={input.key === lonely ? "sm:col-span-2" : undefined}>
                  <InputField
                    input={input}
                    value={inputs[input.key] ?? ""}
                    onChange={(value) =>
                      setInputs((current) => ({ ...current, [input.key]: value }))
                    }
                    stores={stores.data ?? []}
                    address={settings.data?.public_ip}
                  />
                </div>
              ),
            )}
          </div>
        )}

        <details className="group">
          <summary className="cursor-pointer text-xs text-muted-foreground select-none hover:text-foreground">
            Names on this instance
          </summary>
          <div className="mt-4 grid gap-4 sm:grid-cols-2">
            {nameFields.map((f) => (
              <TextField
                key={f.id}
                label={f.label}
                placeholder={f.fallback}
                value={names[f.id] ?? ""}
                onChange={(e) => setNames((current) => ({ ...current, [f.id]: e.target.value }))}
                hint={f.id.startsWith("apps.") ? undefined : "Unique on the whole instance."}
              />
            ))}
          </div>
        </details>

        <ErrorAlert error={error} />
        <div className="flex justify-end border-t border-border pt-5">
          <ActionButton onClick={install}>Install {template.title}</ActionButton>
        </div>
      </CardContent>
    </Card>
  );
}

function InputField({
  input,
  value,
  onChange,
  stores,
  address,
}: {
  input: TemplateInput;
  value: string;
  onChange: (value: string) => void;
  stores: ObjectStore[];
  address?: string;
}) {
  const label = input.required ? input.label : `${input.label} (optional)`;
  switch (input.type) {
    case "choice":
      return (
        <div className="space-y-2">
          <GlideSelect
            label={label}
            value={value || String(input.default ?? "")}
            onChange={onChange}
            options={(input.options ?? []).map((o) => ({ value: o, label: o }))}
            searchable
            triggerClassName="flex h-10 w-full cursor-pointer items-center justify-between gap-2 rounded-lg border border-border-strong bg-secondary px-3 text-left text-sm font-medium text-foreground transition-colors hover:border-primary/40 hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
          />
          {input.help && <p className="text-xs text-muted-foreground">{input.help}</p>}
        </div>
      );
    case "store":
      return (
        <div className="space-y-2">
          <GlideSelect
            label={label}
            value={value}
            onChange={onChange}
            placeholder="Pick an object store"
            emptyMessage="This instance has no object store yet."
            options={stores.map((s) => ({ value: s.name, label: s.name, tag: s.provider_label }))}
            searchable
            triggerClassName="flex h-10 w-full cursor-pointer items-center justify-between gap-2 rounded-lg border border-border-strong bg-secondary px-3 text-left text-sm font-medium text-foreground transition-colors hover:border-primary/40 hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
          />
          {input.help && <p className="text-xs text-muted-foreground">{input.help}</p>}
        </div>
      );
    case "secret":
      return (
        <TextField
          label={label}
          type="password"
          autoComplete="new-password"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder={input.generate ? "Generated when left empty" : undefined}
          hint={input.help}
        />
      );
    case "domain":
      return (
        <TextField
          label={label}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder="app.example.com"
          hint={
            input.help ??
            (address
              ? `The name has to resolve to this instance, ${address}, for the app to answer at it.`
              : "The name has to resolve to this instance for the app to answer at it.")
          }
        />
      );
    default:
      return (
        <TextField
          label={label}
          type={input.type === "number" ? "number" : "text"}
          value={value}
          placeholder={input.default !== undefined ? String(input.default) : undefined}
          onChange={(e) => onChange(e.target.value)}
          hint={input.help}
        />
      );
  }
}
