import assert from "node:assert/strict";
import test from "node:test";
import { ciScope } from "./ci-scope.mjs";

test("master CI runs only the affected components", () => {
  assert.deepEqual(ciScope(["templates/umami/template.yaml"]), {
    core: false,
    templates: true,
    dashboard: false,
    site: false,
  });
  assert.deepEqual(ciScope(["site/package.json"]), {
    core: false,
    templates: false,
    dashboard: false,
    site: true,
  });
  assert.deepEqual(ciScope(["dashboard/src/app/page.tsx", "core/internal/app/app.go"]), {
    core: true,
    templates: false,
    dashboard: true,
    site: false,
  });
  assert.deepEqual(ciScope(["core/template/parse.go"]), {
    core: true,
    templates: true,
    dashboard: false,
    site: false,
  });
  assert.equal(ciScope(["core/go.mod"]).templates, true);
  assert.equal(ciScope(["core/internal/slug/slug.go"]).templates, true);
  assert.equal(ciScope(["install.sh"]).core, true);
});

test("unknown and workflow paths get every check", () => {
  const all = { core: true, templates: true, dashboard: true, site: true };
  for (const paths of [
    [],
    ["README.md"],
    [".github/workflows/ci.yml"],
    ["templates/umami/template.yaml", "renovate.json"],
  ]) {
    assert.deepEqual(ciScope(paths), all);
  }
});
