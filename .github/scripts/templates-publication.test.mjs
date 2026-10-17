import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

test("catalog publication creates and advances only its branch and is idempotent", () => {
  const dir = mkdtempSync(join(tmpdir(), "cubeship-publication-"));
  const remote = join(dir, "remote.git");
  const repo = join(dir, "repo");
  const script = readFileSync(".github/workflows/templates.yml", "utf8")
    .split("        run: |\n")[1]
    .split("\n")
    .map((line) => line.slice(10))
    .join("\n");
  assert.match(readFileSync(".github/workflows/templates.yml", "utf8"), /core\/internal\/template\/\*\*/);
  assert.match(readFileSync(".github/workflows/templates.yml", "utf8"), /core\/internal\/slug\/\*\*/);
  const git = (...args) =>
    execFileSync("git", ["-C", repo, ...args], {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
    }).trim();
  try {
    execFileSync("git", ["init", "--bare", "-q", remote]);
    execFileSync("git", ["init", "-q", "-b", "master", repo]);
    git("config", "user.name", "Test");
    git("config", "user.email", "test@example.com");
    git("config", "commit.gpgsign", "false");
    writeFileSync(join(repo, "README.md"), "source");
    git("add", ".");
    git("commit", "-qm", "Initial");
    const master = git("rev-parse", "HEAD");
    git("remote", "add", "origin", remote);
    git("push", "origin", "master");
    const publish = (value) => {
      git("switch", "master");
      writeFileSync(join(dir, "snapshot.json"), JSON.stringify(value));
      execFileSync("bash", ["-e", "-o", "pipefail", "-c", script], {
        cwd: repo,
        env: { ...process.env, RUNNER_TEMP: dir },
        stdio: "pipe",
      });
      assert.equal(git("rev-parse", "origin/master"), master);
      assert.equal(git("ls-tree", "--name-only", "HEAD"), "snapshot.json");
      return git("rev-parse", "HEAD");
    };
    const first = publish({ version: 2, commit: "first" });
    assert.equal(publish({ version: 2, commit: "first" }), first);
    const second = publish({ version: 2, commit: "second" });
    assert.notEqual(second, first);
    assert.equal(git("rev-parse", "HEAD^"), first);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
