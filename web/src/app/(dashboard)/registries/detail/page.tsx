"use client";

import { BoxIcon, ChevronLeftIcon, ChevronRightIcon, SettingsIcon, Trash2Icon } from "lucide-react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useCallback, useEffect, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { CopyButton } from "@/components/copy-button";
import { ErrorAlert } from "@/components/error-alert";
import { AWSIcon, DigitalOceanIcon } from "@/components/icons";
import { Notice } from "@/components/notice";
import { useOrg } from "@/components/org-context";
import { PageHeader } from "@/components/page-header";
import { SearchBar } from "@/components/search-bar";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
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
export default function RegistryDetail() {
  return (
    <Suspense>
      <Detail />
    </Suspense>
  );
}

function Detail() {
  const params = useSearchParams();
  const _router = useRouter();
  const { org } = useOrg();
  const id = params.get("id") ?? "";
  const host = params.get("host") ?? "";
  const own = id === "" || id === "cubeship";

  const [repos, setRepos] = useState<RegistryRepository[] | null>(null);
  const [open, setOpen] = useState<string | null>(null);
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

  function toggle(name: string) {
    if (open === name) {
      setOpen(null);
      return;
    }
    setOpen(name);
    if (images[name]) return;
    api
      .get<RegistryImage[]>(`${base}/images?repository=${encodeURIComponent(name)}`)
      .then((list) => setImages((prev) => ({ ...prev, [name]: list })))
      .catch((e) => setError(message(e)));
  }

  // The mark belongs to the provider, and the detail page knows which
  // one only from what the listing passed it.
  const provider = params.get("provider") ?? "";
  const Icon =
    provider === "aws" ? AWSIcon : provider === "digitalocean" ? DigitalOceanIcon : BoxIcon;

  // Cubeship's own registry has no host in the query string — it is
  // instance configuration, and it is empty until there is a domain.
  const [deletingRepo, setDeletingRepo] = useState<string | null>(null);
  const [deletingTag, setDeletingTag] = useState<{ repo: string; tag: string } | null>(null);
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
                <Link href={`/registries/settings?id=${id}&host=${encodeURIComponent(host)}`}>
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
        title={`Delete ${deletingTag?.tag}?`}
        description={
          <>
            Only this tag, out of <code>{deletingTag?.repo}</code>. Anything pinned to it stops
            being able to pull.
          </>
        }
        onConfirm={async () => {
          if (!deletingTag) return;
          await api.del(
            `${base}/images?repository=${encodeURIComponent(deletingTag.repo)}&tag=${encodeURIComponent(deletingTag.tag)}`,
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

      {filtered.length > 0 && (
        <Card className="py-0">
          <Table>
            <TableHeader>
              <TableRow>
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
                  onDeleteTag={own ? undefined : (tag) => setDeletingTag({ repo: repo.name, tag })}
                  open={open === repo.name}
                  images={images[repo.name]}
                  onToggle={() => toggle(repo.name)}
                />
              ))}
            </TableBody>
          </Table>
        </Card>
      )}
    </>
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
  onDeleteTag?: (tag: string) => void;
}) {
  return (
    <>
      <TableRow className="cursor-pointer" onClick={onToggle}>
        <TableCell className="px-4 py-2.5 font-mono text-xs">{name}</TableCell>
        <TableCell className="px-4 py-2.5 text-right whitespace-nowrap">
          {usage && (
            <span className="mr-3 font-mono text-[11px] text-muted-foreground">
              {usage.images} · {bytes(usage.bytes)}
            </span>
          )}
          {onDelete && (
            <Button
              variant="ghost"
              size="xs"
              className="mr-1 text-muted-foreground hover:text-destructive"
              onClick={(e) => {
                e.stopPropagation();
                onDelete();
              }}
            >
              <Trash2Icon className="size-3.5" />
            </Button>
          )}
          <ChevronRightIcon
            className={`inline size-3.5 text-muted-foreground transition-transform ${
              open ? "rotate-90" : ""
            }`}
          />
        </TableCell>
      </TableRow>

      {open && (
        <TableRow className="hover:bg-transparent">
          <TableCell colSpan={2} className="px-4 py-0">
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
                    key={`${image.tag}-${image.digest ?? ""}`}
                    className="flex items-center gap-4 py-0.5 font-mono text-xs"
                  >
                    <span className="min-w-0 flex-1 truncate">{image.tag}</span>
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
                    />
                    {onDeleteTag && (
                      <Button
                        variant="ghost"
                        size="xs"
                        aria-label={`Delete ${image.tag}`}
                        className="text-muted-foreground hover:text-destructive"
                        onClick={() => onDeleteTag(image.tag)}
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
