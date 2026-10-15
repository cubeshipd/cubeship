// Where Umami is. Empty collects nothing: the tracker's paths answer 404.
export function umamiUrl(): string {
  return process.env.UMAMI_URL || "";
}
