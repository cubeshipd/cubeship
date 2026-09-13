import { bigint, integer, jsonb, pgTable, text, timestamp } from "drizzle-orm/pg-core";

// Read only. hosted/discovery owns these tables and their migrations;
// this is the site's description of what it reads, not a schema it
// creates. A column added there is added here by hand.
export const repositories = pgTable("repositories", {
  id: bigint("id", { mode: "number" }).primaryKey(),
  nodeId: text("node_id").notNull(),
  owner: text("owner").notNull(),
  name: text("name").notNull(),
  description: text("description").notNull(),
  url: text("url").notNull(),
  ownerAvatarUrl: text("owner_avatar_url").notNull(),
  stars: integer("stars").notNull(),
  topics: text("topics").array().notNull(),
  hidden: text("hidden"),
  latestReleaseId: bigint("latest_release_id", { mode: "number" }),
  firstSeenAt: timestamp("first_seen_at", { withTimezone: true }).notNull(),
  checkedAt: timestamp("checked_at", { withTimezone: true }).notNull(),
});

export const releases = pgTable("releases", {
  id: bigint("id", { mode: "number" }).primaryKey(),
  repositoryId: bigint("repository_id", { mode: "number" }).notNull(),
  tag: text("tag").notNull(),
  commitSha: text("commit_sha").notNull(),
  name: text("name").notNull(),
  url: text("url").notNull(),
  publishedAt: timestamp("published_at", { withTimezone: true }).notNull(),
  status: text("status").notNull(),
  problems: jsonb("problems").notNull(),
  manifest: jsonb("manifest"),
  source: text("source"),
  readme: text("readme"),
  iconKey: text("icon_key"),
  indexedAt: timestamp("indexed_at", { withTimezone: true }).notNull(),
});
