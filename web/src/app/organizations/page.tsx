"use client";

import { useCallback, useEffect, useState } from "react";
import { api, type Environment, type Org, type Project } from "@/lib/api";
import { Button, Card, ErrorNote, Field, PageHeader, Shell, inputClass, message } from "@/components/ui";

// Organizations, the projects in them and the environments in those.
// One page rather than three: on a single-VPS install this whole tree is
// usually four rows, and clicking through it would be all navigation and
// no content.
export default function Organizations() {
  return (
    <Shell>
      <PageHeader title="Organizations" sub="Apps live in an environment, inside a project, inside an organization." />
      <OrgList />
    </Shell>
  );
}

function OrgList() {
  const [orgs, setOrgs] = useState<Org[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(() => {
    api.get<Org[]>("/orgs").then(setOrgs).catch((e) => setError(message(e)));
  }, []);
  useEffect(reload, [reload]);

  return (
    <>
      <ErrorNote error={error} />
      {orgs?.map((o) => (
        <OrgCard key={o.slug} org={o} onChange={reload} onError={setError} />
      ))}
      <NewOrg onCreated={reload} onError={setError} />
    </>
  );
}

function OrgCard({
  org,
  onChange,
  onError,
}: {
  org: Org;
  onChange: () => void;
  onError: (m: string) => void;
}) {
  const [projects, setProjects] = useState<Project[] | null>(null);

  const reload = useCallback(() => {
    api
      .get<Project[]>(`/orgs/${org.slug}/projects`)
      .then(setProjects)
      .catch((e) => onError(message(e)));
  }, [org.slug, onError]);
  useEffect(reload, [reload]);

  async function remove() {
    try {
      await api.del(`/orgs/${org.slug}`);
      onChange();
    } catch (e) {
      onError(message(e));
    }
  }

  return (
    <Card>
      <div className="flex items-baseline justify-between">
        <div>
          <span className="font-medium">{org.name}</span>{" "}
          <span className="font-mono text-xs text-muted">{org.slug}</span>
        </div>
        <Button variant="danger" onClick={remove}>
          Delete
        </Button>
      </div>

      <div className="mt-3 border-t border-line pt-3">
        {projects?.length === 0 && <p className="text-sm text-muted">No projects.</p>}
        {projects?.map((p) => (
          <ProjectRow key={p.slug} org={org.slug} project={p} onChange={reload} onError={onError} />
        ))}
        <NewProject org={org.slug} onCreated={reload} onError={onError} />
      </div>
    </Card>
  );
}

function ProjectRow({
  org,
  project,
  onChange,
  onError,
}: {
  org: string;
  project: Project;
  onChange: () => void;
  onError: (m: string) => void;
}) {
  const [envs, setEnvs] = useState<Environment[] | null>(null);
  const [adding, setAdding] = useState(false);
  const [slug, setSlug] = useState("");

  const path = `/orgs/${org}/projects/${project.slug}`;
  const reload = useCallback(() => {
    api
      .get<Environment[]>(`${path}/environments`)
      .then(setEnvs)
      .catch((e) => onError(message(e)));
  }, [path, onError]);
  useEffect(reload, [reload]);

  async function addEnv(e: React.FormEvent) {
    e.preventDefault();
    try {
      await api.post(`${path}/environments`, { slug, name: slug });
      setSlug("");
      setAdding(false);
      reload();
    } catch (err) {
      onError(message(err));
    }
  }

  return (
    <div className="mb-2 rounded-md bg-raised p-3">
      <div className="flex items-baseline justify-between">
        <div>
          <span className="text-sm">{project.name}</span>{" "}
          <span className="font-mono text-xs text-muted">{project.slug}</span>
        </div>
        <button
          className="text-xs text-muted hover:text-bad"
          onClick={async () => {
            try {
              await api.del(path);
              onChange();
            } catch (e) {
              onError(message(e));
            }
          }}
        >
          Delete
        </button>
      </div>
      <div className="mt-2 flex flex-wrap items-center gap-2">
        {envs?.map((e) => (
          <span key={e.slug} className="rounded-full border border-line px-2 py-0.5 font-mono text-[11px] text-muted">
            {e.slug}
          </span>
        ))}
        {adding ? (
          <form onSubmit={addEnv} className="flex gap-2">
            <input
              className={`${inputClass} w-40 py-1`}
              value={slug}
              onChange={(e) => setSlug(e.target.value)}
              placeholder="staging"
              autoFocus
            />
            <Button type="submit" className="py-1">
              Add
            </Button>
          </form>
        ) : (
          <button className="text-xs text-muted hover:text-body" onClick={() => setAdding(true)}>
            + environment
          </button>
        )}
      </div>
    </div>
  );
}

function NewProject({
  org,
  onCreated,
  onError,
}: {
  org: string;
  onCreated: () => void;
  onError: (m: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [slug, setSlug] = useState("");
  const [name, setName] = useState("");

  if (!open)
    return (
      <button className="text-xs text-muted hover:text-body" onClick={() => setOpen(true)}>
        + project
      </button>
    );

  return (
    <form
      className="flex gap-2"
      onSubmit={async (e) => {
        e.preventDefault();
        try {
          await api.post(`/orgs/${org}/projects`, { slug, name: name || slug });
          setSlug("");
          setName("");
          setOpen(false);
          onCreated();
        } catch (err) {
          onError(message(err));
        }
      }}
    >
      <input
        className={`${inputClass} py-1`}
        value={slug}
        onChange={(e) => setSlug(e.target.value)}
        placeholder="slug"
        autoFocus
      />
      <input
        className={`${inputClass} py-1`}
        value={name}
        onChange={(e) => setName(e.target.value)}
        placeholder="Name"
      />
      <Button type="submit" className="py-1">
        Create
      </Button>
    </form>
  );
}

function NewOrg({ onCreated, onError }: { onCreated: () => void; onError: (m: string) => void }) {
  const [slug, setSlug] = useState("");
  const [name, setName] = useState("");

  return (
    <Card>
      <form
        className="flex items-end gap-2"
        onSubmit={async (e) => {
          e.preventDefault();
          try {
            await api.post("/orgs", { slug, name: name || slug });
            setSlug("");
            setName("");
            onCreated();
          } catch (err) {
            onError(message(err));
          }
        }}
      >
        <div className="flex-1">
          <Field label="Slug">
            <input className={inputClass} value={slug} onChange={(e) => setSlug(e.target.value)} />
          </Field>
        </div>
        <div className="flex-1">
          <Field label="Name">
            <input className={inputClass} value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
        </div>
        <Button type="submit" className="mb-3">
          New organization
        </Button>
      </form>
    </Card>
  );
}
