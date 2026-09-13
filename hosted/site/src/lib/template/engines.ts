export type Engine = {
  engine: string;
  // Newest first: an omitted version means the newest at install time.
  versions: string[];
  stem: string;
  port: number;
  hasDatabase: boolean;
  fixedUsername?: string;
  refusedUsernames?: string[];
};

// internal/datastore/engine.go is the original. A template is checked
// here, before any instance is involved, so the table is copied.
export const engines: Engine[] = [
  {
    engine: "postgres",
    versions: ["18", "17", "16", "15"],
    stem: "DATABASE",
    port: 5432,
    hasDatabase: true,
  },
  {
    engine: "mysql",
    versions: ["8.4", "8.0"],
    stem: "DATABASE",
    port: 3306,
    hasDatabase: true,
    refusedUsernames: ["root"],
  },
  {
    engine: "mariadb",
    versions: ["11.4", "10.11"],
    stem: "DATABASE",
    port: 3306,
    hasDatabase: true,
    refusedUsernames: ["root"],
  },
  {
    engine: "redis",
    versions: ["7.4", "7.2"],
    stem: "REDIS",
    port: 6379,
    hasDatabase: false,
    fixedUsername: "default",
  },
  { engine: "mongodb", versions: ["8.0", "7.0"], stem: "MONGO", port: 27017, hasDatabase: true },
];

export function findEngine(name: string): Engine | undefined {
  return engines.find((e) => e.engine === name);
}

export function databaseVarNames(engine: Engine, prefix: string): string[] {
  const stem = `${prefix}${engine.stem}`;
  const names = [`${stem}_URL`, `${stem}_HOST`, `${stem}_PORT`, `${stem}_USER`, `${stem}_PASSWORD`];
  if (engine.hasDatabase) names.push(`${stem}_NAME`);
  return names;
}

export function storeVarNames(prefix: string): string[] {
  const stem = `${prefix}S3`;
  return [
    `${stem}_ENDPOINT`,
    `${stem}_REGION`,
    `${stem}_BUCKET`,
    `${stem}_ACCESS_KEY_ID`,
    `${stem}_SECRET_ACCESS_KEY`,
    `${stem}_PATH_STYLE`,
  ];
}
