"use client";

import { ChevronLeftIcon } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { use, useCallback, useEffect, useState } from "react";
import { ActionButton } from "@/components/action-button";
import { AppNetwork } from "@/components/app-network";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { DangerAction, DangerZone } from "@/components/danger-zone";
import { ErrorAlert } from "@/components/error-alert";
import { GitHubSource } from "@/components/github-source";
import { LoadingList } from "@/components/loading";
import { OptionCards } from "@/components/option-cards";
import { PageHeader, SectionHeader } from "@/components/page-header";
import { TextAreaField, TextField } from "@/components/text-field";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { type App, type AppSource, api } from "@/lib/api";
import { message } from "@/lib/errors";

// The daemon has four sources. There are only two things an app can be:
// something this instance builds, or something someone else already
// built. Which of the two ways it is built — and which of the two
// registries an image comes from — is the next question down.
type Origin = "github" | "image";

const SOURCE: Record<Origin, Record<string, AppSource>> = {
  github: { railpack: "railpack", dockerfile: "dockerfile" },
  image: { cubeship: "registry", external: "external" },
};

export default function AppSettingsPage({
  params,
}: PageProps<"/projects/[project]/[env]/[app]/settings">) {
  const { project, env, app } = use(params);
  return <Settings reference={`${project}/${env}/${app}`} />;
}

function Settings({ reference }: { reference: string }) {
  const router = useRouter();
  const [app, setApp] = useState<App | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);

  const path = `/apps/${reference}`;
  const reload = useCallback(() => {
    if (!reference) return;
    api
      .get<App>(path)
      .then(setApp)
      .catch((e) => setError(message(e)));
  }, [path, reference]);
  useEffect(reload, [reload]);

  if (!reference) {
    return (
      <p className="text-sm text-muted-foreground">
        No app named.{" "}
        <Link href="/" className="text-foreground underline underline-offset-4">
          Back to projects
        </Link>
        .
      </p>
    );
  }
  if (error && !app) return <ErrorAlert error={error} />;

  return (
    <>
      <Link
        href={`/projects/${reference}`}
        className="mb-4 inline-flex items-center gap-1.5 font-mono text-[11px] text-muted-foreground transition-colors hover:text-primary"
      >
        <ChevronLeftIcon className="size-3.5" />
        {reference}
      </Link>

      <PageHeader title="App settings" />

      <ErrorAlert error={error} />

      {/* The heading above says the same thing for every app and comes
          from the URL, so it does not wait — see the app's own page for
          why returning null until the answer lands made every
          navigation blink. */}
      {!app && <LoadingList rows={4} />}

      {app && (
        <>
          <General app={app} onSaved={setApp} onError={setError} />
          <AppNetwork app={app} onSaved={setApp} />
          <SourceSection app={app} onSaved={setApp} onError={setError} />

          <DangerZone>
            <DangerAction
              title="Delete this app"
              description="Its container is stopped first. Images already pushed stay in the registry — reclaiming that disk needs a garbage collection pass Cubeship does not run."
              action={
                <Button variant="destructive" onClick={() => setDeleting(true)}>
                  Delete app
                </Button>
              }
            />
          </DangerZone>

          <ConfirmDialog
            open={deleting}
            onOpenChange={setDeleting}
            title="Delete app"
            description="The container serving it is stopped and the app is gone. This cannot be undone."
            confirmWord={app.name}
            confirmLabel="Delete app"
            onConfirm={async () => {
              await api.del(path);
              router.push(`/projects/${app.project}/${app.environment}`);
            }}
          />
        </>
      )}
    </>
  );
}

type SectionProps = {
  app: App;
  onSaved: (a: App) => void;
  onError: (m: string | null) => void;
};

// patch is every section's save: PATCH leaves out what it does not
// mention, so one section cannot blank another's fields.
function usePatch({ app, onSaved, onError }: SectionProps) {
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);

  async function save(body: Record<string, string>) {
    setBusy(true);
    onError(null);
    setSaved(false);
    try {
      onSaved(await api.patch<App>(`/apps/${app.reference}`, body));
      setSaved(true);
    } catch (err) {
      onError(message(err));
    }
    setBusy(false);
  }

  return { busy, saved, setSaved, save };
}

function General(props: SectionProps) {
  const { app } = props;
  const { busy, saved, setSaved, save } = usePatch(props);
  const [description, setDescription] = useState(app.description ?? "");
  const dirty = description !== (app.description ?? "");

  return (
    <>
      <SectionHeader title="General" />
      <Card>
        <CardContent>
          <form
            className="space-y-4"
            onSubmit={(e) => {
              e.preventDefault();
              save({ description });
            }}
          >
            <TextAreaField
              label="Description"
              hint="What this app is. Empty is fine."
              rows={3}
              value={description}
              onChange={(e) => {
                setDescription(e.target.value);
                setSaved(false);
              }}
            />

            <div className="space-y-2">
              <Label className="text-xs text-muted-foreground">Reference</Label>
              <div className="flex h-10 items-center border border-border bg-secondary/40 px-3 font-mono text-sm text-muted-foreground">
                {app.reference}
              </div>
              <p className="text-xs text-subtle-foreground">
                Not editable. It is this app&apos;s registry repository path and the basis of its
                container and router names.
              </p>
            </div>

            <SaveRow busy={busy} saved={saved} dirty={dirty} />
          </form>
        </CardContent>
      </Card>
    </>
  );
}

function SourceSection(props: SectionProps) {
  const { app } = props;
  const { busy, saved, setSaved, save } = usePatch(props);

  const builds = app.source === "dockerfile" || app.source === "railpack";
  const [origin, setOrigin] = useState<Origin>(builds ? "github" : "image");
  const [buildWith, setBuildWith] = useState(
    app.source === "dockerfile" ? "dockerfile" : "railpack",
  );
  const [imageFrom, setImageFrom] = useState(app.source === "external" ? "external" : "cubeship");
  const [repo, setRepo] = useState(app.repo ?? "");
  const [gitRef, setGitRef] = useState(app.ref ?? "");
  const [dockerfile, setDockerfile] = useState(app.dockerfile ?? "");
  const [image, setImage] = useState(app.image ?? "");

  const source = SOURCE[origin][origin === "github" ? buildWith : imageFrom];
  const nowBuilds = origin === "github";
  const problem = originProblem(source, { repo, image });

  const touch = () => setSaved(false);

  return (
    <>
      <SectionHeader title="Source" sub="Where this app's image comes from." />
      <Card>
        <CardContent>
          <form
            className="space-y-5"
            onSubmit={(e) => {
              e.preventDefault();
              // The source and its settings travel together: the daemon
              // judges them as one, and refuses a setting the source
              // would ignore.
              save({
                source,
                image: source === "external" ? image.trim() : "",
                repo: nowBuilds ? repo.trim() : "",
                ref: nowBuilds ? gitRef.trim() : "",
                dockerfile: source === "dockerfile" ? dockerfile.trim() : "",
              });
            }}
          >
            <OptionCards<Origin>
              value={origin}
              onChange={(v) => {
                setOrigin(v);
                touch();
              }}
              options={[
                {
                  value: "github",
                  title: "Git provider",
                  body: "Cubeship clones the repository and builds it here, so what runs is code this instance compiled.",
                },
                {
                  value: "image",
                  title: "Docker image",
                  body: "An image someone already built and published. Cubeship runs it as it is.",
                },
              ]}
            />

            {origin === "github" ? (
              <div className="space-y-5 border-l-2 border-primary/40 pl-4">
                <GitHubSource
                  repo={repo}
                  gitRef={gitRef}
                  onRepo={(url, defaultBranch) => {
                    setRepo(url);
                    // A repository's default branch is the right answer
                    // until someone says otherwise, and choosing a new
                    // repository makes the old branch meaningless.
                    setGitRef(defaultBranch);
                    touch();
                  }}
                  onRef={(v) => {
                    setGitRef(v);
                    touch();
                  }}
                />

                <OptionCards
                  label="How it is built"
                  value={buildWith}
                  onChange={(v) => {
                    setBuildWith(v);
                    touch();
                  }}
                  options={[
                    {
                      value: "railpack",
                      title: "Railpack",
                      body: "No Dockerfile needed — Railpack reads the repository, works out what it is, and produces the build.",
                    },
                    {
                      value: "dockerfile",
                      title: "Dockerfile",
                      body: "Build the Dockerfile in the repository, exactly as written.",
                    },
                  ]}
                />

                {buildWith === "dockerfile" && (
                  <TextField
                    label="Dockerfile path"
                    hint="Optional. Relative to the repository root."
                    spellCheck={false}
                    value={dockerfile}
                    onChange={(e) => {
                      setDockerfile(e.target.value);
                      touch();
                    }}
                    placeholder="Dockerfile"
                  />
                )}
              </div>
            ) : (
              <div className="space-y-5 border-l-2 border-primary/40 pl-4">
                <OptionCards
                  label="Where the image comes from"
                  value={imageFrom}
                  onChange={(v) => {
                    setImageFrom(v);
                    touch();
                  }}
                  options={[
                    {
                      value: "cubeship",
                      title: "Cubeship's registry",
                      body: "Pushing to it is the deploy. Needs an instance domain before there is anywhere to push.",
                    },
                    {
                      value: "external",
                      title: "Another registry",
                      body: "Nothing tells Cubeship when it is pushed to, so you deploy when you want to.",
                    },
                  ]}
                />

                {imageFrom === "external" && (
                  <TextField
                    label="Image"
                    hint="Without a tag — the tag is the deploy's argument, and an app pinned to one could never be told to run another. A private registry needs a login under Registries."
                    spellCheck={false}
                    value={image}
                    onChange={(e) => {
                      setImage(e.target.value);
                      touch();
                    }}
                    placeholder="registry.digitalocean.com/acme/api"
                  />
                )}
              </div>
            )}

            {problem && <ErrorAlert error={problem} />}
            <SaveRow busy={busy} saved={saved} dirty={!problem} />
          </form>
        </CardContent>
      </Card>
    </>
  );
}

function SaveRow({ busy, saved, dirty }: { busy: boolean; saved: boolean; dirty: boolean }) {
  return (
    <div className="flex items-center gap-3">
      <ActionButton type="submit" busy={busy} disabled={!dirty}>
        Save
      </ActionButton>
      {saved && <span className="text-xs text-muted-foreground">Saved.</span>}
    </div>
  );
}

// The same refusals the daemon makes, said here instead — so a mistake
// is a sentence under the field rather than a rejected submit. The
// daemon still checks: this is a courtesy, not the rule.
function originProblem(source: AppSource, o: { repo: string; image: string }): string | null {
  if (source === "external") {
    const image = o.image.trim();
    if (!image) return null;
    if (image.includes("://") || /\s/.test(image)) {
      return "That is an image reference, not a URL — registry.example.com/acme/api.";
    }
    if ((image.split("/").pop() ?? "").includes(":")) {
      return "Leave the tag off. Which tag to run is what a deploy chooses, and an app pinned to one could never be told to run another.";
    }
    return null;
  }

  if (source === "dockerfile" || source === "railpack") {
    const repo = o.repo.trim();
    if (!repo) return null;
    if (!/^(https|http|git):\/\//.test(repo)) {
      return "The repository must be an https://, http:// or git:// URL — ssh needs a key this instance does not have.";
    }
    if (repo.includes("#")) {
      return "Put the branch or commit in its own field, not in the URL.";
    }
  }
  return null;
}
