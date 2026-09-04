"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, type App, type Environment, type Org, type Project } from "@/lib/api";
import { Button, Card, ErrorNote, Field, PageHeader, Shell, inputClass, message } from "@/components/ui";

export default function NewApp() {
  return (
    <Shell>
      <PageHeader title="New app" sub="An app is named by where it lives: org/project/environment/name." />
      <Form />
    </Shell>
  );
}

function Form() {
  const router = useRouter();
  const [orgs, setOrgs] = useState<Org[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [envs, setEnvs] = useState<Environment[]>([]);

  const [org, setOrg] = useState("");
  const [project, setProject] = useState("");
  const [environment, setEnvironment] = useState("");
  const [name, setName] = useState("");
  const [domain, setDomain] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api.get<Org[]>("/orgs").then((o) => {
      setOrgs(o);
      setOrg((current) => current || o[0]?.slug || "");
    });
  }, []);

  useEffect(() => {
    if (!org) return;
    api.get<Project[]>(`/orgs/${org}/projects`).then((p) => {
      setProjects(p);
      setProject(p[0]?.slug ?? "");
    });
  }, [org]);

  useEffect(() => {
    if (!org || !project) return;
    api.get<Environment[]>(`/orgs/${org}/projects/${project}/environments`).then((e) => {
      setEnvs(e);
      setEnvironment(e.find((x) => x.slug === "production")?.slug ?? e[0]?.slug ?? "");
    });
  }, [org, project]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const created = await api.post<App>("/apps", { name, domain, org, project, environment });
      router.push(`/apps?ref=${created.reference}`);
    } catch (err) {
      setError(message(err));
      setBusy(false);
    }
  }

  return (
    <Card>
      <ErrorNote error={error} />
      <form onSubmit={submit}>
        <div className="grid grid-cols-3 gap-3">
          <Field label="Organization">
            <select className={inputClass} value={org} onChange={(e) => setOrg(e.target.value)}>
              {orgs.map((o) => (
                <option key={o.slug} value={o.slug}>
                  {o.slug}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Project">
            <select className={inputClass} value={project} onChange={(e) => setProject(e.target.value)}>
              {projects.map((p) => (
                <option key={p.slug} value={p.slug}>
                  {p.slug}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Environment">
            <select
              className={inputClass}
              value={environment}
              onChange={(e) => setEnvironment(e.target.value)}
            >
              {envs.map((e) => (
                <option key={e.slug} value={e.slug}>
                  {e.slug}
                </option>
              ))}
            </select>
          </Field>
        </div>

        <Field label="Name" hint="Unique within its environment. Becomes part of the registry path.">
          <input className={inputClass} value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label="Domain" hint="Where Traefik serves it.">
          <input
            className={inputClass}
            value={domain}
            onChange={(e) => setDomain(e.target.value)}
            placeholder="app.example.com"
          />
        </Field>

        <Button type="submit" variant="primary" disabled={busy || !org || !project}>
          {busy ? "Creating…" : "Create app"}
        </Button>
      </form>
    </Card>
  );
}
