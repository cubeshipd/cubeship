import type { RegistryImage } from "@/lib/api";

// GHCR's public API only reports image build time. Use it when the
// publication time is unavailable; leave undated tags last.
export function selectableImageTags(tags: RegistryImage[]): RegistryImage[] {
  return tags
    .filter((image) => image.tag && !/^sha(?:256)?[-_]?[0-9a-f]{7,64}$/i.test(image.tag))
    .sort(
      (a, b) =>
        (Date.parse(b.pushed_at ?? b.created_at ?? "") || 0) -
        (Date.parse(a.pushed_at ?? a.created_at ?? "") || 0),
    );
}
