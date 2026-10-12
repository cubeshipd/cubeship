# Public Issue triage (disabled preparation)

`issue-triage.yml` is an offline-tested preparation for `cubeshipd/cubeship`.
Both `enabled` and `publishEnabled` in `scripts/issue-config.json` remain **false**.
No Google request or GitHub mutation has been tested live. The previous PR AI
workflow and scripts were removed locally; PR52 has not been closed or merged.

After the remaining activation approval, only newly opened public Issues would
enter automatic triage. The workflow does not listen to edits, comments or PRs.
It accepts a maintainer-only manual `workflow_dispatch` with an Issue number and
`dry_run=true`: the collector verifies maintain/admin permission through GitHub,
validates public source and history, and makes no Google request or write.
Manual live dispatch, reruns, other branches, private repositories, bots, PRs,
closed/missing Issues and unknown contexts fail closed. Missing permission/read
access has no private or credential fallback.

## What triage may change

The deterministic publisher can add up to three existing labels from `bug`,
`enhancement`, `documentation`, `question`, `accessibility`, `area: app` and
`dependencies`. It fills absent native Issue type (Task, Bug or Feature) and
absent native **Priority** (Urgent, High, Medium or Low) only at confidence >=0.85.
Priority is Issue field `46483201`, independent of Projects: its POST payload is
an additive nonempty list with the field ID and option **name**, never a PUT or
replacement of all fields. Human type/Priority, Effort (`46483204`), other fields,
existing labels, title and body are preserved. A legacy priority label blocks
Priority inference; there is no automatic P1/P2/P3 conversion or migration.

A bot comment may contain an escaped English translation and up to three
possible duplicates at confidence >=0.90. English remains provisional. Links
are built locally from verified same-repository candidates; the model cannot
choose arbitrary URLs or field IDs. There is no automatic duplicate/invalid/
wontfix policy label, closing, reopening, assignment or deletion. A comment is
created only when translation or duplicates add information; reuse requires the
stable original-content fingerprint marker and ownership by the bot.

Content is refetched before generation and each publication phase. A changed
original, target state or candidate blocks stale effects. Checks and additive
requests are not a transaction: some effects can complete before a later refusal,
and a human value can race between the last read and write. Unknown API outcomes
or an unconfirmed native receipt are reported without blind retry or broader
permissions. Original-content checks and comment markers provide best-effort
idempotency; duplicate comments and uncertain usage remain possible.

## Data and credentials

Google would receive only sanitized public title/body prose and public titles
and short snippets of at most ten candidates from this repository. Authors,
profiles, comments, native field values, Projects, attachments, code, logs and
credentials are excluded. Sensitive/private markers and credentials are checked
in the full bounded original before omission. Complete safe code/log blocks are
omitted, with that omission represented in the model input; an omitted empty
body cannot cause an invented translation. Ordinary technical commands and
code-like prose are inert data and are never executed, fetched or used as tools.
Conservative filtering cannot detect every secret in public prose.

The fingerprint remains SHA256 of original `title + NUL + body`; sanitization
does not replace the Issue or silently truncate prose. Raw title/body is bounded
at 2 MiB; sanitized primary prose at 4,096 UTF-8 bytes. A GitHub manual-ID response
is independently capped at 2 MiB, read through a fixed trusted GET without
redirects or source-body logging.

Every stage executes trusted default-branch source at `github.workflow_sha`.
All three script/config literal SHA256 digests must pass before any module import.
Inference has no checkout, GitHub token property (even an empty one is refused)
or repository permissions. Its three-file bootstrap is anonymous and fixed to
this public repository. The Google secret appears only on the conditional
`Generate Gemini proposal` step, after successful collection and verified
bootstrap. No key is present in gates, downloads, summaries or publication.

Future per-job token permissions are:

| Job | Permissions | Reason |
| --- | --- | --- |
| Collector | contents: read, issues: read, actions: read | Trusted source, public source/catalog and complete admission history |
| Gemini inference | none (`permissions: {}`) | Public anonymous refetches and exact current-run provenance |
| Publisher | contents: read, issues: write, actions: read | Trusted source, authorized additive effects and its own-run provenance proof |

There is no Actions write, organization/Projects scope, new token, account grant
or settings change. This describes disabled workflow permissions; it does not
activate them. Snapshot transfer contains only exact context, identity,
fingerprints and candidate IDs/hashes. Proposal transfer adds only validated
filtered output and filtered usage. Same-run named artifacts retain one day;
raw source, prompts, raw responses and logs are never uploaded. Downloads use a
fresh directory, passed directly to the CLI; it refuses extra entries, symlinks,
hard links, oversize files and wrong origin. COLLECTED metadata is a trusted
same-run workflow attestation, not a standalone cryptographic admission token.

## Admission and cost limits

One repository-global workflow concurrency group holds serialization from
collection through publication: `cancel-in-progress: false`, `queue: max`.
GitHub supports up to 100 pending runs; excess runs can be cancelled. It is not
an unlimited delivery queue. The collector is the sole full-history/quota
admission authority, before any inference. Unknown, incomplete, ambiguous,
concurrent or rerun history blocks admission. History is bounded to ten pages
of 100 runs. Do not bypass this gate with fabricated COLLECTED objects.

The `Gemini inference` job always records its harmless gate even when collection
is disabled, dry, blocked or skipped. Its stable `Generate Gemini proposal` step
is explicitly skipped in those cases. Completed live runs must contain exactly
one such step; a started non-skipped step reserves the Issue identity and quota
conservatively, including failure, timeout and cancellation. Quotas use the
step start time, not when a queued run was created. Dry provenance and skipped
steps do not reserve generation budget; accounting is best effort, not proof of
actual billing or exactly-once execution. A reservation also prevents later
regeneration of the same identity outside the budget window.

| Configured limit | Value |
| --- | --- |
| Model | gemini-3.5-flash-lite, no tools or exposed thoughts |
| Full system/input/schema | 32,768 UTF-8 bytes; 8,000 counted tokens |
| Primary prose / candidate snippet | 4,096 / 400 UTF-8 bytes |
| Output | 2,048 tokens; 16,384 JSON bytes |
| Rendered comment | 6,000 characters |
| Count / generation timeout | 30 / 60 seconds |
| Generation and GitHub mutations | One attempt; no blind retries |
| GitHub GETs | Up to three attempts with bounded backoff |
| Started-generation admission | At most ten/rolling hour and 100/UTC calendar month, best effort |

The US$5 monthly goal is **not a hard account billing cap**. Configured Standard
prices are US$0.30/million input tokens and US$2.50/million output tokens including
thinking; the configured-limit estimate is US$0.00752 per attempt. Actual model
compatibility and measured provider consumption await authorized live testing.
These limits do not bound other account uses.

Synthetic ten-candidate inference now uses 19 anonymous GitHub GETs independent
of prior history length; authenticated collector history reads remain separate.
Three positive cases and one collector-blocked sensitive case would use 57
inference GETs with no retries or extra pages. A shared anonymous 60/hour bucket,
unrelated traffic, bootstrap, pagination and retries can consume the small
margin. Ten/hour is an admission ceiling, not promised throughput. Pace and stop
on unknown or exhausted read limits; four positive cases already need pacing.
Anonymous maintainer-role reads can also abstain if GitHub does not expose them.

The current stop mechanism is the two config flags/repository allowlist; no
per-Issue opt-out label or new label/field/type/Project setup is implemented.
Do not delete history or retry uncertain generation to clear a refusal.

## Verification and remaining activation

Offline verification runs `node --test .github/scripts/*.test.mjs` and the
repository `make check`. Tests execute the literal shell bootstrap with fixture
curl and real Node/cryptographic verification, including every bad hash before
import; real CLI runs use only synthetic native HTTP transports. CI still keeps
its six essential jobs and always-running aggregate gate unchanged.

Local actionlint 1.7.12 reports only its unsupported `concurrency.queue` key.
The workflow structurally verifies the exact official group/cancel/queue shape;
a temporary lint copy removes **only** that key to expose any other errors.
No linter install, broad ignore or production queue removal is used.

Before persistent activation, present one concrete approval for the remaining
security scope: activation in `cubeshipd/cubeship`, use of the existing Gemini
secret, public data sent to Google/model/cost and external end-to-end tests.
Existing authorization for labels/type/Priority/comments on new Issues remains;
do not ask again for those effects. No key read, grant, field/type/label setup,
Projects setup, push, PR merge or remote cleanup is authorized by this preparation.

Proposed public synthetic test titles are “Falha simulada ao salvar”,
“Synthetic save failure duplicate”, “Melhoria simulada de filtro” and
“Synthetic security refusal”. None has been created and no Issue numbers are
assigned. After approval, verify useful translation, native type/Priority,
verified duplicate links, human fields/Effort/original preservation, repetition
without new effects, security refusal and actual usage. Stop on unknown API or
read-limit outcomes. Replication to `cubeshipd/cubeship-templates` and
`lucasaarch/jian` follows successful Cubeship testing, not this local preparation.
Recoverable closing of only those four future test Issues and obsolete PR52
requires the separate proposed cleanup approval; preserve branches and history,
never delete or merge PR52 during cleanup.

Current design: [Issue triage](../docs/superpowers/specs/2026-10-12-gemini-issue-triage-design.md).
Official contracts: [Issue field values](https://docs.github.com/en/rest/issues/issue-field-values),
[workflow concurrency](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-workflow-concurrency).
