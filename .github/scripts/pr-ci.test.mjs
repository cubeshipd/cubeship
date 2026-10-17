import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import test from "node:test";

test("runs PR checks without approval and keeps component filters", () => {
  const workflow = readFileSync(".github/workflows/pr-ci.yml", "utf8");
  assert.match(
    workflow,
    /pull_request:\n    branches: \[master\]\n    types: \[opened, synchronize, reopened, ready_for_review, converted_to_draft\]/,
  );
  assert.doesNotMatch(workflow, /pull_request_target|pull_request_review|approval/);
  assert.match(workflow, /ref: \$\{\{ github\.event\.pull_request\.base\.sha \}\}/);
  assert.match(workflow, /contents: read\n      pull-requests: read/);
  for (const [job, scope] of [
    ["fast-checks", "core"],
    ["templates", "templates"],
    ["dashboard", "dashboard"],
    ["site", "site"],
  ]) {
    const section = workflow.match(
      new RegExp(`  ${job}:\\n([\\s\\S]*?)(?=\\n  [a-z-]+:\\n|$)`),
    )?.[1];
    assert.match(section ?? "", /needs: \[changes\]/);
    assert.match(section ?? "", new RegExp(`if: needs\\.changes\\.outputs\\.${scope} == 'true'`));
  }
  assert.equal(
    (workflow.match(/ref: \$\{\{ github\.event\.pull_request\.head\.sha \}\}/g) ?? []).length,
    4,
  );
  assert.equal((workflow.match(/persist-credentials: false/g) ?? []).length, 5);
  assert.doesNotMatch(
    readFileSync(".github/workflows/ci.yml", "utf8").split("\njobs:\n")[0],
    /pull_request:/,
  );
});

test("publishes CI / aggregate only when every applicable PR check succeeds", () => {
  const workflow = readFileSync(".github/workflows/pr-ci.yml", "utf8");
  const aggregate = workflow.split("\n  aggregate:\n")[1];
  assert.match(aggregate ?? "", /name: CI \/ aggregate/);
  assert.match(aggregate ?? "", /needs: \[changes, fast-checks, templates, dashboard, site\]/);
  assert.match(aggregate ?? "", /if: always\(\)/);
  assert.doesNotMatch(aggregate ?? "", /actions\/checkout|secrets\./);
  const body = aggregate?.split("          node --input-type=module <<'NODE'\n")[1]?.split("          NODE\n")[0];
  assert.ok(body);
  const needs = {
    changes: { result: "success", outputs: { core: "true", templates: "false", dashboard: "false", site: "false" } },
    "fast-checks": { result: "success" },
    templates: { result: "skipped" },
    dashboard: { result: "skipped" },
    site: { result: "skipped" },
  };
  const run = (input) =>
    spawnSync(process.execPath, ["--input-type=module", "--eval", body], {
      env: { ...process.env, NEEDS_JSON: JSON.stringify(input) },
      encoding: "utf8",
    });
  assert.equal(run(needs).status, 0);
  needs["fast-checks"].result = "failure";
  assert.equal(run(needs).status, 1);
});

test("assigns new PRs to the maintainer without checking out PR code", () => {
  const workflow = readFileSync(".github/workflows/assign-pr.yml", "utf8");
  assert.match(workflow, /pull_request_target:\n    types: \[opened\]/);
  assert.match(workflow, /issues: write/);
  assert.match(workflow, /assignees\[\]=lucasaarch/);
  assert.doesNotMatch(workflow, /actions\/checkout/);
});
