// UTC, so the server and the browser print the same day.
const dateFormat = new Intl.DateTimeFormat("en", { dateStyle: "medium", timeZone: "UTC" });

export function formatDate(date: Date): string {
  return dateFormat.format(date);
}
