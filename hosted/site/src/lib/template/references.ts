export type ReferenceKind = "input" | "app" | "db" | "store";

export type Reference = {
  kind: ReferenceKind;
  key: string;
  attr?: string;
  raw: string;
  start: number;
};

export const attributesFor: Record<ReferenceKind, string[]> = {
  input: [],
  app: ["internal", "host", "port"],
  db: ["host", "port", "user", "password", "name"],
  store: ["bucket", "endpoint"],
};

const pattern = /\$\{(input|app|db|store)\.([a-zA-Z][a-zA-Z0-9_-]*)(?:\.([a-z]+))?\}/g;

export function findReferences(value: string): Reference[] {
  return [...value.matchAll(pattern)].map((match) => ({
    kind: match[1] as ReferenceKind,
    key: match[2],
    attr: match[3],
    raw: match[0],
    start: match.index,
  }));
}

// internal/app/reference.go: the network alias every replica answers on.
export function internalHost(project: string, environment: string, appName: string): string {
  return `cubeship-${project}-${environment}-${appName}`;
}
