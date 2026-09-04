"use client";

import { BoxIcon, ChevronLeftIcon, ChevronRightIcon, SettingsIcon, Trash2Icon } from "lucide-react";
import Link from "next/link";
import { use, useCallback, useEffect, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { CopyButton } from "@/components/copy-button";
import { ErrorAlert } from "@/components/error-alert";
import { AWSIcon, DigitalOceanIcon } from "@/components/icons";
import { LoadingList, LoadingNote } from "@/components/loading";
import { Notice } from "@/components/notice";
import { useOrg } from "@/components/org-context";
import { PageHeader } from "@/components/page-header";
import { SearchBar } from "@/components/search-bar";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  api,
  type RegistryCredential,
  type RegistryImage,
  type RegistryRepository,
  type RegistryUsage,
  type Settings,
} from "@/lib/api";
import { message } from "@/lib/errors";

// What a registry holds.
//
// Two registries answer this and they are asked differently: Cubeship's
// own has no credential — a push authenticates with the pusher's API key
// — so it is addressed as the organization's registry rather than by an
// id. Everything below the fetch is the same either way.
export default function RegistryDetail({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const { org } = useOrg();
  // "cubeship" is the reserved name for the one registry this instance
  // runs. Every other id is a stored credential's, which is a number, so
  // the two can never collide.
  const own = id === "cubeship";

  // The host and the provider used to travel in the query string beside
  // the id. They come from the credential now: an id is enough to say
  // which registry this is, and a link that had to carry three values to
  // render a heading was a link nobody could type.
  const [credential, setCredential] = useState<RegistryCredential | null>(null);
  useEffect(() => {
    if (own || !org) return;
    api
      .get<RegistryCredential[]>(`/orgs/${org}/registries`)
      .then((list) => setCredential(list.find((c) => String(c.id) === id) ?? null))
      .catch(() => setCredential(null));
  }, [org, own, id]);
  const host = credential?.host ?? "";

  const [repos, setRepos] = useState<RegistryRepository[] | null>(null);
  // A set, not one name: comparing two repositories means having both
  // open, and one that closes when you open the next makes that
  // impossible.
  const [open, setOpen] = useState<Set<string>>(new Set());
  const [images, setImages] = useState<Record<string, RegistryImage[]>>({});
  const [error, setError] = useState<string | null>(null);
  const [unsupported, setUnsupported] = useState(false);
  const [query, setQuery] = useState("");
  const [usage, setUsage] = useState<RegistryUsage | null>(null);

  const base = own ? `/orgs/${org}/registry` : `/orgs/${org}/registries/${id}`;

  const load = useCallback(() => {
    if (!org) return;
    setError(null);
    setUnsupported(false);
    api
      .get<RegistryRepository[]>(`${base}/repositories`)
      .then(setRepos)
      .catch((e) => {
        // 501 is the registry's own answer — Docker Hub and GitHub's
        // disable the catalogue the Registry v2 API defines — and is
        // not a failure to report as one.
        if (e?.status === 501) {
          setUnsupported(true);
          setRepos([]);
          return;
        }
        setError(message(e));
      });
  }, [base, org]);
  useEffect(load, [load]);

  // Measured after the list is on screen, never before it: it is one
  // call per repository at the far end, and there can be hundreds.
  useEffect(() => {
    if (!org || own || !repos || repos.length === 0) return;
    api
      .get<RegistryUsage>(`${base}/usage`)
      .then(setUsage)
      .catch(() => setUsage(null));
  }, [base, org, own, repos]);

  // refresh re-reads the tags of repositories whose contents changed.
  // A row that is open has to be re-read here, because nothing else
  // will: expanding is what fetches, and it has already happened.
  function refresh(names: Set<string>) {
    setImages((prev) => {
      const next = { ...prev };
      for (const name of names) delete next[name];
      return next;
    });
    for (const name of names) {
      if (!open.has(name)) continue;
      api
        .get<RegistryImage[]>(`${base}/images?repository=${encodeURIComponent(name)}`)
        .then((list) => setImages((prev) => ({ ...prev, [name]: list })))
        .catch((e) => setError(message(e)));
    }
  }

  function toggle(name: string) {
    setOpen((prev) => {
      const next = new Set(prev);
      if (!next.delete(name)) next.add(name);
      return next;
    });
    if (images[name]) return;
    api
      .get<RegistryImage[]>(`${base}/images?repository=${encodeURIComponent(name)}`)
      .then((list) => setImages((prev) => ({ ...prev, [name]: list })))
      .catch((e) => setError(message(e)));
  }

  // The mark belongs to the provider, and the detail page knows which
  // one only from what the listing passed it.
  const provider = credential?.provider ?? "";
  const Icon =
    provider === "aws" ? AWSIcon : provider === "digitalocean" ? DigitalOceanIcon : BoxIcon;

  // Cubeship's own registry has no host in the query string — it is
  // instance configuration, and it is empty until there is a domain.
  // Selection is by name, not by index: the list is filtered and
  // reloaded under it, and a position stops meaning the same row the
  // moment either happens.
  const [pickedRepos, setPickedRepos] = useState<Set<string>>(new Set());
  // A tag is only identified by its repository *and* its tag, so the
  // key carries both. The separator is a character neither can hold.
  const [pickedTags, setPickedTags] = useState<Set<string>>(new Set());
  const [bulk, setBulk] = useState<"repos" | "tags" | null>(null);

  const [deletingRepo, setDeletingRepo] = useState<string | null>(null);
  // `ref` is a tag, or "@<digest>" for an image that has none.
  const [deletingTag, setDeletingTag] = useState<{ repo: string; ref: string } | null>(null);
  const [ownHost, setOwnHost] = useState("");

  useEffect(() => {
    if (!own) return;
    api
      .get<Settings>("/settings")
      .then((s) => setOwnHost(s.registry_host ?? ""))
      .catch(() => setOwnHost(""));
  }, [own]);
  const registryHost = own ? ownHost : host;

  const byRepo = Object.fromEntries((usage?.repositories ?? []).map((r) => [r.name, r])) as Record<
    string,
    { bytes: number; images: number } | undefined
  >;

  const filtered = repos
    ? repos.filter((r) => r.name.toLowerCase().includes(query.trim().toLowerCase()))
    : [];

  // A ticked repository means every tag in it, and the tag boxes read
  // that rather than being ticked one by one. Nothing is fetched to
  // decide it — ticking a repository whose row was never expanded, or
  // ticking two hundred of them from the header box, would otherwise be
  // two hundred requests to someone else's registry on one click.
  const tagPicked = (repo: string, ref: string) =>
    pickedRepos.has(repo) || pickedTags.has(tagKey(repo, ref));

  function setRepoPicked(name: string, on: boolean) {
    setPickedRepos((prev) => {
      const next = new Set(prev);
      if (on) next.add(name);
      else next.delete(name);
      return next;
    });
    // Its tags were carried by it, so they go when it does.
    if (!on) setPickedTags((prev) => withoutRepo(prev, name));
  }

  // Unticking one tag inside a ticked repository unticks the
  // repository — it is no longer "all of this" — and writes down the
  // rest explicitly, so the other tags stay ticked. The list is at hand
  // because you can only untick a tag you can see.
  function toggleTag(repo: string, ref: string, siblings: RegistryImage[]) {
    if (pickedRepos.has(repo)) {
      setRepoPicked(repo, false);
      setPickedTags((prev) => {
        const next = new Set(prev);
        for (const image of siblings) {
          if (imageRef(image) !== ref) next.add(tagKey(repo, imageRef(image)));
        }
        return next;
      });
      return;
    }
    setPickedTags((prev) => {
      const next = new Set(prev);
      const key = tagKey(repo, ref);
      if (!next.delete(key)) next.add(key);
      return next;
    });
  }

  // The header box acts on what is on screen, not on everything the
  // registry holds: a filter that narrowed the list to four rows and a
  // box that then selected two hundred would be a trap.
  const allShownPicked = filtered.length > 0 && filtered.every((r) => pickedRepos.has(r.name));

  // A ticked repository already carries its tags, so those tags are not
  // a second thing to delete and not a second thing to count. Only tags
  // outside a ticked repository stand on their own.
  const looseTags = [...pickedTags].filter(
    (key) => !pickedRepos.has(key.split(TAG_KEY_SEPARATOR)[0]),
  );

  // Deletes run one after another rather than at once. Each is a call
  // to someone else's registry, and a burst of them is how you get rate
  // limited half way through a set — which leaves the worst outcome, a
  // selection partly gone with no record of which half.
  async function deletePicked(kind: "repos" | "tags") {
    setError(null);
    const failed: string[] = [];

    if (kind === "repos") {
      const gone: string[] = [];
      for (const name of pickedRepos) {
        try {
          await api.del(`${base}/repositories?repository=${encodeURIComponent(name)}`);
          gone.push(name);
        } catch {
          failed.push(name);
        }
      }
      setPickedRepos(new Set(failed));
      // Their tags went with them, so they stop being selected too.
      setPickedTags((prev) => gone.reduce(withoutRepo, prev));
      setOpen((prev) => {
        const next = new Set(prev);
        for (const name of gone) next.delete(name);
        return next;
      });
      setImages((prev) => {
        const next = { ...prev };
        for (const name of gone) delete next[name];
        return next;
      });
    } else {
      for (const key of looseTags) {
        const [repo, ref] = key.split(TAG_KEY_SEPARATOR);
        try {
          await api.del(`${base}/images?repository=${encodeURIComponent(repo)}&${refQuery(ref)}`);
        } catch {
          failed.push(key);
        }
      }
      setPickedTags((prev) => {
        const next = new Set(prev);
        for (const key of looseTags) next.delete(key);
        for (const key of failed) next.add(key);
        return next;
      });
      // Only the repositories that were touched are stale. Clearing the
      // whole cache left every expanded row reading "Reading tags…"
      // forever: nothing refetches a row that is already open.
      refresh(new Set(looseTags.map((key) => key.split(TAG_KEY_SEPARATOR)[0])));
    }

    if (failed.length > 0) {
      setError(
        `${failed.length} of them could not be deleted, and are still selected: ${failed
          .map((f) => f.replace(TAG_KEY_SEPARATOR, f.includes(`${TAG_KEY_SEPARATOR}@`) ? "" : ":"))
          .join(", ")}`,
      );
    }
    setBulk(null);
    load();
  }

  return (
    <>
      <Link
        href="/registries"
        className="mb-4 inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
      >
        <ChevronLeftIcon className="size-3.5" />
        Registries
      </Link>

      <PageHeader
        title={own ? "Cubeship registry" : host}
        literal={!own}
        icon={<Icon className="size-5 shrink-0 text-muted-foreground" />}
        actions={
          !own && (
            <Button
              variant="outline"
              nativeButton={false}
              render={
                <Link href={`/registries/${id}/settings`}>
                  <SettingsIcon />
                  Settings
                </Link>
              }
            />
          )
        }
        sub={
          own
            ? "What this organization has pushed. A repository path here is an app's reference."
            : "What this registry holds, as it reports it."
        }
        below={
          repos &&
          repos.length > 0 && (
            // Above the separator, because it filters the page rather
            // than the table: what it hides is gone from everything
            // below.
            //
            // One surface, and the focus ring on it rather than on the
            // field: the mark and the count are part of the same
            // control, and a ring around only the middle of it looks
            // like a mistake.
            <SearchBar
              value={query}
              onChange={setQuery}
              placeholder="Filter repositories"
              trailing={
                <span className="shrink-0 font-mono text-[11px] text-muted-foreground">
                  {filtered.length}/{repos.length}
                  {usage && (
                    <span title="Layers shared between images are counted once per image, so this is an upper bound.">
                      {" · ~"}
                      {bytes(usage.total_bytes)}
                    </span>
                  )}
                </span>
              }
            />
          )
        }
      />

      <ErrorAlert error={error} />

      {/* The catalogue is a live call to someone else's registry, so
          this is a wait worth naming rather than an empty page. */}
      {repos === null && !error && !unsupported && (
        <div>
          <LoadingList rows={5} />
          <LoadingNote>Asking the registry what it holds</LoadingNote>
        </div>
      )}

      {unsupported && (
        <Notice>
          This registry does not list what it holds. Docker Hub and GitHub&apos;s disable the
          catalogue the Registry v2 API defines — that is their answer, not a failure here.
        </Notice>
      )}

      {repos?.length === 0 && !unsupported && (
        <Card>
          <CardContent className="py-2 text-sm text-muted-foreground">
            Nothing pushed yet.
          </CardContent>
        </Card>
      )}

      {/* One dialog for both, because the question is the same shape
          and the count is what makes it dangerous. Typing "delete" is
          the guard rather than a name — there is no one name to type. */}
      <ConfirmDialog
        open={bulk !== null}
        onOpenChange={(open) => !open && setBulk(null)}
        title={
          bulk === "repos"
            ? `Delete ${pickedRepos.size} ${pickedRepos.size === 1 ? "repository" : "repositories"}?`
            : `Delete ${looseTags.length} ${looseTags.length === 1 ? "tag" : "tags"}?`
        }
        // The count is the warning, not the list. Naming every one of
        // them ran off the screen at the sizes this is actually used
        // at, and a wall of references is not something anyone reads
        // before typing the confirmation anyway.
        description={
          bulk === "repos"
            ? "Every tag in each of them goes. Apps pulling from any of them keep running — a container already exists — and their next deploy fails."
            : "Anything pinned to one of these stops being able to pull."
        }
        confirmWord={bulk === "repos" ? "delete my repositories" : "delete my tags"}
        onConfirm={() => deletePicked(bulk ?? "repos")}
      />

      <ConfirmDialog
        open={deletingRepo !== null}
        onOpenChange={(open) => !open && setDeletingRepo(null)}
        title={`Delete ${deletingRepo}?`}
        description={
          <>
            Every tag in it goes. Apps pulling from it keep running — a container already exists —
            and their next deploy fails.
          </>
        }
        confirmWord={deletingRepo ?? undefined}
        onConfirm={async () => {
          await api.del(
            `${base}/repositories?repository=${encodeURIComponent(deletingRepo ?? "")}`,
          );
          setDeletingRepo(null);
          load();
        }}
      />

      <ConfirmDialog
        open={deletingTag !== null}
        onOpenChange={(open) => !open && setDeletingTag(null)}
        title={
          deletingTag?.ref.startsWith("@")
            ? "Delete this untagged image?"
            : `Delete ${deletingTag?.ref}?`
        }
        description={
          <>
            Only this image, out of <code>{deletingTag?.repo}</code>. Anything pinned to it stops
            being able to pull.
          </>
        }
        onConfirm={async () => {
          if (!deletingTag) return;
          await api.del(
            `${base}/images?repository=${encodeURIComponent(deletingTag.repo)}&${refQuery(deletingTag.ref)}`,
          );
          setImages((prev) => {
            const next = { ...prev };
            delete next[deletingTag.repo];
            return next;
          });
          setDeletingTag(null);
        }}
      />

      {repos && repos.length > 0 && filtered.length === 0 && (
        <Card>
          <CardContent className="py-2 text-sm text-muted-foreground">
            Nothing matches “{query}”.
          </CardContent>
        </Card>
      )}

      {(pickedRepos.size > 0 || looseTags.length > 0) && !own && (
        <SelectionBar
          repos={pickedRepos.size}
          tags={looseTags.length}
          onClear={() => {
            setPickedRepos(new Set());
            setPickedTags(new Set());
          }}
          onDeleteRepos={() => setBulk("repos")}
          onDeleteTags={() => setBulk("tags")}
        />
      )}

      {filtered.length > 0 && (
        <Card className="py-0">
          <Table>
            <TableHeader>
              <TableRow>
                {!own && (
                  <TableHead className="w-10 px-4">
                    <Checkbox
                      aria-label="Select every repository shown"
                      checked={allShownPicked}
                      indeterminate={
                        !allShownPicked && filtered.some((r) => pickedRepos.has(r.name))
                      }
                      onCheckedChange={(on) => {
                        for (const r of filtered) setRepoPicked(r.name, Boolean(on));
                      }}
                    />
                  </TableHead>
                )}
                <TableHead className="px-4">Repository</TableHead>
                <TableHead className="px-4" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {filtered.map((repo) => (
                <RepoRows
                  key={repo.name}
                  name={repo.name}
                  host={registryHost}
                  usage={byRepo[repo.name]}
                  onDelete={own ? undefined : () => setDeletingRepo(repo.name)}
                  onDeleteTag={own ? undefined : (ref) => setDeletingTag({ repo: repo.name, ref })}
                  open={open.has(repo.name)}
                  images={images[repo.name]}
                  onToggle={() => toggle(repo.name)}
                  selectable={!own}
                  picked={pickedRepos.has(repo.name)}
                  onPick={() => setRepoPicked(repo.name, !pickedRepos.has(repo.name))}
                  tagPicked={tagPicked}
                  onPickTag={toggleTag}
                />
              ))}
            </TableBody>
          </Table>
        </Card>
      )}
    </>
  );
}

// withoutRepo drops every tag belonging to one repository.
function withoutRepo(keys: Set<string>, repo: string): Set<string> {
  const next = new Set<string>();
  for (const key of keys) {
    if (!key.startsWith(repo + TAG_KEY_SEPARATOR)) next.add(key);
  }
  return next;
}

// An image is identified by its repository and, inside it, by its tag —
// or by its digest where it has no tag. Two untagged images would share
// any stand-in name, so selecting one would select both and deleting
// either would be a request the registry cannot answer.
//
// The separator is a character neither a repository path, a tag nor a
// digest can contain, so no pair of real names collides with another
// pair's key.
const TAG_KEY_SEPARATOR = "\u0000";

// imageRef is how one image is named inside its repository: its tag, or
// "@<digest>" when it has none. It is what the key is built from and
// what the delete sends.
function imageRef(image: RegistryImage): string {
  return image.tag || `@${image.digest ?? ""}`;
}

function tagKey(repo: string, ref: string) {
  return repo + TAG_KEY_SEPARATOR + ref;
}

// The query a delete carries for one image.
function refQuery(ref: string): string {
  return ref.startsWith("@")
    ? `digest=${encodeURIComponent(ref.slice(1))}`
    : `tag=${encodeURIComponent(ref)}`;
}

// What an image is called on screen. An untagged one has no name, and
// saying so is better than inventing one.
function imageLabel(image: RegistryImage): string {
  return image.tag || "<untagged>";
}

// What sits above the table once anything is selected. It names the
// count rather than showing a bare Delete, because the count is the
// whole difference between this and the button on a row.
function SelectionBar({
  repos,
  tags,
  onClear,
  onDeleteRepos,
  onDeleteTags,
}: {
  repos: number;
  tags: number;
  onClear: () => void;
  onDeleteRepos: () => void;
  onDeleteTags: () => void;
}) {
  return (
    <div className="mb-3 flex flex-wrap items-center gap-3 border border-primary/30 bg-primary/5 px-4 py-2.5">
      <span className="font-mono text-[11px] tracking-[0.12em] text-primary uppercase">
        {repos > 0 && `${repos} ${repos === 1 ? "repository" : "repositories"}`}
        {repos > 0 && tags > 0 && " · "}
        {tags > 0 && `${tags} ${tags === 1 ? "tag" : "tags"}`}
        {" selected"}
      </span>
      <span className="flex-1" />
      {tags > 0 && (
        <Button variant="outline" size="sm" onClick={onDeleteTags}>
          <Trash2Icon />
          Delete tags
        </Button>
      )}
      {repos > 0 && (
        <Button variant="destructive" size="sm" onClick={onDeleteRepos}>
          <Trash2Icon />
          Delete repositories
        </Button>
      )}
      <Button variant="ghost" size="sm" onClick={onClear}>
        Clear
      </Button>
    </div>
  );
}

// bytes renders a size the way someone reads one, which is two
// significant figures and a unit — not a byte count.
function bytes(n: number): string {
  if (n < 1024) return `${n} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let value = n / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  return `${value >= 10 ? value.toFixed(0) : value.toFixed(1)} ${units[unit]}`;
}

function RepoRows({
  name,
  host,
  open,
  images,
  usage,
  onToggle,
  onDelete,
  onDeleteTag,
  selectable,
  picked,
  onPick,
  tagPicked,
  onPickTag,
}: {
  name: string;
  // The registry's address, so a row can offer the whole reference
  // rather than the half of it that is on screen.
  host: string;
  open: boolean;
  images: RegistryImage[] | undefined;
  usage?: { bytes: number; images: number };
  onToggle: () => void;
  // Absent where the registry cannot delete, which is what removes the
  // button rather than a button that fails.
  onDelete?: () => void;
  onDeleteTag?: (ref: string) => void;
  // Absent on Cubeship's own registry, which nothing here deletes from.
  selectable: boolean;
  picked: boolean;
  onPick: () => void;
  tagPicked: (repo: string, ref: string) => boolean;
  onPickTag: (repo: string, ref: string, siblings: RegistryImage[]) => void;
}) {
  return (
    <>
      {/* select-none: dragging across a row to click it should not
          leave half the page highlighted. Every row here is a button in
          all but name. */}
      <TableRow className="cursor-pointer select-none" onClick={onToggle}>
        {selectable && (
          // The row expands on click and the box must not, so the cell
          // swallows the event rather than the box alone: the box's own
          // hit area is deliberately larger than the box.
          <TableCell
            className="w-10 px-4 py-2.5"
            onClick={(e) => e.stopPropagation()}
            onKeyDown={(e) => e.stopPropagation()}
          >
            <Checkbox aria-label={`Select ${name}`} checked={picked} onCheckedChange={onPick} />
          </TableCell>
        )}
        <TableCell className="px-4 py-2.5 font-mono text-xs leading-6">{name}</TableCell>
        {/* A flex row, not three things in a line of text: a count, a
            ghost button and an inline icon each sit on their own
            baseline, and the button's line-height is not the span's.
            One row, centred, with gaps instead of margins. */}
        <TableCell className="px-4 py-2.5 whitespace-nowrap">
          <div className="flex items-center justify-end gap-2">
            {usage && (
              <span className="font-mono text-[11px] leading-none text-muted-foreground">
                {usage.images} · {bytes(usage.bytes)}
              </span>
            )}
            {onDelete && (
              <Button
                variant="ghost"
                size="xs"
                className="size-6 p-0 text-muted-foreground hover:text-destructive"
                onClick={(e) => {
                  e.stopPropagation();
                  onDelete();
                }}
              >
                <Trash2Icon className="size-3.5" />
              </Button>
            )}
            <ChevronRightIcon
              className={`size-3.5 shrink-0 text-muted-foreground transition-transform ${
                open ? "rotate-90" : ""
              }`}
            />
          </div>
        </TableCell>
      </TableRow>

      {open && (
        <TableRow className="hover:bg-transparent">
          <TableCell colSpan={selectable ? 3 : 2} className="px-4 py-0">
            {images === undefined && (
              <p className="py-3 text-xs text-muted-foreground">Reading tags…</p>
            )}
            {images?.length === 0 && <p className="py-3 text-xs text-muted-foreground">No tags.</p>}
            {images && images.length > 0 && (
              // One per line, in columns. Wrapped across the width, a
              // tag was something you had to find the start of, and the
              // sizes and dates never lined up with each other — which
              // is the only way those two mean anything.
              <ul className="py-2">
                {images.map((image) => (
                  <li
                    key={tagKey(name, imageRef(image))}
                    className="flex items-center gap-4 py-1 font-mono text-xs select-none"
                  >
                    {selectable && (
                      <Checkbox
                        aria-label={`Select ${name}:${imageLabel(image)}`}
                        className="shrink-0"
                        checked={tagPicked(name, imageRef(image))}
                        onCheckedChange={() => onPickTag(name, imageRef(image), images)}
                      />
                    )}
                    <span
                      className={`min-w-0 flex-1 truncate ${image.tag ? "" : "text-subtle-foreground"}`}
                    >
                      {imageLabel(image)}
                    </span>
                    <span className="w-20 shrink-0 text-right text-muted-foreground">
                      {image.size ? `${(image.size / 1_048_576).toFixed(0)} MB` : ""}
                    </span>
                    <span className="w-24 shrink-0 text-right text-muted-foreground">
                      {image.pushed_at ? new Date(image.pushed_at).toLocaleDateString() : ""}
                    </span>
                    {/* The whole reference, which is what you paste
                        somewhere else — the tag on its own is not
                        something anything can pull. */}
                    <CopyButton
                      value={host ? `${host}/${name}:${image.tag}` : `${name}:${image.tag}`}
                      label={`Copy ${name}:${image.tag}`}
                      className="size-6 shrink-0 p-0"
                    />
                    {onDeleteTag && (
                      <Button
                        variant="ghost"
                        size="xs"
                        aria-label={`Delete ${imageLabel(image)}`}
                        className="size-6 shrink-0 p-0 text-muted-foreground hover:text-destructive"
                        onClick={() => onDeleteTag(imageRef(image))}
                      >
                        <Trash2Icon className="size-3.5" />
                      </Button>
                    )}
                  </li>
                ))}
              </ul>
            )}
          </TableCell>
        </TableRow>
      )}
    </>
  );
}
