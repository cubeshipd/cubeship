// data-track="install" data-track-location="hero" arrives in a dataset as
// { track: "install", trackLocation: "hero" }.
export function eventData(
  dataset: DOMStringMap,
): { name: string; data: Record<string, string> } | undefined {
  const name = dataset.track;
  if (!name) return undefined;
  const data: Record<string, string> = {};
  for (const [key, value] of Object.entries(dataset)) {
    if (key === "track" || !key.startsWith("track") || value === undefined) continue;
    const property = key.slice("track".length);
    data[property.charAt(0).toLowerCase() + property.slice(1)] = value;
  }
  return { name, data };
}
