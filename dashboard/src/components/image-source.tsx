"use client";

import { useQuery } from "@tanstack/react-query";
import { type ReactNode, useEffect, useId } from "react";
import { SearchableSelect } from "@/components/searchable-select";
import { TextField } from "@/components/text-field";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import type { RegistryCredential } from "@/lib/api";
import {
  appTagsKey,
  buildDataStaleTime,
  imageTagsKey,
  loadAppTags,
  loadImageTags,
  loadRegistryCredentials,
  loadRegistryRepositories,
  registryCredentialsKey,
  registryRepositoriesKey,
} from "@/lib/build-data";
import { selectableImageTags } from "@/lib/image-tags";

// Where a published image comes from: which registry, which image in it,
// and which tag of that image.
//
// It used to be a pair of cards — Cubeship's registry or another one —
// and a text field you typed a reference into. The pair was the model
// showing through rather than a choice anybody has: "another registry"
// is not one thing, it is however many this instance is connected to,
// and picking it told you nothing about which. What somebody actually
// decides is the registry, then the image, then the tag, and every one
// of those is something this instance can list.
//
// The asymmetry is real and is not hidden: **on Cubeship's own registry
// there is no image to pick.** It is this app's own path — a push there
// is what deploys it — and pointing an app at another app's path would
// break the webhook, which matches a pushed repository to an app by its
// reference and nothing else.

// CUBESHIP is the reserved key for the registry this instance runs, the
// same word its id takes everywhere else.
export const CUBESHIP = "cubeship";

// DOCKER_HUB is where an image with no registry in its name comes from,
// and it is offered whether or not anything is connected: a public image
// needs no login, which is the one thing a fresh install can run.
const DOCKER_HUB = "index.docker.io";

export type ImageSourceValue = {
  // Registry is CUBESHIP, or the host of the registry to pull from.
  registry: string;
  // Image is the full reference without a tag, empty on Cubeship's own.
  image: string;
  // Tag is empty when the app follows the registry: on Cubeship's own
  // that is autodeploy, and anywhere else it is `latest`.
  tag: string;
};

export function ImageSource({
  sourceControl,
  value,
  onChange,
  pushPath,
  appReference,
}: {
  sourceControl: ReactNode;
  value: ImageSourceValue;
  onChange: (next: ImageSourceValue) => void;
  // Where a push to this instance's registry goes, which is what an app
  // on it runs. Empty while the instance has no domain — there is
  // nowhere to push yet, and saying so beats an empty field.
  pushPath?: string;
  // The app's own path inside this instance's registry, which is what a
  // tag listing there is asked for.
  appReference: string;
}) {
  const registriesQuery = useQuery({
    queryKey: registryCredentialsKey,
    queryFn: loadRegistryCredentials,
    staleTime: buildDataStaleTime,
  });

  const onCubeship = value.registry === CUBESHIP;
  const connected = registriesQuery.data ?? [];
  const picked = connected.find((r) => r.host === value.registry);

  // Docker Hub is in the list once: as the connected account when there
  // is one, and as itself when there is not.
  const choices = [
    { value: CUBESHIP, label: "Cubeship's registry" },
    ...connected.map((r) => ({ value: r.host, label: registryLabel(r) })),
    ...(connected.some((r) => r.host === DOCKER_HUB)
      ? []
      : [{ value: DOCKER_HUB, label: "Docker Hub" }]),
  ];

  return (
    <div className="space-y-5">
      <div className="grid grid-cols-1 items-start gap-4 sm:grid-cols-2">
        {sourceControl}
        <SearchableSelect
          label="Registry"
          choices={choices}
          value={value.registry}
          busy={registriesQuery.isPending}
          onChange={(registry) =>
            // The image and the tag belong to the registry that was left,
            // so neither survives the move. Carrying them over is how a
            // field ends up naming something the new registry has never
            // heard of.
            onChange({ registry, image: "", tag: "" })
          }
        />
      </div>

      {onCubeship ? (
        <TextField
          label="Image"
          value={pushPath || "this instance has no domain yet"}
          readOnly
          disabled
          spellCheck={false}
          onChange={() => {}}
        />
      ) : (
        <ImageField
          registryID={picked?.id}
          host={value.registry}
          value={value.image}
          onChange={(image) => onChange({ ...value, image, tag: "" })}
        />
      )}

      {onCubeship && (
        <div className="flex items-start gap-3">
          <Switch
            id="autodeploy"
            checked={value.tag === ""}
            onCheckedChange={(on) => onChange({ ...value, tag: on ? "" : "latest" })}
          />
          <div className="space-y-1">
            <Label htmlFor="autodeploy">Deploy on push</Label>
            <p className="max-w-prose text-xs text-muted-foreground">
              Every push to this path deploys the app, whichever tag it carries. Turn it off to pin
              a tag and deploy when you decide to — the two cannot both be true, so choosing a tag
              is what turns this off.
            </p>
          </div>
        </div>
      )}

      {!(onCubeship && value.tag === "") && (
        <TagField
          image={onCubeship ? "" : value.image}
          repository={onCubeship ? appReference : ""}
          value={value.tag}
          onChange={(tag) => onChange({ ...value, tag })}
        />
      )}
    </div>
  );
}

function registryLabel(r: RegistryCredential): string {
  if (r.host === DOCKER_HUB) return "Docker Hub";
  return r.namespace ? `${r.host}/${r.namespace}` : r.host;
}

// ImageField is a list when the registry will produce one and a field
// when it will not.
//
// Docker Hub is the case that decides the shape: it has no public
// catalogue, so there is nothing to list and never will be. A registry
// that refuses the catalogue answers 501, which is the same answer for
// the same reason — and both are ordinary rather than broken, so
// neither is an error on the screen.
function ImageField({
  registryID,
  host,
  value,
  onChange,
}: {
  registryID?: number;
  host: string;
  value: string;
  onChange: (image: string) => void;
}) {
  const suggestionsID = useId();
  const repositoriesQuery = useQuery({
    queryKey: registryRepositoriesKey(registryID ?? 0),
    queryFn: () => loadRegistryRepositories(registryID ?? 0),
    enabled: registryID !== undefined,
    staleTime: buildDataStaleTime,
  });
  const repos = repositoriesQuery.data ?? [];
  const listable =
    registryID !== undefined &&
    !repositoriesQuery.isError &&
    (repositoriesQuery.isPending || repos.length > 0);
  const full = (name: string) => (host === DOCKER_HUB ? name : `${host}/${name}`);

  if (host === "ghcr.io") {
    return (
      <>
        <TextField
          label="Image"
          value={value}
          spellCheck={false}
          list={suggestionsID}
          onChange={(e) => onChange(e.target.value)}
          placeholder="ghcr.io/owner/image"
          hint="Suggestions show packages this login can access. Type an exact public image path to use another GHCR package."
        />
        <datalist id={suggestionsID}>
          {repos.map((repo) => (
            <option key={repo.name} value={full(repo.name)} />
          ))}
        </datalist>
      </>
    );
  }

  if (!listable) {
    return (
      <TextField
        label="Image"
        value={value}
        spellCheck={false}
        onChange={(e) => onChange(e.target.value)}
        placeholder={host === DOCKER_HUB ? "nginx" : `${host}/acme/api`}
      />
    );
  }

  return (
    <SearchableSelect
      label="Image"
      choices={repos.map((r) => ({ value: full(r.name), label: r.name }))}
      value={value}
      busy={repositoriesQuery.isPending}
      empty="This registry holds nothing yet."
      onChange={onChange}
    />
  );
}

// TagField lists what the image can be deployed at, newest image date
// first, and chooses the first for you.
//
// Chosen rather than merely offered, because an empty tag is not a
// neutral state here: on any registry but this instance's own it means
// `latest`, which is a tag that may not exist and is in any case a
// different decision from the one somebody is in the middle of making.
function TagField({
  image,
  repository,
  value,
  onChange,
}: {
  // One of the two: an image somewhere else, or a repository in this
  // instance's own registry.
  image: string;
  repository: string;
  value: string;
  onChange: (tag: string) => void;
}) {
  const tagsQuery = useQuery({
    queryKey: image ? imageTagsKey(image) : appTagsKey(repository),
    queryFn: () => (image ? loadImageTags(image) : loadAppTags(repository)),
    enabled: !!image || !!repository,
    staleTime: buildDataStaleTime,
  });
  const tags = tagsQuery.data ? selectableImageTags(tagsQuery.data) : null;
  const listable = (!!image || !!repository) && (tagsQuery.isPending || (tags?.length ?? 0) > 0);

  useEffect(() => {
    if (tags?.length && value === "") onChange(tags[0].tag);
  }, [onChange, tags, value]);

  if (!listable) {
    return (
      <TextField
        label="Tag"
        value={value}
        spellCheck={false}
        onChange={(e) => onChange(e.target.value)}
        placeholder="latest"
        hint={
          image || repository
            ? "Nothing to list there yet — type the tag this app should run."
            : "Choose an image first."
        }
      />
    );
  }

  return (
    <SearchableSelect
      label="Tag"
      choices={(tags ?? []).map((t) => ({ value: t.tag, label: t.tag }))}
      value={value}
      placeholder={value || "Select a tag"}
      busy={tagsQuery.isPending}
      onChange={onChange}
      hint="Newest image first by publication or build date. This is the one the app runs until you change it."
    />
  );
}
