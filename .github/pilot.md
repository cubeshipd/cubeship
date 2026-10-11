# Manual Gemini pilot

This is an advisory, public-only pilot for `cubeshipd/cubeship`. It does not
comment on a PR, create a check, approve, fix, push, merge, or publish code.
Findings are hypotheses for a maintainer to verify. The workflow is separate
from CI, RC releases, and the base mobile integration.

**Live execution is disabled** by `liveEnabled: false` in
[scripts/pilot-config.json](scripts/pilot-config.json). Dry runs validate real
public metadata and the complete eligible diff, but never request a model
diagnosis. Their artifact and summary identify this as a dry simulation, with
no fabricated findings. Dry mode checks metadata, patch, byte and line gates,
but does not call `countTokens`: it cannot measure the provider token count or
validate the live Gemini model/schema contract. The 12,000-token provider gate
and actual provider interoperability remain unvalidated until an authorized
live workflow run. Selecting `dry_run: false` currently refuses the run
before the secret-bearing step. A secret existing in the repository does not
enable live execution. No billing, quota, secret, or permission setup is
performed by this change.

## Requirements and manual use

The workflow must first be reviewed and registered on the repository's default
branch before GitHub accepts a dispatch. That deployment and a real workflow
run remain unvalidated here; do not bypass this restriction with a PR or another
branch. Once it is registered:

1. Choose **Actions → Gemini pilot → Run workflow**, on the default branch.
2. Enter the open public PR number and its exact current lowercase 40-character
   head SHA. Keep **dry_run** checked (the default).
3. Inspect the run's job summaries and `gemini-pilot-result` artifact. A dry
   summary confirms validation only; it is not evidence that Gemini diagnosed
   the PR.

Both the original actor and triggering actor must have the canonical GitHub
`maintain` or `admin` role. An ordinary write role is insufficient. The workflow
requires attempt 1, the default branch, the expected head, a public repository,
and public head/base repositories. Reruns are refused; dispatch a new dry run
when appropriate. Changes to the head, base, visibility, or open state can make
a run obsolete or blocked. No PR code is checked out or executed in the pilot.

Only the trusted workflow commit supplies scripts. Collection and summary use
read-only checkout with credentials persistence disabled. Inference has empty
GitHub permissions, no checkout or GitHub token, and downloads exactly three
fixed public script/config files anonymously from `github.workflow_sha`.
Literal SHA-256 checks must all pass before any downloaded module executes.
A script/config change therefore also requires review and updating its literal
workflow hash. Action revisions are pinned to full upstream commits. Provider
credentials are scoped solely to the enabled live inference step; neither the
bootstrap nor dry steps reference the secret.

## Content and limits

The complete public diff must fit **20 files, 1,000 changed lines, 128 KiB of
serialized input including system instructions, and 12,000 input tokens**.
Sensitive, generated, lock, binary, symlink, missing or truncated content is
refused, as are incomplete API pagination and unsupported file changes. Limits
are whole-input gates: no chunking and no partial diagnosis passed off as
complete. CI input is limited to verified same-SHA status metadata, never logs.
Empty CI metadata means unknown coverage, not successful CI.

The configured model is `gemini-3.8-flash`, low thinking, one candidate, no tools.
An eventual enabled live run makes at most one generation attempt with a
90-second timeout and **4,096 output tokens**. There is no generation retry,
including after 429, server errors or ambiguous timeouts. Public GitHub reads
allow at most three total attempts (two retries with backoff). Output must fit
**8 KiB JSON, three findings, and a 4,000-character summary**. Findings must
refer to actual changed lines. Links are assembled locally from validated
paths and immutable SHAs. The fixed target-to-head comparison supplies a
validated `diffBase` (merge base) for old trees and LEFT links, including renamed
old paths. `base` remains the target tip for freshness and CI merge parents.
The closed snapshot, model schema and artifact origin include both identities.
URLs strictly encode path components, including parentheses. The final rendered
summary, including escaped prose and complete links, must fit 4,000 characters;
otherwise `SUMMARY_LIMIT` refuses the result before artifact publication. Raw
source excerpts, credentials, links supplied by the model, tool output and malformed results are rejected conservatively. Filtering can refuse
useful content and is not a guarantee against every obfuscated disclosure.

The repository concurrency group allows one running pilot with
`cancel-in-progress: false`. GitHub may replace an older pending run; this is
not a durable queue. Jobs have **3/5/2-minute** collection/inference/summary
budgets, totaling ten runner minutes; queue and startup time are not a hard
workflow wall-clock guarantee. History protects each repo/PR/head on a
best-effort basis: another live attempt reserves that SHA even if it failed.
Dry history does not consume the live reservation. The canonical title
`gemini:<repo>:dry` is independent of raw PR/SHA inputs, so refused malformed dry
dispatches cannot poison later history. Live titles remain
`gemini:<repo>:<PR>:<headSHA>:live`; unknown or malformed live history fails closed.
Only canonical dry entries with the expected workflow path, default branch and
valid workflow SHA are recognized as nonbillable. This is not exactly-once
billing or a global quota.

## Visibility, outcomes, and cost

Treat run summaries and artifacts as public. Collection transfers only
identity/decision metadata via outputs. The sole uploaded file is filtered
`artifact.json`, retained **one day**, with no prompt, diff, logs, raw model
response, context, credential or permission response. Summary downloads only
the current run's fixed named artifact, rejects extra files, links, wrong
paths, oversized or unexpected envelopes, and checks repo/PR/head/base/diffBase/run/
workflow/mode origin against fresh public metadata before publication.

A run may be **blocked**, **incomplete**, or **obsolete** and provide no
verified diagnosis. Provider usage can be **uncertain**, including a timeout
that may already have been billed. Missing counts are not measured zero. The
manual target is **US$5 per month and at most 100 analyses per month**; inspect
provider usage and stop before exceeding the budget. Neither output limits
nor this workflow guarantee a hard financial cap. Numeric estimates include
candidate and thinking tokens at Standard rates, without discount assumptions.

Keep `liveEnabled: false` to disable live requests. Enabling it requires a
separate reviewed change, updated trusted hash, and authorized provider setup
and validation. Private code is excluded; there is no Free-tier fallback.
Any future private-code use requires a separately authorized Paid setup and
policy change, not a flag workaround.

Public PR 48 remains an external future evaluation case. Its generated lockfile
is excluded by the whole-diff policy; do not weaken the gate to force it through.
No known answer or CI logs enter the prompt, and this pilot does not fix or merge
that external PR. Replication to other repositories is deferred.
