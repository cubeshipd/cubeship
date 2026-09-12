import { validRange } from "semver";
import type { Diagnostic, Locate } from "./diagnostics";
import { databaseVarNames, findEngine, storeVarNames } from "./engines";
import { attributesFor, findReferences } from "./references";
import type { Manifest } from "./schema";
import {
  healthPathProblem,
  MAX_AUTOSCALE,
  MIN_CPU,
  MIN_MEMORY,
  parseSize,
  prefixPattern,
  reserved,
  slugPattern,
} from "./values";

type Path = (string | number)[];

export function checkSemantics(manifest: Manifest, locate: Locate): Diagnostic[] {
  const found: Diagnostic[] = [];
  const add = (
    severity: Diagnostic["severity"],
    code: string,
    message: string,
    path: Path,
    hint?: string,
  ) => {
    found.push({
      severity,
      code,
      message,
      path,
      range: locate(path, "key") ?? locate(path.slice(0, -1)),
      hint,
    });
  };
  const error = (code: string, message: string, path: Path, hint?: string) =>
    add("error", code, message, path, hint);

  const kinds = [
    { field: "inputs", items: manifest.inputs },
    { field: "databases", items: manifest.databases },
    { field: "stores", items: manifest.stores },
    { field: "apps", items: manifest.apps },
  ] as const;

  // Keys are unique within their kind; a name is unique across the whole
  // instance for a database, so within the file at the very least.
  for (const { field, items } of kinds) {
    const seenKeys = new Set<string>();
    const seenNames = new Set<string>();
    items.forEach((item, index) => {
      if (seenKeys.has(item.key)) {
        error("key.duplicate", `two ${field} share the key "${item.key}"`, [field, index, "key"]);
      }
      seenKeys.add(item.key);

      const named = item as { name?: string };
      const name = named.name ?? item.key;
      if (field !== "inputs") {
        if (!slugPattern.test(name)) {
          error(
            "name.reserved",
            `"${name}" is not a valid name: lowercase letters, digits and dashes`,
            [field, index, "name"],
          );
        }
        const words =
          field === "databases"
            ? reserved.databases
            : field === "stores"
              ? reserved.stores
              : reserved.slugs;
        if (words.has(name)) {
          error("name.reserved", `"${name}" is a reserved name on an instance`, [
            field,
            index,
            "name",
          ]);
        }
        if (seenNames.has(name)) {
          error("name.duplicate", `two ${field} would be called "${name}"`, [field, index, "name"]);
        }
        seenNames.add(name);
      }
    });
  }

  if (manifest.minCubeship && !validRange(manifest.minCubeship)) {
    error("version.range", `"${manifest.minCubeship}" is not a version`, ["minCubeship"]);
  }

  manifest.inputs.forEach((input, index) => {
    if (input.type === "secret" && input.generate !== undefined && input.generate < 8) {
      error("input.generate", "a generated secret is at least 8 characters", [
        "inputs",
        index,
        "generate",
      ]);
    }
  });

  manifest.databases.forEach((database, index) => {
    const engine = findEngine(database.engine);
    if (!engine) {
      error(
        "engine.unknown",
        `"${database.engine}" is not an engine this platform runs`,
        ["databases", index, "engine"],
        "postgres, mysql, mariadb, redis or mongodb",
      );
      return;
    }
    if (database.version && !engine.versions.includes(database.version)) {
      error(
        "engine.version",
        `${engine.engine} ${database.version} is not offered`,
        ["databases", index, "version"],
        `try ${engine.versions.join(", ")}`,
      );
    }
    if (database.username) {
      if (engine.fixedUsername && database.username !== engine.fixedUsername) {
        error("engine.username", `${engine.engine} only has the user "${engine.fixedUsername}"`, [
          "databases",
          index,
          "username",
        ]);
      }
      if (engine.refusedUsernames?.includes(database.username)) {
        error("engine.username", `${engine.engine} refuses the user "${database.username}"`, [
          "databases",
          index,
          "username",
        ]);
      }
    }
    if (database.database && !engine.hasDatabase) {
      add(
        "warning",
        "database.ignored",
        `${engine.engine} has no named databases, so this is ignored`,
        ["databases", index, "database"],
      );
    }
    checkLimits(database.limits, ["databases", index, "limits"]);
  });

  manifest.stores.forEach((store, index) => {
    checkLimits(store.limits, ["stores", index, "limits"]);
  });

  const inputKeys = new Set(manifest.inputs.map((i) => i.key));
  const domainInputs = new Set(
    manifest.inputs.filter((i) => i.type === "domain").map((i) => i.key),
  );
  const appKeys = new Set(manifest.apps.map((a) => a.key));
  const dbKeys = new Set(manifest.databases.map((d) => d.key));
  const storeKeys = new Set(manifest.stores.map((s) => s.key));
  const known = { input: inputKeys, app: appKeys, db: dbKeys, store: storeKeys };

  const checkText = (text: string, path: Path) => {
    for (const reference of findReferences(text)) {
      if (!known[reference.kind].has(reference.key)) {
        error(
          "reference.unknown",
          `${reference.raw} names no ${reference.kind} in this template`,
          path,
        );
        continue;
      }
      const attributes = attributesFor[reference.kind];
      if (attributes.length === 0 && reference.attr) {
        error("reference.attribute", `${reference.raw}: an input has no attributes`, path);
      } else if (
        attributes.length > 0 &&
        (!reference.attr || !attributes.includes(reference.attr))
      ) {
        error(
          "reference.attribute",
          `${reference.raw}: a ${reference.kind} has ${attributes.join(", ")}`,
          path,
        );
      }
    }
  };

  manifest.apps.forEach((app, index) => {
    const at = (...rest: Path): Path => ["apps", index, ...rest];

    const building = app.repo !== undefined || app.build !== undefined;
    if (!app.image && !building) {
      error(
        "source.missing",
        "an app runs an image or builds a repository",
        at("image"),
        "give image, or repo with build",
      );
    }
    if (app.image && building) {
      error("source.conflict", "an app is either an image or a build, not both", at("image"));
    }
    if (app.image?.includes(":")) {
      error("image.tagged", "the image carries no tag: the tag is its own field", at("image"));
    }
    if (building) {
      if (!app.repo) error("source.missing", "a build needs a repo", at("repo"));
      if (!app.build)
        error("build.missing", "a repo needs build: dockerfile or railpack", at("build"));
      if (app.repo && !/^(https?|git):\/\//.test(app.repo)) {
        error("repo.scheme", "a repository is an http, https or git URL", at("repo"));
      }
      if (app.repo?.includes("#")) {
        error("repo.ref", "the branch goes in ref, not after a #", at("repo"));
      }
      if (app.tag) error("source.conflict", "a built app has no tag", at("tag"));
      if (app.dockerfile && app.build !== "dockerfile") {
        error(
          "dockerfile.misplaced",
          "dockerfile only means something with build: dockerfile",
          at("dockerfile"),
        );
      }
    }

    if (app.health) {
      const problem = healthPathProblem(app.health);
      if (problem) error("health.path", problem, at("health"));
    }

    app.domains.forEach((entry, d) => {
      const references = findReferences(entry.host);
      const single = references.length === 1 && references[0].raw === entry.host.trim();
      if (!single || references[0].kind !== "input") {
        error(
          "domain.literal",
          "a domain comes from an input, never a literal host",
          at("domains", d, "host"),
          "add an input of type domain and use ${input.<key>}",
        );
        return;
      }
      // Existence and attribute shape go through the same check as
      // everything else; only "is this input a domain" is specific here.
      checkText(entry.host, at("domains", d, "host"));
      if (inputKeys.has(references[0].key) && !domainInputs.has(references[0].key)) {
        error(
          "domain.literal",
          `${references[0].raw} is not an input of type domain`,
          at("domains", d, "host"),
        );
      }
    });

    const writes = new Map<string, string>();
    app.attach.forEach((entry, a) => {
      if (entry.prefix && !prefixPattern.test(entry.prefix)) {
        error(
          "attach.prefix",
          "a prefix is upper case and ends in an underscore, like ANALYTICS_",
          at("attach", a, "prefix"),
        );
      }
      if (Boolean(entry.database) === Boolean(entry.store)) {
        error(
          "attach.kind",
          "an attachment names a database or a store, exactly one",
          at("attach", a),
        );
        return;
      }
      if (entry.database) {
        const database = manifest.databases.find((d) => d.key === entry.database);
        if (!database) {
          error(
            "attach.unknown",
            `no database in this template has the key "${entry.database}"`,
            at("attach", a, "database"),
          );
          return;
        }
        const engine = findEngine(database.engine);
        if (!engine) return;
        for (const name of databaseVarNames(engine, entry.prefix)) {
          const owner = writes.get(name);
          if (owner) {
            error(
              "attach.collision",
              `${owner} and ${entry.database} both write ${name}: give one a prefix`,
              at("attach", a, "prefix"),
            );
            break;
          }
          writes.set(name, entry.database);
        }
        return;
      }
      const store = manifest.stores.find((s) => s.key === entry.store);
      if (!store) {
        const input = manifest.inputs.find((i) => i.key === entry.store && i.type === "store");
        if (!input) {
          error(
            "attach.unknown",
            `no store or store input has the key "${entry.store}"`,
            at("attach", a, "store"),
          );
          return;
        }
      }
      if (!entry.bucket) {
        error(
          "attach.bucket",
          "a store attachment names the bucket it gives the app",
          at("attach", a, "bucket"),
        );
      }
      for (const name of storeVarNames(entry.prefix)) {
        const owner = writes.get(name);
        if (owner) {
          error(
            "attach.collision",
            `${owner} and ${entry.store} both write ${name}: give one a prefix`,
            at("attach", a, "prefix"),
          );
          break;
        }
        writes.set(name, entry.store ?? "");
      }
    });

    for (const [name, value] of Object.entries(app.env)) {
      checkText(value, at("env", name));
      const owner = writes.get(name);
      if (owner) {
        add(
          "warning",
          "attach.collision",
          `this overrides ${name}, which attaching ${owner} already writes`,
          at("env", name),
        );
      }
    }

    checkLimits(app.limits, at("limits"));

    if (app.autoscale) {
      const { min = 1, max, cpu } = app.autoscale;
      if (max < 1 || max > MAX_AUTOSCALE) {
        error("autoscale.range", `max is between 1 and ${MAX_AUTOSCALE}`, at("autoscale", "max"));
      }
      if (min < 1 || min > max)
        error("autoscale.range", "min is between 1 and max", at("autoscale", "min"));
      if (cpu <= 0)
        error(
          "autoscale.range",
          "cpu is a target above 0, where 100 is one core",
          at("autoscale", "cpu"),
        );
    }
  });

  return found;

  function checkLimits(value: { cpu?: number; memory?: string | number } | undefined, path: Path) {
    if (!value) return;
    if (value.cpu !== undefined && value.cpu < MIN_CPU) {
      error("limits.cpu", `a CPU limit is at least ${MIN_CPU} of a core`, [...path, "cpu"]);
    }
    if (value.memory !== undefined) {
      const bytes = parseSize(value.memory);
      if (bytes === undefined) {
        error("limits.memory", "a memory limit is a size: 512Mi, 2Gi, 1500M", [...path, "memory"]);
      } else if (bytes < MIN_MEMORY) {
        error("limits.memory", "a memory limit is at least 6Mi", [...path, "memory"]);
      }
    }
  }
}
