import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import test from "node:test";
import { evaluate, evaluateRun, REQUIRED } from "./ci-aggregate.mjs";

const required = [
  "scope",
  "templates",
  "check",
  "database",
  "integration",
  "scripts",
  "dashboard",
  "site",
];
const success = () => ({
  ...Object.fromEntries(required.map((id) => [id, { result: "success" }])),
  scope: {
    result: "success",
    outputs: { core: "true", templates: "true", dashboard: "true", site: "true" },
  },
});
test("essential_matrix", () => {
  assert.deepEqual(REQUIRED, required);
  assert.equal(evaluate(required, success()).ok, true);
  for (const id of required) {
    for (const result of ["failure", "cancelled", "skipped", "pending", undefined]) {
      const needs = success();
      if (result === undefined) delete needs[id];
      else needs[id] = { result };
      assert.equal(evaluate(required, needs).ok, false, `${id}: ${result}`);
    }
  }
  assert.equal(evaluate(["validate"], { validate: { result: "skipped" } }).ok, false);
});
test("conditional_skip", () => {
  const needs = { validate: { result: "success" }, smoke: { result: "skipped" } };
  assert.equal(evaluate(["validate"], needs, { smoke: { applicable: false } }).ok, true);
  assert.equal(evaluate(["validate"], needs, { smoke: { applicable: true } }).ok, false);
  assert.equal(evaluate(["validate"], needs, { smoke: {} }).ok, false);
  assert.equal(evaluate(["validate"], needs).ok, false);
  assert.equal(
    evaluate(["validate"], { validate: { result: "skipped" } }, { validate: { applicable: false } })
      .ok,
    false,
  );
});
test("unrelated_check and malformed input fail closed", () => {
  assert.equal(evaluate(required, { unrelated: { result: "success" } }).ok, false);
  assert.equal(evaluate(required, { ...success(), unrelated: { result: "success" } }).ok, false);
  for (const value of [null, [], "success", 1]) assert.equal(evaluate(required, value).ok, false);
  for (const value of [null, [], {}, { result: 1 }]) {
    assert.equal(evaluate(required, { ...success(), check: value }).ok, false);
  }
  assert.equal(evaluate(required, success(), { smoke: {} }).ok, false);
});
test("CLI uses the fixed allowlist and rejects invalid JSON", () => {
  for (const [needs, expected] of [
    [JSON.stringify(success()), 0],
    ["{}", 1],
    ["null", 1],
    ["{", 1],
  ]) {
    const result = spawnSync(process.execPath, [".github/scripts/ci-aggregate.mjs"], {
      env: { ...process.env, NEEDS_JSON: needs },
      encoding: "utf8",
    });
    assert.equal(result.status, expected, result.stderr);
  }
});
test("inline Node execution uses environment data safely", () => {
  const body = readFileSync(".github/scripts/ci-aggregate.mjs", "utf8");
  for (const [needs, expected] of [
    [JSON.stringify(success()), 0],
    ["{}", 1],
    ["{", 1],
  ]) {
    const result = spawnSync(process.execPath, ["--input-type=module"], {
      input: body,
      env: { ...process.env, NEEDS_JSON: needs },
      encoding: "utf8",
    });
    assert.equal(result.status, expected, result.stderr);
  }
});
test("workflow embeds the trusted script verbatim and preserves fixed dependencies", () => {
  const workflow = readFileSync(".github/workflows/ci.yml", "utf8");
  const body = workflow
    .split("          node --input-type=module <<'NODE'\n")[1]
    ?.split("          NODE\n")[0];
  assert.equal(
    body
      ?.split("\n")
      .filter((line, index, lines) => index < lines.length - 1)
      .map((line) => line.slice(10))
      .join("\n") + "\n",
    readFileSync(".github/scripts/ci-aggregate.mjs", "utf8"),
  );
  const aggregate = workflow.split("\n  aggregate:\n")[1];
  assert.match(aggregate, /name: CI \/ aggregate/);
  assert.match(
    aggregate,
    /needs: \[scope, templates, check, database, integration, scripts, dashboard, site\]/,
  );
  assert.match(aggregate, /if: always\(\)/);
  assert.match(aggregate, /NEEDS_JSON: \$\{\{ toJSON\(needs\) \}\}/);
  assert.doesNotMatch(aggregate, /checkout|secrets\./);
});

test("only explicitly excluded component jobs may be skipped", () => {
  const needs = success();
  needs.scope.outputs = { core: "false", templates: "true", dashboard: "false", site: "false" };
  for (const id of required.filter((id) => !["scope", "templates"].includes(id)))
    needs[id].result = "skipped";
  assert.equal(evaluateRun(needs).ok, true);
  needs.templates.result = "failure";
  assert.equal(evaluateRun(needs).ok, false);
  needs.templates.result = "success";
  needs.scope.outputs.core = "true";
  assert.equal(evaluateRun(needs).ok, false);
  needs.scope.outputs.core = "false";
  needs.scope.outputs.site = "true";
  needs.site.result = "success";
  assert.equal(evaluateRun(needs).ok, true);
  needs.site.result = "skipped";
  assert.equal(evaluateRun(needs).ok, false);
  delete needs.scope.outputs.core;
  assert.equal(evaluateRun(needs).ok, false);
  needs.scope.outputs.core = "false";
  needs.scope.result = "failure";
  assert.equal(evaluateRun(needs).ok, false);
});
