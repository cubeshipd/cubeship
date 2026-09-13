"use client";

import { useQuery } from "@tanstack/react-query";
import {
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
import { RailPortal } from "@/components/header-rail";
import { LoadingList } from "@/components/loading";
import { Notice } from "@/components/notice";
import { SearchableSelect } from "@/components/searchable-select";
import { SectionHeader } from "@/components/section-header";
import { TemplateMark } from "@/components/template-card";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
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
  type TemplateReleaseManifest,
  type TemplateReleaseOption,
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
  const releases = useQuery({
    queryKey: ["templates", owner, repo, "releases"],
    queryFn: () =>
      api.get<{ releases: TemplateReleaseOption[] }>(`/templates/${owner}/${repo}/releases`),
  });
  // Empty is the newest, whose manifest the template already carries. Any
  // other version is read the way installing it would read it.
  const [chosen, setChosen] = useState("");
  const newest = template.data?.release.tag ?? "";
  const other = chosen !== "" && chosen !== newest;
  const older = useQuery({
    queryKey: ["templates", owner, repo, "manifest", chosen],
    queryFn: () =>
      api.get<TemplateReleaseManifest>(
        `/templates/${owner}/${repo}/manifest?release=${encodeURIComponent(chosen)}`,
      ),
    enabled: other,
  });

  if (template.error) return <ErrorAlert error={message(template.error)} />;
  if (!template.data) return <LoadingList rows={5} />;
  const t = template.data;
  const tag = other ? chosen : newest;
  const manifest = other ? older.data?.manifest : t.manifest;

  return (
    <>
      <RailPortal>
        <Button
          variant="outline"
          nativeButton={false}
          render={
            <a href={t.url} target="_blank" rel="noreferrer noopener">
              <ExternalLinkIcon />
              GitHub
            </a>
          }
        />
      </RailPortal>
      <div className="mb-8 flex items-center gap-4">
        <TemplateMark src={t.icon_url} className="size-14" />
        <div className="min-w-0">
          <h1 className="flex items-center gap-2 text-lg font-semibold">
            {t.title}
            {t.verified && (
              <BadgeCheckIcon className="size-5 text-primary" aria-label="Verified publisher" />
            )}
          </h1>
          <p className="text-sm text-muted-foreground">{t.description}</p>
          <p className="mt-0.5 font-mono text-xs text-subtle-foreground">
            {t.owner}/{t.name} · {tag}
          </p>
        </div>
        <div className="ml-auto w-48 shrink-0">
          <SearchableSelect
            label="Version"
            value={tag}
            busy={releases.data === undefined && !releases.error}
            onChange={setChosen}
            choices={(releases.data?.releases ?? [t.release]).map((r) => ({
              value: r.tag,
              label: r.tag,
              hint:
                r.tag === newest
                  ? "latest"
                  : r.published_at
                    ? new Date(r.published_at).toLocaleDateString()
                    : undefined,
            }))}
          />
        </div>
      </div>
      {other && older.error ? (
        <ErrorAlert error={message(older.error)} />
      ) : other && !older.data ? (
        <LoadingList rows={5} />
      ) : manifest ? (
        <>
          {other && older.data && !older.data.fits && (
            <Notice tone="warning">{older.data.problem}</Notice>
          )}
          <Creates manifest={manifest} />
          <InstallForm
            key={tag}
            template={t}
            manifest={manifest}
            release={tag}
            blocked={other && older.data ? !older.data.fits : false}
          />
        </>
      ) : (
        <ErrorAlert error="The catalog has no manifest for this template's release." />
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
      detail:
        a.source.type === "image"
          ? `${a.source.image}:${a.source.tag ?? "latest"}`
          : (a.source.repo ?? a.source.type),
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
      detail: s.buckets.length > 0 ? s.buckets.join(", ") : "object storage",
    })),
  ];
  return (
    <>
      <SectionHeader title="What this creates" />
      <Card className="gap-0 divide-y divide-border py-0">
        {rows.map((r) => (
          <div key={r.key} className="flex items-center gap-3 px-4 py-2.5">
            <span className="text-primary [&_svg]:size-4">{r.icon}</span>
            <span className="font-mono text-sm">{r.name}</span>
            <span className="min-w-0 truncate font-mono text-xs text-muted-foreground">
              {r.detail}
            </span>
          </div>
        ))}
      </Card>
    </>
  );
}

function InstallForm({
  template,
  manifest,
  release,
  blocked,
}: {
  template: TemplateDetail;
  manifest: TemplateManifest;
  // The version installed.
  release: string;
  // A version this instance is too old for, which the daemon would refuse.
  blocked: boolean;
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
          release,
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

  return (
    <>
      <SectionHeader
        title="Install"
        sub="The project and environment are created when they do not exist. Anything that fails is undone."
      />
      <Card>
        <CardContent className="space-y-6">
          <div className="grid gap-4 sm:grid-cols-2">
            <SearchableSelect
              label="Project"
              value={projectPick}
              busy={projects.isLoading}
              onChange={(v) => {
                setProjectPick(v);
                setEnvPick(NEW);
              }}
              choices={[
                {
                  value: NEW,
                  label: "New project",
                  icon: PlusIcon,
                  hint: "Created by the install",
                },
                ...(projects.data ?? []).map((p) => ({ value: p.slug, label: p.slug })),
              ]}
            />
            {projectPick === NEW ? (
              <TextField
                label="Project name"
                spellCheck={false}
                value={newProject}
                onChange={(e) => setNewProject(e.target.value)}
              />
            ) : (
              <SearchableSelect
                label="Environment"
                value={envPick}
                busy={environments.isLoading}
                onChange={setEnvPick}
                choices={[
                  { value: NEW, label: "New environment", icon: PlusIcon },
                  ...(environments.data ?? []).map((e) => ({ value: e.slug, label: e.slug })),
                ]}
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
              {manifest.inputs.map((input) =>
                input.type === "domain" ? (
                  <DomainInput
                    key={input.key}
                    label={input.required ? input.label : `${input.label} (optional)`}
                    hint={input.help}
                    value={inputs[input.key] ?? ""}
                    onChange={(value) =>
                      setInputs((current) => ({ ...current, [input.key]: value }))
                    }
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
                  <InputField
                    key={input.key}
                    input={input}
                    value={inputs[input.key] ?? ""}
                    onChange={(value) =>
                      setInputs((current) => ({ ...current, [input.key]: value }))
                    }
                    stores={stores.data ?? []}
                    address={settings.data?.public_ip}
                  />
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
          <div className="flex justify-end">
            <ActionButton onClick={install} disabled={blocked}>
              Install {template.title} {release}
            </ActionButton>
          </div>
        </CardContent>
      </Card>
    </>
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
        <SearchableSelect
          label={label}
          hint={input.help}
          value={value || String(input.default ?? "")}
          onChange={onChange}
          choices={(input.options ?? []).map((o) => ({ value: o, label: o }))}
        />
      );
    case "store":
      return (
        <SearchableSelect
          label={label}
          hint={input.help}
          value={value}
          onChange={onChange}
          placeholder="Pick an object store"
          empty="This instance has no object store yet."
          choices={stores.map((s) => ({ value: s.name, label: s.name }))}
        />
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
