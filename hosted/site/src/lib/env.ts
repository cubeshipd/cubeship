// Read through a function, never at import time: `next build` runs with
// none of the site's variables set.
export function catalogUrl(): string {
  return process.env.CATALOG_URL || "http://localhost:8080";
}

// Where Umami is. Empty collects nothing: the tracker's paths answer 404.
export function umamiUrl(): string {
  return process.env.UMAMI_URL || "";
}
