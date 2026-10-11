import { readFile, writeFile, appendFile, rename, rm, readdir, lstat } from 'node:fs/promises';
import { pathToFileURL } from 'node:url';
import { config, SYSTEM_INSTRUCTION, authorize, checkHistory, prepareInput, sameSnapshot } from './pilot-core.mjs';

const ids = ['repo', 'pr', 'head', 'base', 'runId', 'workflowSha'];
const identity = snapshot => Object.fromEntries(ids.map(k => [k, snapshot[k]]));
const blocked = () => ({ state: 'blocked', reason: 'VALIDATION_FAILED' });
const integer = n => Number.isSafeInteger(n) && n >= 0;
const sha = s => typeof s === 'string' && /^[a-f0-9]{40}$/.test(s);
const object = x => x !== null && typeof x === 'object' && !Array.isArray(x);
const exact = (x, keys) => object(x) && Object.keys(x).length === keys.length && keys.every(k => Object.hasOwn(x, k));
const fixedError = code => new Error(code);
const nativeTransport = Symbol('nativeTransport');
const wait = ms => new Promise(resolve => setTimeout(resolve, ms));
export function createTransport(fetchImpl = globalThis.fetch, { now = Date.now, sleep = wait } = {}) {
  return { [nativeTransport]: true, fetch: fetchImpl, now, sleep };
}
// All network calls pass this boundary. Redirects never carry credentials elsewhere.
async function request(url, options, transport, limit = 2 * 1024 * 1024) {
  const u = new URL(url);
  if (u.protocol !== 'https:' || u.port || u.username || u.password || !['api.github.com', 'generativelanguage.googleapis.com'].includes(u.hostname)) throw fixedError('HOST_REFUSED');
  let response;
  try { response = await transport.fetch(u.href, { ...options, redirect: 'error' }); }
  catch { throw fixedError('REQUEST_FAILED'); }
  if (!response.ok) { await response.body?.cancel(); return { status: response.status, retryAfter: response.headers.get('retry-after') }; }
  let size = 0; const chunks = [];
  try {
    for await (const chunk of response.body) { size += chunk.byteLength; if (size > limit) throw fixedError('RESPONSE_LIMIT'); chunks.push(chunk); }
    return { status: response.status, data: JSON.parse(Buffer.concat(chunks).toString('utf8')) };
  } catch { throw fixedError('INVALID_RESPONSE'); }
}
function githubPath(path) {
  if (typeof path !== 'string' || !path.startsWith('/repos/') || /[\\#\x00-\x20]/.test(path) || path.includes('..')) throw fixedError('GITHUB_PATH_REFUSED');
  const url = new URL(`https://api.github.com${path}`);
  const repo = url.pathname.split('/').slice(2, 4).join('/');
  if (!config.repositories.includes(repo) || !/^\/repos\/[^/]+\/[^/]+(?:\/(?:pulls\/\d+(?:\/files)?|git\/trees\/[a-f0-9]{40}|commits\/[a-f0-9]{40}|actions\/workflows\/(?:gemini-pilot|ci)\.yml\/runs|actions\/runs\/\d+|collaborators\/[A-Za-z0-9-]+\/permission))?$/.test(url.pathname)) throw fixedError('GITHUB_PATH_REFUSED');
  for (const [key, value] of url.searchParams) if (!['page', 'per_page', 'recursive', 'head_sha'].includes(key) || !/^(?:\d+|[a-f0-9]{40})$/.test(value)) throw fixedError('GITHUB_PATH_REFUSED');
  return url.href;
}
export async function githubRead(path, transport = createTransport()) {
  const url = githubPath(path);
  for (let attempt = 0; attempt < 3; attempt++) {
    let response;
    try { response = await request(url, { method: 'GET', headers: { Accept: 'application/vnd.github+json', 'X-GitHub-Api-Version': '2022-11-28' }, signal: AbortSignal.timeout(30000) }, transport); }
    catch { response = { status: 503 }; }
    if (response.data !== undefined) return response.data;
    if (![429, 500, 502, 503, 504].includes(response.status) || attempt === 2) throw fixedError('GITHUB_READ_FAILED');
    const after = response.retryAfter;
    const delay = after && /^\d+$/.test(after) ? Number(after) * 1000 : after ? Date.parse(after) - (transport.now ?? Date.now)() : 1000 * 2 ** attempt;
    if (!Number.isFinite(delay) || delay > 30000) throw fixedError('GITHUB_READ_FAILED');
    await (transport.sleep ?? wait)(Math.max(0, delay));
  }
}
async function pages(path, field, transport, maxPages = 10) {
  const rows = []; let total;
  for (let page = 1; page <= maxPages; page++) {
    const data = await githubRead(`${path}${path.includes('?') ? '&' : '?'}per_page=100&page=${page}`, transport);
    const batch = field ? data[field] : data;
    if (!Array.isArray(batch) || batch.length > 100 || (field && (!integer(data.total_count) || (total !== undefined && total !== data.total_count)))) throw fixedError('PAGINATION_INCOMPLETE');
    if (field) total = data.total_count;
    rows.push(...batch);
    if (batch.length < 100) { if (field && total !== rows.length) throw fixedError('PAGINATION_INCOMPLETE'); return rows; }
  }
  throw fixedError('PAGINATION_INCOMPLETE');
}
function validateContext(c) {
  if (!object(c) || !config.repositories.includes(c.repo) || !Number.isSafeInteger(c.pr) || c.pr < 1 || !sha(c.sha) || !sha(c.workflowSha) || !Number.isSafeInteger(c.runId) || c.runId < 1 || c.runAttempt !== 1 || !/^[A-Za-z0-9-]+$/.test(c.actor) || !/^[A-Za-z0-9-]+$/.test(c.triggeringActor) || !['boolean', 'undefined'].includes(typeof c.dryRun)) throw fixedError('CONTEXT_REFUSED');
}
export async function collectSnapshot(context, transport = createTransport()) {
  validateContext(context);
  const root = `/repos/${context.repo}`;
  const repo = await githubRead(root, transport);
  const pr = await githubRead(`${root}/pulls/${context.pr}`, transport);
  if (repo.private !== false || repo.visibility !== 'public' || pr.head?.repo?.private !== false || pr.base?.repo?.private !== false || pr.base.repo.full_name !== context.repo || pr.state !== 'open' || pr.number !== context.pr || !sha(pr.head.sha) || !sha(pr.base.sha) || context.defaultBranch !== repo.default_branch || context.ref !== `refs/heads/${repo.default_branch}`) throw fixedError('PUBLIC_PR_REQUIRED');
  const snapshot = { repo: context.repo, pr: context.pr, head: pr.head.sha, base: pr.base.sha, runId: context.runId, workflowSha: context.workflowSha, runAttempt: context.runAttempt, dryRun: context.dryRun === true };
  const rawRuns = await pages(`${root}/actions/workflows/gemini-pilot.yml/runs`, 'workflow_runs', transport);
  const runs = [];
  for (const run of rawRuns) {
    const match = /^gemini:([^:]+):([1-9]\d*):([a-f0-9]{40}):(dry|live)$/.exec(run.display_title ?? '');
    if (!match || match[1] !== context.repo || run.event !== 'workflow_dispatch' || !Number.isSafeInteger(run.id) || !Number.isSafeInteger(run.run_attempt)) throw fixedError('HISTORY_INCOMPLETE');
    if (match[4] === 'live') runs.push({ repo: match[1], pr: Number(match[2]), head: match[3], runId: run.id, runAttempt: run.run_attempt });
  }
  const history = context.dryRun === true ? { state: 'ready' } : checkHistory(snapshot, runs);
  if (history.state !== 'ready') throw fixedError('HISTORY_REFUSED');
  const run = await githubRead(`${root}/actions/runs/${context.runId}`, transport);
  if (run.id !== context.runId || run.run_attempt !== 1 || run.event !== 'workflow_dispatch' || run.path !== '.github/workflows/gemini-pilot.yml' || run.head_sha !== context.workflowSha || run.head_branch !== context.defaultBranch || run.actor?.login !== context.actor || run.triggering_actor?.login !== context.triggeringActor || run.repository?.full_name !== context.repo || run.display_title !== `gemini:${context.repo}:${context.pr}:${context.sha}:${context.dryRun === true ? 'dry' : 'live'}`) throw fixedError('RUN_ORIGIN_REFUSED');
  const files = await pages(`${root}/pulls/${context.pr}/files`, null, transport, 1);
  if (files.length !== pr.changed_files || files.length > config.maxFiles) throw fixedError('DIFF_INCOMPLETE');
  const trees = {};
  for (const revision of [snapshot.head, snapshot.base]) {
    const tree = await githubRead(`${root}/git/trees/${revision}?recursive=1`, transport);
    if (tree.truncated !== false || !Array.isArray(tree.tree)) throw fixedError('TREE_INCOMPLETE');
    trees[revision] = new Map(tree.tree.map(entry => [entry.path, entry]));
  }
  const filtered = files.map(f => {
    const old = trees[snapshot.base].get(f.previous_filename ?? f.filename);
    const current = trees[snapshot.head].get(f.filename);
    const entry = f.status === 'removed' ? old : current;
    const modes = [old, current].filter(Boolean);
    return { path: f.filename, previousPath: f.previous_filename, status: f.status, additions: f.additions, deletions: f.deletions, patch: f.patch, mode: entry?.type === 'blob' && modes.every(e => e.type === 'blob' && ['100644', '100755'].includes(e.mode)) ? entry.mode : undefined };
  });
  const ci = [];
  const revisions = [snapshot.head];
  if (sha(pr.merge_commit_sha)) {
    const merge = await githubRead(`${root}/commits/${pr.merge_commit_sha}`, transport);
    if (merge.parents?.length === 2 && merge.parents[0].sha === snapshot.base && merge.parents[1].sha === snapshot.head) { revisions.push(pr.merge_commit_sha); snapshot.merge = { sha: pr.merge_commit_sha, parents: [snapshot.base, snapshot.head] }; }
  }
  for (const revision of revisions) {
    const checks = await pages(`${root}/actions/workflows/ci.yml/runs?head_sha=${revision}`, 'workflow_runs', transport);
    for (const run of checks) if (run.head_sha === revision && run.path === '.github/workflows/ci.yml' && ['pull_request', 'push'].includes(run.event) && Number.isSafeInteger(run.id) && ['completed', 'queued', 'in_progress'].includes(run.status)) ci.push({ id: run.id, sha: revision, status: run.status, conclusion: ['success', 'failure', 'cancelled', 'skipped', 'timed_out', 'neutral', 'action_required', 'stale'].includes(run.conclusion) ? run.conclusion : null });
  }
  // The files endpoint is mutable: bind its result to a fresh public identity.
  await assertCurrentSnapshot(snapshot, transport);
  return { ...snapshot, historyComplete: true, files: filtered, ci };
}
async function assertCurrentSnapshot(snapshot, transport) {
  const root = `/repos/${snapshot.repo}`;
  const repo = await githubRead(root, transport);
  const pr = await githubRead(`${root}/pulls/${snapshot.pr}`, transport);
  if (repo.private !== false || repo.visibility !== 'public' || pr.number !== snapshot.pr || pr.state !== 'open' || pr.head?.repo?.private !== false || pr.base?.repo?.private !== false || pr.base.repo.full_name !== snapshot.repo || pr.head.sha !== snapshot.head || pr.base.sha !== snapshot.base) throw fixedError('SNAPSHOT_CHANGED');
}
const unsafe = /[`<>\x00-\x1f\x7f]|https?:|www\.|\[[^\]]*\]\(|(?:password|secret|api[_-]?key|access[_-]?token)["\']?\s*[:=]|\b(?:gh[pousr]_|github_pat_|AIza|AKIA|ASIA)|-----BEGIN|[A-Za-z0-9+/_=-]{40,}/i;
const safeText = (text, max) => typeof text === 'string' && text.length > 0 && text.length <= max && !unsafe.test(text);
export function validateResult(json, changedLines, snapshot) {
  try {
    const raw = typeof json === 'string' ? json : JSON.stringify(json);
    if (Buffer.byteLength(raw) > config.maxOutputBytes) return blocked();
    const result = typeof json === 'string' ? JSON.parse(json) : json;
    if (!exact(result, ['snapshot', 'coverage', 'summary', 'findings']) || !exact(result.snapshot, ids) || !sameSnapshot(snapshot, result.snapshot) || !['complete', 'incomplete'].includes(result.coverage) || !safeText(result.summary, 900) || !Array.isArray(result.findings) || result.findings.length > config.maxFindings) return blocked();
    for (const f of result.findings) if (!exact(f, ['severity', 'path', 'side', 'line', 'explanation']) || !['low', 'medium', 'high'].includes(f.severity) || !['LEFT', 'RIGHT'].includes(f.side) || !Number.isSafeInteger(f.line) || !Object.hasOwn(changedLines, f.path) || !changedLines[f.path][f.side].includes(f.line) || !safeText(f.explanation, 650)) return blocked();
    const prose = [result.summary, ...result.findings.map(f => f.explanation)].join(' ').toLowerCase();
    if ((snapshot.sourceLines ?? []).some(line => line.trim().length >= 12 && prose.includes(line.trim().toLowerCase()))) return blocked();
    return { state: 'ready', result: JSON.parse(JSON.stringify(result)) };
  } catch { return blocked(); }
}
function schema(snapshot) {
  const closed = properties => ({ type: 'object', properties, required: Object.keys(properties), additionalProperties: false });
  return closed({ snapshot: closed(Object.fromEntries(ids.map(k => [k, { type: typeof snapshot[k] === 'number' ? 'integer' : 'string', enum: [snapshot[k]] }]))), coverage: { type: 'string', enum: ['complete', 'incomplete'] }, summary: { type: 'string', maxLength: 900 }, findings: { type: 'array', maxItems: 3, items: closed({ severity: { type: 'string', enum: ['low', 'medium', 'high'] }, path: { type: 'string' }, side: { type: 'string', enum: ['LEFT', 'RIGHT'] }, line: { type: 'integer', minimum: 1 }, explanation: { type: 'string', maxLength: 650 } }) } });
}
function hasToolContent(value) {
  if (!value || typeof value !== 'object') return false;
  return Object.entries(value).some(([key, child]) => /^(?:functionCall|functionResponse|executableCode|codeExecutionResult|toolCalls|tools|thought|thoughtSignature)$/.test(key) || hasToolContent(child));
}
function sourceSnapshot(snapshot) {
  return { ...snapshot, sourceLines: snapshot.files.flatMap(f => f.patch.split('\n').filter(l => /^[ +-]/.test(l)).map(l => l.slice(1))) };
}
function usageSummary(metadata, counted, now) {
  const keys = ['promptTokenCount', 'candidatesTokenCount', 'thoughtsTokenCount', 'totalTokenCount'];
  const usage = Object.fromEntries(keys.filter(k => integer(metadata?.[k])).map(k => [k, metadata[k]]));
  const measured = keys.every(k => integer(metadata?.[k])) && metadata.promptTokenCount === counted && metadata.totalTokenCount === metadata.promptTokenCount + metadata.candidatesTokenCount + metadata.thoughtsTokenCount;
  usage.measurement = measured ? 'measured' : 'uncertain';
  const rates = now < Date.parse('2027-01-01T00:00:00Z') ? [.75, 3.75] : [1.5, 7.5];
  if (keys.slice(0, 3).every(k => integer(metadata?.[k]))) usage.estimatedCostUSD = (metadata.promptTokenCount * rates[0] + (metadata.candidatesTokenCount + metadata.thoughtsTokenCount) * rates[1]) / 1e6;
  return usage;
}
export async function generateOnce(input, transport = createTransport(), key) {
  try {
    if (transport[nativeTransport] && !config.liveEnabled && input.context?.dryRun !== true) return { state: 'blocked', reason: 'LIVE_DISABLED' };
    const current = await collectSnapshot(input.context, transport);
    if (!sameSnapshot(input.snapshot, current) || current.head !== input.context.sha) return { state: 'obsolete', reason: 'SNAPSHOT_CHANGED' };
    const prepared = prepareInput(current, current.files, current.ci);
    if (prepared.decision.state !== 'ready') return prepared.decision;
    if (input.prompt !== undefined && input.prompt !== prepared.prompt) return blocked();
    if (input.context.dryRun === true) return { state: 'ready', reason: 'DRY_RUN_VALIDATED' };
    if (typeof key !== 'string' || !key) return { state: 'blocked', reason: 'KEY_REQUIRED' };
    const body = { systemInstruction: { parts: [{ text: SYSTEM_INSTRUCTION }] }, contents: [{ role: 'user', parts: [{ text: prepared.prompt }] }], generationConfig: { candidateCount: 1, responseMimeType: 'application/json', responseJsonSchema: schema(current), thinkingConfig: { thinkingLevel: 'LOW', includeThoughts: false }, maxOutputTokens: config.maxOutputTokens } };
    const endpoint = `https://generativelanguage.googleapis.com/v1beta/models/${config.model}`;
    const options = { method: 'POST', headers: { 'Content-Type': 'application/json', 'x-goog-api-key': key } };
    const count = await request(`${endpoint}:countTokens`, { ...options, body: JSON.stringify({ generateContentRequest: { model: `models/${config.model}`, ...body } }), signal: AbortSignal.timeout(30000) }, transport, 16384);
    if (!integer(count.data?.totalTokens)) return { state: 'blocked', reason: 'TOKEN_COUNT_FAILED' };
    if (count.data.totalTokens > config.maxTotalTokens) return { state: 'incomplete', reason: 'TOKEN_LIMIT' };
    // Counting may take 30 seconds; refuse a newly stale or nonpublic snapshot.
    await assertCurrentSnapshot(current, transport);
    // This call is deliberately outside every retry loop, including transport errors.
    const response = await request(`${endpoint}:generateContent`, { ...options, body: JSON.stringify(body), signal: AbortSignal.timeout(config.generationTimeoutSeconds * 1000) }, transport, 65536);
    if (!response.data) return { state: 'blocked', reason: 'GENERATION_FAILED' };
    if (hasToolContent(response.data)) return blocked();
    const candidates = response.data.candidates;
    if (!Array.isArray(candidates) || candidates.length !== 1 || candidates[0].finishReason !== 'STOP' || candidates[0].content?.role && candidates[0].content.role !== 'model') return blocked();
    const parts = candidates[0].content?.parts;
    if (!Array.isArray(parts) || parts.length !== 1 || !exact(parts[0], ['text'])) return blocked();
    const decision = validateResult(parts[0].text, prepared.changedLines, sourceSnapshot(current));
    if (decision.state !== 'ready') return decision;
    return { ...decision, usage: usageSummary(response.data.usageMetadata, count.data.totalTokens, (transport.now ?? Date.now)()) };
  } catch (error) { return error.message === 'SNAPSHOT_CHANGED' ? { state: 'obsolete', reason: 'SNAPSHOT_CHANGED' } : { state: 'blocked', reason: 'REQUEST_FAILED' }; }
}
const escape = text => text.replace(/[\\`*_{}\[\]()#+.!|<>@]/g, c => `&#${c.charCodeAt(0)};`);
function sourcePath(finding, snapshot) {
  if (finding.side !== 'LEFT') return finding.path;
  const file = snapshot.files?.find(entry => entry.path === finding.path);
  if (!file || (file.status === 'renamed' && !file.previousPath)) throw fixedError('SOURCE_PATH_UNAVAILABLE');
  return file.previousPath ?? file.path;
}
export function renderSummary(result, snapshot) {
  const header = `Gemini advisory diagnosis — hypotheses requiring human verification.\n\nRepository: ${escape(snapshot.repo)} · PR #${snapshot.pr}\nHead: ${snapshot.head}\nBase: ${snapshot.base}\n\n`;
  const findings = result.findings.map(f => { const revision = f.side === 'LEFT' ? snapshot.base : snapshot.head; const path = sourcePath(f, snapshot).split('/').map(encodeURIComponent).join('/'); return `- ${f.severity}: [source line ${f.line}](https://github.com/${snapshot.repo}/blob/${revision}/${path}#L${f.line}) — ${escape(f.explanation)}`; }).join('\n');
  return (header + `Coverage: ${result.coverage}\n\n${escape(result.summary)}\n\n${findings}`).slice(0, config.maxSummaryCharacters);
}

// CLI: context is trusted dispatch metadata, never PR content. collect writes only
// IDs + decision. infer re-fetches all public inputs in memory; artifact has only
// validated result + numeric usage. No prompt/diff/response temporary is created.
async function atomicJSON(path, value) {
  const temporary = `${path}.tmp`;
  try { await writeFile(temporary, JSON.stringify(value), { mode: 0o600, flag: 'wx' }); await rename(temporary, path); }
  finally { await rm(temporary, { force: true }); }
}
// Native artifact downloads go into one fresh fixed directory. Refuse a
// different filename, extra entries, links, oversized data or unfiltered fields
// before the summary command can consume anything from it.
export async function readArtifactDirectory(directory) {
  const root = await lstat(directory);
  if (!root.isDirectory() || root.isSymbolicLink()) throw fixedError('ARTIFACT_REFUSED');
  const entries = await readdir(directory);
  if (entries.length !== 1 || entries[0] !== 'artifact.json') throw fixedError('ARTIFACT_REFUSED');
  const path = `${directory}/artifact.json`;
  const stat = await lstat(path);
  if (!stat.isFile() || stat.isSymbolicLink() || stat.nlink !== 1 || stat.size > 16384) throw fixedError('ARTIFACT_REFUSED');
  const artifact = JSON.parse(await readFile(path, 'utf8'));
  if (!object(artifact) || artifact.state !== 'ready' ||
      Object.keys(artifact).some(k => !['state', 'reason', 'result', 'usage', 'origin'].includes(k)) ||
      !exact(artifact.origin, [...ids, 'dryRun']) || typeof artifact.origin.dryRun !== 'boolean') throw fixedError('ARTIFACT_REFUSED');
  if (artifact.origin.dryRun) {
    if (!exact(artifact, ['state', 'reason', 'origin']) || artifact.reason !== 'DRY_RUN_VALIDATED') throw fixedError('ARTIFACT_REFUSED');
  } else {
    if (!exact(artifact, ['state', 'result', 'usage', 'origin']) || !exact(artifact.result, ['snapshot', 'coverage', 'summary', 'findings'])) throw fixedError('ARTIFACT_REFUSED');
    const usage = artifact.usage;
    const counts = ['promptTokenCount', 'candidatesTokenCount', 'thoughtsTokenCount', 'totalTokenCount'];
    if (!object(usage) || !['measured', 'uncertain'].includes(usage.measurement) ||
        Object.keys(usage).some(k => ![...counts, 'measurement', 'estimatedCostUSD'].includes(k)) ||
        counts.some(k => Object.hasOwn(usage, k) && !integer(usage[k])) ||
        (Object.hasOwn(usage, 'estimatedCostUSD') && (!Number.isFinite(usage.estimatedCostUSD) || usage.estimatedCostUSD < 0))) throw fixedError('ARTIFACT_REFUSED');
  }
  return artifact;
}
export async function runCLI(argv, { env = process.env, transport = createTransport() } = {}) {
  const [command, contextPath, inputPath, outputPath] = argv;
  try {
    if (!['collect', 'infer', 'summarize'].includes(command)) throw fixedError('COMMAND_REFUSED');
    const context = JSON.parse(await readFile(contextPath, 'utf8'));
    validateContext(context);
    if (command === 'collect') {
      const current = await collectSnapshot(context, transport);
      const permissions = {};
      for (const actor of new Set([context.actor, context.triggeringActor])) {
        const permissionTransport = { ...transport, fetch: (url, options) => transport.fetch(url, { ...options, headers: { ...options.headers, ...(env.GITHUB_TOKEN ? { Authorization: `Bearer ${env.GITHUB_TOKEN}` } : {}) } }) };
        const data = await githubRead(`/repos/${context.repo}/collaborators/${actor}/permission`, permissionTransport);
        permissions[actor] = ['maintain', 'admin'].includes(data.role_name) ? data.role_name : undefined;
      }
      const decision = authorize({ ...context, historyComplete: current.historyComplete }, { number: current.pr, state: 'open', isPublic: true, head: current.head, base: current.base }, permissions);
      if (decision.state !== 'ready') return decision;
      const prepared = prepareInput(current, current.files, current.ci);
      if (prepared.decision.state !== 'ready') return prepared.decision;
      await atomicJSON(inputPath, { snapshot: identity(current), decision, dryRun: context.dryRun === true });
      return { state: 'ready', reason: 'COLLECTED' };
    }
    const artifact = JSON.parse(await readFile(inputPath, 'utf8'));
    if (command === 'infer') {
      if (artifact.decision?.state !== 'ready' || artifact.dryRun !== (context.dryRun === true)) return blocked();
      if (!config.liveEnabled && context.dryRun !== true) return { state: 'blocked', reason: 'LIVE_DISABLED' };
      const result = await generateOnce({ context, snapshot: artifact.snapshot }, transport, context.dryRun === true ? undefined : env.GEMINI_API_KEY);
      if (result.state === 'ready') await atomicJSON(outputPath, { ...result, origin: { ...identity(artifact.snapshot), dryRun: context.dryRun === true } });
      return { state: result.state, reason: result.reason ?? 'INFERRED' };
    }
    const current = await collectSnapshot(context, transport);
    if (!sameSnapshot(artifact.origin, current) || artifact.origin.dryRun !== (context.dryRun === true) || current.head !== context.sha || artifact.state !== 'ready') return { state: 'obsolete', reason: 'ORIGIN_CHANGED' };
    const prepared = prepareInput(current, current.files, current.ci);
    if (prepared.decision.state !== 'ready') return prepared.decision;
    const dry = context.dryRun === true;
    const decision = dry && artifact.reason === 'DRY_RUN_VALIDATED' && artifact.result === undefined ? { state: 'ready' } : validateResult(artifact.result, prepared.changedLines, sourceSnapshot(current));
    if (decision.state !== 'ready') return decision;
    // Re-query immediately before publication after all validation work.
    const latest = await githubRead(`/repos/${context.repo}/pulls/${context.pr}`, transport);
    if (latest.head?.sha !== current.head || latest.base?.sha !== current.base || latest.state !== 'open') return { state: 'obsolete', reason: 'SNAPSHOT_CHANGED' };
    await appendFile(env.GITHUB_STEP_SUMMARY, dry ? renderSummary({ coverage: 'complete', summary: 'Dry run validated public input. No model diagnosis was requested.', findings: [] }, current) : renderSummary(decision.result, current));
    return { state: 'ready', reason: 'SUMMARIZED' };
  } catch { return { state: 'blocked', reason: 'CLI_FAILED' }; }
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const result = await runCLI(process.argv.slice(2));
  process.stdout.write(`${JSON.stringify(result)}\n`);
  if (result.state !== 'ready') process.exitCode = 1;
}
