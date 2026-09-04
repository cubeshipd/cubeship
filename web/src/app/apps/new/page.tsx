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
  const [source, setSource] = useState<"registry" | "external">("registry");
  const [image, setImage] = useState("");
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

        <Field label="Where the image comes from">
          <select
            className={inputClass}
            value={source}
            onChange={(e) => setSource(e.target.value as "registry" | "external")}
          >
            <option value="registry">Cubeship&apos;s registry — pushing deploys it</option>
            <option value="external">Another registry — you deploy when you want to</option>
          </select>
        </Field>

        {source === "external" && (
          <Field
            label="Image"
            hint="Without a tag — the tag is chosen each deploy. A private registry needs a login under Registries."
          >
            <input
              className={inputClass}
              value={image}
              onChange={(e) => setImage(e.target.value)}
              placeholder="registry.digitalocean.com/acme/api"
            />
          </Field>
        )}

        <Button type="submit" variant="primary" disabled={busy || !org || !project}>
          {busy ? "Creating…" : "Create app"}
        </Button>
      </form>
    </Card>
  );
}
