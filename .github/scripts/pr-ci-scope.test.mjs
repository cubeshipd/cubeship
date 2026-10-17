import assert from "node:assert/strict";
import test from "node:test";
import { prCIScope } from "./pr-ci-scope.mjs";

test("scopes PR checks to the files they validate", () => {
  assert.deepEqual(prCIScope(["site/package.json", "site/pnpm-lock.yaml"]), {
    core: false,
    templates: false,
    dashboard: false,
    site: true,
  });
  assert.deepEqual(prCIScope(["templates/umami/template.yaml"]), {
    core: false,
    templates: true,
    dashboard: false,
    site: false,
  });
  assert.deepEqual(prCIScope(["dashboard/src/page.tsx", "core/internal/app/app.go"]), {
    core: true,
    templates: false,
    dashboard: true,
    site: false,
  });
});

test("workflow and build coordination changes run core checks", () => {
  for (const path of [".github/workflows/pr-ci.yml", "Makefile", "go.work.sum"]) {
    assert.equal(prCIScope([path]).core, true);
  }
  assert.equal(prCIScope(["README.md"]).core, false);
  for (const path of ["core/internal/slug/slug.go", "core/go.mod", "core/go.sum"]) {
    assert.equal(prCIScope([path]).templates, true);
  }
});
