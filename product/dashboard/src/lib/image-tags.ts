import type { RegistryImage } from "@/lib/api";

// Registry APIs do not all report push times. Keep their original order
// after the dated tags, and never offer a commit tag as a release choice.
export function selectableImageTags(tags: RegistryImage[]): RegistryImage[] {
  return tags
    .filter((image) => image.tag && !/^sha(?:256)?[-_]?[0-9a-f]{7,64}$/i.test(image.tag))
    .sort((a, b) => (Date.parse(b.pushed_at ?? "") || 0) - (Date.parse(a.pushed_at ?? "") || 0));
}
