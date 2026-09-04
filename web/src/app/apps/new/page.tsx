"use client";

import { cn } from "cn";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { ErrorAlert } from "@/components/error-alert";
import { useOrg } from "@/components/org-context";
import { PageHeader } from "@/components/page-header";
import { Shell } from "@/components/shell";
import { TextField } from "@/components/text-field";
import { Card, CardContent } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { type App, api, type Environment, type Project } from "@/lib/api";
import { message } from "@/lib/errors";

export default function NewApp() {
  return (
    <Shell>
      <PageHeader
        title="New app"
        sub="An app is named by where it lives: org/project/environment/name."
      />
      <Suspense>
        <Form />
      </Suspense>
    </Shell>
  );
}

function Form() {
  const router = useRouter();
  // The organization is the frame the whole dashboard is in, so it is
  // read from the sidebar rather than asked for a fourth time here.
  const { org } = useOrg();
  // Reached from a project's environment, which is the answer to two of
  // the three questions below — so they arrive filled in.
  const params = useSearchParams();
  const fromProject = params.get("project") ?? "";
  const fromEnv = params.get("env") ?? "";
  const [projects, setProjects] = useState<Project[]>([]);
  const [envs, setEnvs] = useState<Environment[]>([]);

  const [project, setProject] = useState("");
  const [environment, setEnvironment] = useState("");
  const [name, setName] = useState("");
  const [domain, setDomain] = useState("");
  const [source, setSource] = useState<"registry" | "external">("registry");
  const [image, setImage] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!org) return;
    api.get<Project[]>(`/orgs/${org}/projects`).then((p) => {
      setProjects(p);
      setProject(p.some((x) => x.slug === fromProject) ? fromProject : (p[0]?.slug ?? ""));
    });
  }, [org, fromProject]);

  useEffect(() => {
    if (!org || !project) return;
    api.get<Environment[]>(`/orgs/${org}/projects/${project}/environments`).then((e) => {
      setEnvs(e);
      const preferred = e.some((x) => x.slug === fromEnv) ? fromEnv : "production";
      setEnvironment(e.find((x) => x.slug === preferred)?.slug ?? e[0]?.slug ?? "");
    });
  }, [org, project, fromEnv]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const created = await api.post<App>("/apps", {
        name,
        domain,
        org,
        project,
        environment,
        source,
        // Only an external app names one; sending an empty string for a
        // registry app is the same as not naming one.
        image: source === "external" ? image.trim() : "",
      });
      router.push(`/apps?ref=${created.reference}`);
    } catch (err) {
      setError(message(err));
      setBusy(false);
    }
  }

  const reference = [org, project, environment, name || "<name>"].filter(Boolean).join("/");

  return (
    <Card>
      <CardContent>
        <ErrorAlert error={error} />

        <form onSubmit={submit} className="space-y-5">
          <div className="grid gap-4 sm:grid-cols-2">
            <SlugSelect label="Project" value={project} onChange={setProject} options={projects} />
            <SlugSelect
              label="Environment"
              value={environment}
              onChange={setEnvironment}
              options={envs}
            />
          </div>

          <TextField
            label="Name"
            hint={
              <>
                Unique within its environment. The app will be{" "}
                <code className="text-muted-foreground">{reference}</code>.
              </>
            }
            className="font-mono"
            spellCheck={false}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />

          <TextField
            label="Domain"
            hint="Where Traefik serves it."
            className="font-mono"
            spellCheck={false}
            value={domain}
            onChange={(e) => setDomain(e.target.value)}
            placeholder="app.example.com"
          />

          <div className="space-y-2">
            <Label className="text-xs text-muted-foreground">Where the image comes from</Label>
            <div className="grid gap-2 sm:grid-cols-2">
              <SourceOption
                selected={source === "registry"}
                onSelect={() => setSource("registry")}
                title="Cubeship's registry"
                body="Pushing to it is the deploy. Needs a domain before there is anywhere to push."
              />
              <SourceOption
                selected={source === "external"}
                onSelect={() => setSource("external")}
                title="Another registry"
                body="Nothing tells Cubeship when it is pushed to, so you deploy when you want to."
              />
            </div>
          </div>

          {source === "external" && (
            <TextField
              label="Image"
              hint="Without a tag — the tag is chosen each deploy. A private registry needs a login under Registries."
              className="font-mono"
              spellCheck={false}
              value={image}
              onChange={(e) => setImage(e.target.value)}
              placeholder="registry.digitalocean.com/acme/api"
            />
          )}

          <ActionButton type="submit" busy={busy} disabled={!org || !project}>
            Create app
          </ActionButton>
        </form>
      </CardContent>
    </Card>
  );
}

// The three selects that name where an app lives. They differ only in
// what they are filled with, so they are one component.
function SlugSelect({
  label,
  value,
  onChange,
  options,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  options: { slug: string }[];
}) {
  return (
    <div className="space-y-2">
      <Label className="text-xs text-muted-foreground">{label}</Label>
      <Select value={value} onValueChange={(v) => onChange(String(v))}>
        <SelectTrigger className="w-full font-mono">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {options.map((o) => (
            <SelectItem key={o.slug} value={o.slug} className="font-mono">
              {o.slug}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

// The source decides whether a push deploys the app, and it cannot be
// changed later — which is why it is two things to read rather than two
// lines in a dropdown.
function SourceOption({
  selected,
  onSelect,
  title,
  body,
}: {
  selected: boolean;
  onSelect: () => void;
  title: string;
  body: string;
}) {
  return (
    <button
      type="button"
      onClick={onSelect}
      aria-pressed={selected}
      className={cn(
        "border p-3 text-left transition-all",
        selected
          ? "neon-edge border-primary/60 bg-primary/8"
          : "border-border bg-background hover:border-border-strong",
      )}
    >
      <div className="flex items-center gap-2 text-sm font-medium">
        <span
          className={cn(
            "size-3 rounded-full border",
            selected ? "border-primary bg-primary/40" : "border-border-strong",
          )}
        />
        {title}
      </div>
      <p className="mt-1.5 text-xs leading-relaxed text-muted-foreground">{body}</p>
    </button>
  );
}
