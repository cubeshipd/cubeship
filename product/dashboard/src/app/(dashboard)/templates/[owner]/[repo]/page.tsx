"use client";

import { useQuery } from "@tanstack/react-query";
import {
  BadgeCheckIcon,
  BoxIcon,
  DatabaseIcon,
  ExternalLinkIcon,
  HardDriveIcon,
} from "lucide-react";
import { useRouter } from "next/navigation";
import { type ReactNode, use, useEffect, useMemo, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ErrorAlert } from "@/components/error-alert";
import { RailPortal } from "@/components/header-rail";
import { LoadingList } from "@/components/loading";
import { SearchableSelect } from "@/components/searchable-select";
import { SectionHeader } from "@/components/section-header";
import { TemplateMark } from "@/components/template-card";
import { TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  api,
  type ObjectStore,
  type Settings,
  type TemplateDetail,
  type TemplateInput,
  type TemplateInstallStarted,
  type TemplateManifest,
} from "@/lib/api";
import { message } from "@/lib/errors";
import { handOverSecrets } from "@/lib/install-secrets";

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
            {t.owner}/{t.name} · {t.release.tag}
          </p>
        </div>
      </div>
      {t.manifest ? (
        <>
          <Creates manifest={t.manifest} />
          <InstallForm template={t} manifest={t.manifest} />
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
}: {
  template: TemplateDetail;
  manifest: TemplateManifest;
}) {
  const router = useRouter();
  const [project, setProject] = useState(manifest.project);
  const [environment, setEnvironment] = useState(manifest.environment);
  const [names, setNames] = useState<Record<string, string>>({});
  const [inputs, setInputs] = useState<Record<string, string>>({});
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
      const started = await api.post<TemplateInstallStarted>(
        `/templates/${template.owner}/${template.name}/installs`,
        {
          release: template.release.tag,
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
            <TextField
              label="Project"
              value={project}
              onChange={(e) => setProject(e.target.value)}
            />
            <TextField
              label="Environment"
              value={environment}
              onChange={(e) => setEnvironment(e.target.value)}
            />
          </div>

          {manifest.inputs.length > 0 && (
            <div className="grid gap-4 sm:grid-cols-2">
              {manifest.inputs.map((input) => (
                <InputField
                  key={input.key}
                  input={input}
                  value={inputs[input.key] ?? ""}
                  onChange={(value) => setInputs((current) => ({ ...current, [input.key]: value }))}
                  stores={stores.data ?? []}
                  address={settings.data?.public_ip}
                />
              ))}
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
            <ActionButton onClick={install}>Install {template.title}</ActionButton>
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
