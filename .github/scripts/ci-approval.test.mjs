import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import test from "node:test";
import { hasCurrentApproval } from "./ci-approval.mjs";

const review = (login, state, commit_id = "head") => ({ login, state, commit_id });

test("requires a non-author approval on the current commit", () => {
  assert.equal(hasCurrentApproval([review("reviewer", "APPROVED")], "head", "author"), true);
  assert.equal(hasCurrentApproval([review("author", "APPROVED")], "head", "author"), false);
  assert.equal(
    hasCurrentApproval([review("reviewer", "APPROVED", "old")], "head", "author"),
    false,
  );
  assert.equal(hasCurrentApproval([], "head", "author"), false);
});

test("uses each reviewer's latest review", () => {
  assert.equal(
    hasCurrentApproval(
      [review("reviewer", "APPROVED"), review("reviewer", "CHANGES_REQUESTED")],
      "head",
      "author",
    ),
    false,
  );
});

test("workflow embeds the same approval check and keeps CI behind it", () => {
  const workflow = readFileSync(".github/workflows/pr-ci.yml", "utf8");
  const body = workflow
    .split("REVIEWS_JSON=\"$reviews\" node --input-type=module <<'NODE'\n")[1]
    ?.split("          NODE\n")[0];
  assert.equal(
    body
      ?.split("\n")
      .filter((line, index, lines) => index < lines.length - 1)
      .map((line) => line.slice(10))
      .join("\n") + "\n",
    readFileSync(".github/scripts/ci-approval.mjs", "utf8"),
  );
  assert.match(workflow, /pull_request_review:\n    types: \[submitted, edited, dismissed\]/);
  assert.match(workflow, /converted_to_draft/);
  assert.match(workflow, /needs: approval\n    if: needs\.approval\.outputs\.approved == 'true'/);
  assert.match(workflow, /No approval for current commit \$PR_HEAD_SHA/);
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

test("CLI fails closed for unapproved, stale, or malformed reviews", () => {
  for (const [reviews, expected] of [
    [[review("reviewer", "APPROVED")], "true\n"],
    [[review("author", "APPROVED")], "false\n"],
    [[review("reviewer", "APPROVED", "old")], "false\n"],
    [null, "false\n"],
  ]) {
    const result = spawnSync(process.execPath, [".github/scripts/ci-approval.mjs"], {
      env: {
        ...process.env,
        CI_APPROVAL_CLI: "1",
        PR_HEAD_SHA: "head",
        PR_AUTHOR: "author",
        REVIEWS_JSON: JSON.stringify(reviews),
      },
      encoding: "utf8",
    });
    assert.equal(result.status, 0, result.stderr);
    assert.equal(result.stdout, expected);
  }
  const malformed = spawnSync(process.execPath, [".github/scripts/ci-approval.mjs"], {
    env: {
      ...process.env,
      CI_APPROVAL_CLI: "1",
      PR_HEAD_SHA: "head",
      PR_AUTHOR: "author",
      REVIEWS_JSON: "{",
    },
    encoding: "utf8",
  });
  assert.equal(malformed.status, 1);
});
