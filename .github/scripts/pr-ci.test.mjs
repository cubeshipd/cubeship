import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
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

test("assigns new PRs to the maintainer without checking out PR code", () => {
  const workflow = readFileSync(".github/workflows/assign-pr.yml", "utf8");
  assert.match(workflow, /pull_request_target:\n    types: \[opened\]/);
  assert.match(workflow, /issues: write/);
  assert.match(workflow, /assignees\[\]=lucasaarch/);
  assert.doesNotMatch(workflow, /actions\/checkout/);
});
