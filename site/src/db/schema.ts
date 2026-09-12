import { relations } from "drizzle-orm";
import {
  type AnyPgColumn,
  bigint,
  index,
  integer,
  jsonb,
  pgTable,
  primaryKey,
  serial,
  text,
  timestamp,
  unique,
} from "drizzle-orm/pg-core";

export const users = pgTable("users", {
  id: serial("id").primaryKey(),
  githubId: bigint("github_id", { mode: "number" }).notNull().unique(),
  login: text("login").notNull(),
  name: text("name"),
  avatarUrl: text("avatar_url"),
  role: text("role").notNull().default("user"),
  blockedAt: timestamp("blocked_at", { withTimezone: true }),
  createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
});

export const sessions = pgTable("sessions", {
  // The cookie carries the token; only its hash is stored, so a dump of
  // this table is not a drawer of live sessions.
  tokenHash: text("token_hash").primaryKey(),
  userId: integer("user_id")
    .notNull()
    .references(() => users.id, { onDelete: "cascade" }),
  expiresAt: timestamp("expires_at", { withTimezone: true }).notNull(),
  createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
});

export const templates = pgTable(
  "templates",
  {
    id: serial("id").primaryKey(),
    slug: text("slug").notNull().unique(),
    authorId: integer("author_id")
      .notNull()
      .references(() => users.id),
    name: text("name").notNull(),
    summary: text("summary").notNull(),
    imageKey: text("image_key"),
    tags: text("tags").array().notNull().default([]),
    status: text("status").notNull().default("draft"),
    likesCount: integer("likes_count").notNull().default(0),
    // No foreign key: templates and template_versions reference each
    // other, and the publish transaction is what keeps this honest.
    currentVersionId: integer("current_version_id"),
    createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
    updatedAt: timestamp("updated_at", { withTimezone: true }).notNull().defaultNow(),
  },
  (table) => [
    index("templates_status_idx").on(table.status),
    index("templates_author_idx").on(table.authorId),
  ],
);

export const templateVersions = pgTable(
  "template_versions",
  {
    id: serial("id").primaryKey(),
    templateId: integer("template_id")
      .notNull()
      .references(() => templates.id, { onDelete: "cascade" }),
    number: integer("number").notNull(),
    manifest: jsonb("manifest").notNull(),
    source: text("source").notNull(),
    schemaVersion: integer("schema_version").notNull(),
    notes: text("notes"),
    createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
  },
  (table) => [unique("template_versions_number").on(table.templateId, table.number)],
);

export const likes = pgTable(
  "likes",
  {
    templateId: integer("template_id")
      .notNull()
      .references(() => templates.id, { onDelete: "cascade" }),
    userId: integer("user_id")
      .notNull()
      .references(() => users.id, { onDelete: "cascade" }),
    createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
  },
  (table) => [primaryKey({ columns: [table.templateId, table.userId] })],
);

export const comments = pgTable(
  "comments",
  {
    id: serial("id").primaryKey(),
    templateId: integer("template_id")
      .notNull()
      .references(() => templates.id, { onDelete: "cascade" }),
    authorId: integer("author_id")
      .notNull()
      .references(() => users.id),
    parentId: integer("parent_id").references((): AnyPgColumn => comments.id, {
      onDelete: "cascade",
    }),
    body: text("body").notNull(),
    createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
    updatedAt: timestamp("updated_at", { withTimezone: true }).notNull().defaultNow(),
    // Soft, so a removed comment keeps the thread's shape.
    deletedAt: timestamp("deleted_at", { withTimezone: true }),
  },
  (table) => [index("comments_template_idx").on(table.templateId)],
);

export const reports = pgTable("reports", {
  id: serial("id").primaryKey(),
  subjectType: text("subject_type").notNull(),
  subjectId: integer("subject_id").notNull(),
  reporterId: integer("reporter_id")
    .notNull()
    .references(() => users.id),
  reason: text("reason").notNull(),
  note: text("note"),
  createdAt: timestamp("created_at", { withTimezone: true }).notNull().defaultNow(),
  resolvedAt: timestamp("resolved_at", { withTimezone: true }),
  resolution: text("resolution"),
});

export const templateRelations = relations(templates, ({ one, many }) => ({
  author: one(users, { fields: [templates.authorId], references: [users.id] }),
  versions: many(templateVersions),
}));
