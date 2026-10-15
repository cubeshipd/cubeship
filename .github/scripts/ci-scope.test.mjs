import assert from "node:assert/strict";
import test from "node:test";
import { needsFullCI } from "./ci-scope.mjs";

test("template-only changes do not boot the daemon test infrastructure", () => {
  assert.equal(
    needsFullCI(["templates/umami/template.yaml", "templates/signoz/Dockerfile"]),
    false,
  );
  for (const paths of [
    [],
    ["core/template/parse.go"],
    ["templates/umami/template.yaml", "renovate.json"],
    [".github/workflows/ci.yml"],
  ]) {
    assert.equal(needsFullCI(paths), true);
  }
});
