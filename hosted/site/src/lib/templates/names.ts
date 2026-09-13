import { Topic } from "./topic";

// A repository is named cubeship-<app>-template by convention, and the
// catalog calls it by the <app>: "cubeship-uptime-kuma-template" is
// Uptime Kuma. A name outside the convention is shown as it is.
export function displayName(repository: string): string {
  const core = repository.replace(/^cubeship-/i, "").replace(/-template$/i, "");
  const words = core.split(/[-_]+/).filter(Boolean);
  if (words.length === 0) return repository;
  return words.map((word) => word[0].toUpperCase() + word.slice(1)).join(" ");
}

// The topic that lists a repository says nothing about what it is.
export function tagsOf(topics: string[]): string[] {
  return topics.filter((topic) => topic !== Topic);
}
