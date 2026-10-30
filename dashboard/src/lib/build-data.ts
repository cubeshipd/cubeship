import {
  ApiError,
  api,
  type GitBranch,
  type GitRepository,
  type RegistryCredential,
  type RegistryImage,
  type RegistryRepository,
} from "@/lib/api";
import { message } from "@/lib/errors";

export type GitRepositoryChoice = GitRepository & {
  provider: "github" | "bitbucket" | "gitlab" | "gitea";
  url: string;
};

export const buildDataStaleTime = 60_000;
export const gitRepositoriesKey = ["git-source-repositories"] as const;
export const registryCredentialsKey = ["registry-credentials"] as const;
export const registryRepositoriesKey = (id: number) => ["registry-repositories", id] as const;
export const imageTagsKey = (image: string) => ["registry-image-tags", image] as const;
export const appTagsKey = (repository: string) => ["app-image-tags", repository] as const;
export const gitBranchesKey = (provider: string, repo: string) =>
  ["git-branches", provider, repo] as const;

export async function loadGitRepositories(): Promise<{
  repositories: GitRepositoryChoice[];
  error: string | null;
}> {
  const [github, bitbucket, remotes] = await Promise.allSettled([
    api.get<GitRepository[]>("/github/repositories"),
    api.get<GitRepository[]>("/bitbucket/repositories"),
    api.get<GitRepository[]>("/gitremotes/repositories"),
  ]);
  const from = (result: PromiseSettledResult<GitRepository[]>, provider: "github" | "bitbucket") =>
    result.status === "fulfilled"
      ? result.value.map((entry) => ({
          ...entry,
          provider,
          url: `https://${provider === "github" ? "github.com" : "bitbucket.org"}/${entry.full_name}`,
        }))
      : [];
  const remoteRepos =
    remotes.status === "fulfilled"
      ? remotes.value.filter(
          (entry): entry is GitRepositoryChoice =>
            !!entry.url && (entry.provider === "gitlab" || entry.provider === "gitea"),
        )
      : [];
  const failure = [
    github.status === "rejected" &&
    (!(github.reason instanceof ApiError) || github.reason.status !== 409)
      ? github.reason
      : null,
    bitbucket.status === "rejected" ? bitbucket.reason : null,
    remotes.status === "rejected" ? remotes.reason : null,
  ].find(Boolean);
  return {
    repositories: [...from(github, "github"), ...from(bitbucket, "bitbucket"), ...remoteRepos],
    error: failure ? message(failure) : null,
  };
}

export function gitBranchRequest(repo: string) {
  const match = repo.match(
    /^https?:\/\/(?:www\.)?(github\.com|bitbucket\.org)\/(.+?)(?:\.git)?\/?$/i,
  );
  const provider = match?.[1]?.toLowerCase() === "bitbucket.org" ? "bitbucket" : "github";
  const fullName = match?.[2] ?? "";
  const url = repo.replace(/\.git\/?$/i, "").replace(/\/$/, "");
  if (!match && !repo.startsWith("http")) return null;
  return {
    key: gitBranchesKey(provider, match ? fullName : url),
    load: () =>
      api.get<GitBranch[]>(
        match
          ? `/${provider}/branches?repo=${encodeURIComponent(fullName)}`
          : `/gitremotes/branches?repo=${encodeURIComponent(url)}`,
      ),
  };
}

export const loadRegistryCredentials = () => api.get<RegistryCredential[]>("/registries");
export const loadRegistryRepositories = (id: number) =>
  api.get<RegistryRepository[]>(`/registries/${id}/repositories`);
export const loadImageTags = (image: string) =>
  api.get<RegistryImage[]>(`/registries/tags?image=${encodeURIComponent(image)}`);
export const loadAppTags = (repository: string) =>
  api.get<RegistryImage[]>(`/registry/images?repository=${encodeURIComponent(repository)}`);

export function registryHostForImage(image: string): string {
  const first = image.split("/")[0];
  if (first.includes(".") || first.includes(":") || first === "localhost") return first;
  return "index.docker.io";
}
