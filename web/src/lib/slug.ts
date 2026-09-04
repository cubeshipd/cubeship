// The shape every user-chosen identifier in Cubeship has to take, as the
// daemon's internal/slug states it: `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`.
// An organization or app slug becomes a path component of a registry
// image reference, and Docker rejects uppercase, accents and spaces — so
// a name typed by a person cannot be used as one unaltered.

// strip folds accented letters onto their ASCII base ("Ação" → "Acao")
// rather than dropping them, which is what keeps a Portuguese name from
// slugifying into holes.
function strip(input: string): string {
  return input.normalize("NFD").replace(/[\u0300-\u036f]/g, "");
}

// sanitize is for a slug field being typed into: it refuses characters
// the daemon would reject, but leaves a trailing dash alone so "my-" can
// still become "my-app" on the next keystroke.
export function sanitize(input: string): string {
  return strip(input)
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, "-")
    .replace(/-{2,}/g, "-")
    .replace(/^-+/, "");
}

// toSlug is for deriving one from a name that was typed for people. The
// result is valid or empty — never something the daemon will refuse.
export function toSlug(input: string): string {
  return sanitize(input).replace(/-+$/, "");
}
