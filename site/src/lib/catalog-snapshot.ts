import { randomUUID } from "node:crypto";
import { mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { z } from "zod";

const entry = z.object({
  slug: z.string().regex(/^[a-z0-9]+(-[a-z0-9]+)*$/),
  id: z.string().regex(/^-[0-9]{1,19}$/),
  name: z.string().min(1),
  description: z.string(),
  source: z
    .string()
    .min(1)
    .max(256 * 1024),
  readme: z
    .string()
    .min(1)
    .max(512 * 1024),
  icon: z
    .string()
    .min(1)
    .max(1024 * 1024)
    .regex(/^[A-Za-z0-9+/]+={0,2}$/),
  manifest: z
    .object({
      schema_version: z.literal(1),
      apps: z.array(z.record(z.string(), z.unknown())),
      inputs: z.array(z.record(z.string(), z.unknown())),
      databases: z.array(z.record(z.string(), z.unknown())),
      stores: z.array(z.record(z.string(), z.unknown())),
    })
    .passthrough(),
});
const schema = z
  .object({
    version: z.literal(1),
    commit: z.string().regex(/^[a-f0-9]{40}$/),
    published_at: z.iso.datetime({ offset: true }),
    templates: z.array(entry).min(1),
  })
  .refine(
    (s) =>
      new Set(s.templates.map((t) => t.slug)).size === s.templates.length &&
      new Set(s.templates.map((t) => t.id)).size === s.templates.length,
    "duplicate template identity",
  );
export type Snapshot = z.infer<typeof schema>;
export type SnapshotEntry = Snapshot["templates"][number];

const source =
  "https://raw.githubusercontent.com/cubeshipd/cubeship-templates/main/_catalog/snapshot.json";
const directory = join(process.cwd(), ".catalog");
const file = join(directory, "snapshot.json");
const maxBytes = 32 * 1024 * 1024;
let current: Snapshot | undefined;
let checkedAt = 0;
let pending: Promise<Snapshot> | undefined;

export function parseSnapshot(text: string): Snapshot {
  if (Buffer.byteLength(text) > maxBytes) throw new Error("catalog snapshot is too large");
  return schema.parse(JSON.parse(text));
}

async function download(): Promise<string> {
  const response = await fetch(source, { cache: "no-store", signal: AbortSignal.timeout(5_000) });
  if (!response.ok || !response.body)
    throw new Error(`catalog snapshot answered ${response.status}`);
  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > maxBytes) throw new Error("catalog snapshot is too large");
      chunks.push(value);
    }
  } finally {
    await reader.cancel();
  }
  return Buffer.concat(chunks).toString("utf8");
}

async function refresh(): Promise<Snapshot> {
  if (!current) {
    try {
      current = parseSnapshot(await readFile(file, "utf8"));
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "ENOENT")
        console.error("catalog cache:", error);
    }
  }
  try {
    const text = await download();
    const next = parseSnapshot(text);
    // Persist before swapping: a failed refresh must also survive a process restart.
    await mkdir(directory, { recursive: true });
    await atomicWrite(join(directory, `${next.commit}.json`), text);
    await atomicWrite(file, text);
    current = next;
  } catch (error) {
    console.error("catalog refresh:", error);
    if (!current)
      throw new Error("the template catalog is unavailable right now", { cause: error });
  } finally {
    checkedAt = Date.now();
  }
  return current;
}

export async function catalogSnapshot(): Promise<Snapshot> {
  if (current && Date.now() - checkedAt < 60_000) return current;
  if (!pending)
    pending = refresh().finally(() => {
      pending = undefined;
    });
  return pending;
}

export async function catalogSnapshotAt(commit: string): Promise<Snapshot | null> {
  if (!/^[a-f0-9]{40}$/.test(commit)) return null;
  const snapshot = await catalogSnapshot();
  if (snapshot.commit === commit) return snapshot;
  try {
    const previous = parseSnapshot(await readFile(join(directory, `${commit}.json`), "utf8"));
    return previous.commit === commit ? previous : null;
  } catch {
    return null;
  }
}

async function atomicWrite(destination: string, text: string): Promise<void> {
  const temporary = `${destination}.${randomUUID()}.tmp`;
  try {
    await writeFile(temporary, text);
    await rename(temporary, destination);
  } finally {
    await rm(temporary, { force: true });
  }
}
