"use client";

import { useRouter } from "next/navigation";
import { useMemo, useRef, useState } from "react";
import type { Diagnostic, NormalizedManifest } from "@/lib/template";
import { validateTemplate } from "@/lib/template";
import { TemplateEditor, type TemplateEditorHandle } from "./editor";
import { Preview } from "./preview";
import { Problems } from "./problems";

const PLACEHOLDER = `version: 1
project: my-app

apps:
  - key: web
    image: nginx
    tag: "1"
    port: 80
    health: /
    domains:
      - host: \${input.domain}
`;

type ExistingTemplate = {
  slug: string;
  name: string;
  summary: string;
  tags: string[];
  imageKey: string | null;
  source: string;
};

async function readError(
  response: Response,
): Promise<{ message: string; diagnostics: Diagnostic[] }> {
  const body = await response.json().catch(() => null);
  return {
    message: body?.error?.message ?? `the server answered ${response.status}`,
    diagnostics: body?.diagnostics ?? [],
  };
}

export function TemplateForm({
  mode,
  template,
}: {
  mode: "new" | "edit";
  template?: ExistingTemplate;
}) {
  const router = useRouter();
  const editorRef = useRef<TemplateEditorHandle>(null);

  const [slug, setSlug] = useState(template?.slug ?? null);
  const [name, setName] = useState(template?.name ?? "");
  const [summary, setSummary] = useState(template?.summary ?? "");
  const [tagsText, setTagsText] = useState(template?.tags.join(", ") ?? "");
  const [source, setSource] = useState(template?.source ?? PLACEHOLDER);
  const [notes, setNotes] = useState("");
  const [photo, setPhoto] = useState<File | null>(null);

  const [serverDiagnostics, setServerDiagnostics] = useState<Diagnostic[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [published, setPublished] = useState<number | null>(null);

  // The very same module the server publishes with — there is no second,
  // looser check running here.
  const result = useMemo(() => validateTemplate(source), [source]);
  const diagnostics = serverDiagnostics.length > 0 ? serverDiagnostics : result.diagnostics;
  const manifest: NormalizedManifest | undefined = result.manifest;

  const tags = tagsText
    .split(",")
    .map((tag) => tag.trim())
    .filter(Boolean);

  function jumpTo(range: NonNullable<Diagnostic["range"]>) {
    editorRef.current?.jumpTo(range);
  }

  async function submit() {
    setSubmitting(true);
    setError(null);
    setServerDiagnostics([]);

    try {
      let currentSlug = slug;
      if (!currentSlug) {
        const response = await fetch("/api/v1/templates", {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ name, summary, tags }),
        });
        if (!response.ok) {
          const failed = await readError(response);
          throw new Error(failed.message);
        }
        currentSlug = ((await response.json()) as { slug: string }).slug;
        setSlug(currentSlug);
      } else {
        // Metadata may have changed since the draft was created — on a
        // retry after a rejected publish, or any edit — so it is kept in
        // sync on every submit rather than only the first one.
        const response = await fetch(`/api/v1/templates/${currentSlug}`, {
          method: "PATCH",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ name, summary, tags }),
        });
        if (!response.ok) throw new Error((await readError(response)).message);
      }

      if (photo) {
        const body = new FormData();
        body.append("file", photo);
        const response = await fetch(`/api/v1/templates/${currentSlug}/image`, {
          method: "POST",
          body,
        });
        if (!response.ok) throw new Error((await readError(response)).message);
      }

      const response = await fetch(`/api/v1/templates/${currentSlug}/versions`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ source, notes: notes || undefined }),
      });
      if (!response.ok) {
        // The server is the authority: its diagnostics replace the
        // editor's own until the author changes the file again.
        const failed = await readError(response);
        setServerDiagnostics(failed.diagnostics);
        throw new Error(failed.message);
      }

      const { number } = (await response.json()) as { number: number };
      setPublished(number);
      if (mode === "new") router.replace(`/templates/${currentSlug}/edit`);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "something went wrong");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="space-y-6">
      <div className="hud-frame grid gap-4 border border-fd-border p-4 sm:grid-cols-2">
        <label className="flex flex-col gap-1">
          <span className="label text-fd-muted-foreground">Name</span>
          <input
            value={name}
            onChange={(event) => setName(event.target.value)}
            minLength={3}
            maxLength={60}
            required
            className="border border-fd-border bg-transparent px-3 py-2 text-fd-foreground text-sm outline-none focus:border-fd-primary"
          />
        </label>
        <label className="flex flex-col gap-1">
          <span className="label text-fd-muted-foreground">Tags (comma separated)</span>
          <input
            value={tagsText}
            onChange={(event) => setTagsText(event.target.value)}
            className="border border-fd-border bg-transparent px-3 py-2 text-fd-foreground text-sm outline-none focus:border-fd-primary"
          />
        </label>
        <label className="flex flex-col gap-1 sm:col-span-2">
          <span className="label text-fd-muted-foreground">Summary</span>
          <input
            value={summary}
            onChange={(event) => setSummary(event.target.value)}
            minLength={10}
            maxLength={160}
            required
            className="border border-fd-border bg-transparent px-3 py-2 text-fd-foreground text-sm outline-none focus:border-fd-primary"
          />
        </label>
        <label className="flex flex-col gap-1 sm:col-span-2">
          <span className="label text-fd-muted-foreground">
            Photo (re-encoded to WebP, 2 MB max)
          </span>
          <input
            type="file"
            accept="image/*"
            onChange={(event) => setPhoto(event.target.files?.[0] ?? null)}
            className="text-fd-muted-foreground text-sm"
          />
        </label>
        {mode === "edit" ? (
          <label className="flex flex-col gap-1 sm:col-span-2">
            <span className="label text-fd-muted-foreground">What changed (optional)</span>
            <input
              value={notes}
              onChange={(event) => setNotes(event.target.value)}
              maxLength={500}
              className="border border-fd-border bg-transparent px-3 py-2 text-fd-foreground text-sm outline-none focus:border-fd-primary"
            />
          </label>
        ) : null}
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <div className="space-y-4">
          <TemplateEditor
            value={source}
            onChange={(next) => {
              setSource(next);
              setServerDiagnostics([]);
            }}
            diagnose={(text) => validateTemplate(text).diagnostics}
            editorRef={editorRef}
          />
          <Problems diagnostics={diagnostics} onJump={jumpTo} />
        </div>
        <div>
          {manifest ? (
            <Preview manifest={manifest} />
          ) : (
            <p className="label border border-fd-border p-4 text-fd-muted-foreground">
              Fix the errors on the left to see what this creates.
            </p>
          )}
        </div>
      </div>

      {error ? <p className="text-magenta text-sm">{error}</p> : null}
      {published !== null ? (
        <p className="text-primary text-sm">Published version {published}.</p>
      ) : null}

      <button
        type="button"
        onClick={submit}
        disabled={submitting || !result.ok || name.length < 3 || summary.length < 10}
        className="hud-frame border border-fd-primary px-4 py-2 font-mono text-fd-primary text-sm uppercase tracking-wide disabled:cursor-not-allowed disabled:opacity-40"
      >
        {submitting ? "Publishing…" : mode === "new" ? "Create and publish" : "Publish new version"}
      </button>
    </div>
  );
}
