import { readFile, writeFile, rename, rm, lstat, readdir, realpath } from 'node:fs/promises';
import { pathToFileURL } from 'node:url';
import { config, SYSTEM_INSTRUCTION, eligibility, fingerprint, sanitizeProse, prepareInput, responseSchema, validateProposal, planEffects } from './issue-core.mjs';

const workflow = '.github/workflows/issue-triage.yml';
const generationStep = 'Generate Gemini proposal';
const keys = ['repo', 'issueNumber', 'issueId', 'contentHash', 'workflowSha', 'runId'];
const contextKeys = ['repo', 'issueNumber', 'issueId', 'event', 'ref', 'defaultBranch', 'workflowSha', 'runId', 'runAttempt', 'actor', 'dryRun'];
const record = x => x !== null && typeof x === 'object' && !Array.isArray(x);
const exact = (x, ks) => record(x) && Object.keys(x).length === ks.length && ks.every(k => Object.hasOwn(x, k));
const positive = n => Number.isSafeInteger(n) && n > 0;
const integer = n => Number.isSafeInteger(n) && n >= 0;
const sha = s => typeof s === 'string' && /^[a-f0-9]{40}$/.test(s);
const hash = s => typeof s === 'string' && /^[a-f0-9]{64}$/.test(s);
const fail = code => { throw new Error(code); };
const result = (reason, state = 'blocked') => ({ state, reason });
const same = (a, b) => exact(a, keys) && exact(b, keys) && keys.every(k => a[k] === b[k]);
const wait = ms => new Promise(resolve => setTimeout(resolve, ms));
export function createTransport(fetchImpl = globalThis.fetch, { now = Date.now, sleep = wait } = {}) { return { fetch: fetchImpl, now, sleep }; }

function validateContext(c) {
  if (!exact(c, contextKeys) || !config.repositories.includes(c.repo) || !positive(c.issueNumber) || !positive(c.issueId) || !positive(c.runId) || c.runAttempt !== 1 || !sha(c.workflowSha) || typeof c.defaultBranch !== 'string' || !/^[A-Za-z0-9_./-]+$/.test(c.defaultBranch) || c.ref !== `refs/heads/${c.defaultBranch}` || !/^[A-Za-z0-9-]+$/.test(c.actor) || typeof c.dryRun !== 'boolean' || !['opened', 'manualdry'].includes(c.event) || (c.event === 'manualdry' && !c.dryRun) || (c.event === 'opened' && c.dryRun)) fail('CONTEXT_REFUSED');
}
function validateSnapshot(s) { if (!exact(s, keys) || !config.repositories.includes(s.repo) || !positive(s.issueNumber) || !positive(s.issueId) || !positive(s.runId) || !hash(s.contentHash) || !sha(s.workflowSha)) fail('ARTIFACT_REFUSED'); }
function titleFor(c) { return c.event === 'manualdry' ? `issue-triage:${c.repo}:dry` : `issue-triage:${c.repo}:${c.issueNumber}:${c.issueId}:live`; }
function githubURL(path) {
  const prefix = `/repos/${config.repositories[0]}`;
  const suffix = path.split('?')[0].slice(prefix.length);
  if (path.startsWith(prefix) && /^(?:|\/issues\/[1-9]\d*(?:\/issue-field-values|\/comments|\/labels)?|\/issues\/comments\/[1-9]\d*|\/issue-types|\/labels|\/actions\/workflows\/issue-triage\.yml\/runs|\/actions\/runs\/[1-9]\d*(?:\/jobs)?|\/collaborators\/[A-Za-z0-9-]+\/permission)$/.test(suffix)) {
    const u = new URL(`https://api.github.com${path}`);
    if (u.hash || /[%\\\x00-\x20]/.test(path.split('?')[0])) fail('ROUTE_REFUSED');
    for (const [k, v] of u.searchParams) if (!['page', 'per_page'].includes(k) || !/^[1-9]\d*$/.test(v) || Number(v) > 100) fail('ROUTE_REFUSED');
    return u.href;
  }
  if (path.startsWith('/search/issues?')) {
    const u = new URL(`https://api.github.com${path}`), params = Object.fromEntries(u.searchParams);
    if (Object.keys(params).length !== 4 || params.q !== `repo:${config.repositories[0]} is:issue` || params.sort !== 'updated' || params.order !== 'desc' || params.per_page !== String(config.maxCandidates)) fail('ROUTE_REFUSED');
    return u.href;
  }
  fail('ROUTE_REFUSED');
}
async function request(url, options, transport, statuses, cap = 2 * 1024 * 1024) {
  const u = new URL(url);
  if (u.protocol !== 'https:' || u.port || u.username || u.password || u.hash || !['api.github.com', 'generativelanguage.googleapis.com'].includes(u.hostname)) fail('ROUTE_REFUSED');
  let response;
  try { response = await transport.fetch(u.href, { ...options, redirect: 'error' }); } catch { fail('REQUEST_FAILED'); }
  if (!statuses.includes(response.status)) { await response.body?.cancel(); return { status: response.status, retryAfter: response.headers.get('retry-after') }; }
  try {
    let size = 0; const chunks = [];
    for await (const chunk of response.body) { size += chunk.byteLength; if (size > cap) fail('RESPONSE_LIMIT'); chunks.push(chunk); }
    return { status: response.status, data: JSON.parse(Buffer.concat(chunks).toString('utf8')) };
  } catch { fail('INVALID_RESPONSE'); }
}
function headers(token) { return { Accept: 'application/vnd.github+json', 'X-GitHub-Api-Version': config.apiVersion, ...(token ? { Authorization: `Bearer ${token}` } : {}) }; }
async function githubRead(path, transport, token) {
  const url = githubURL(path);
  for (let attempt = 0; attempt < config.githubReadAttempts; attempt++) {
    let response;
    try { response = await request(url, { method: 'GET', headers: headers(token), signal: AbortSignal.timeout(30000) }, transport, [200]); } catch { response = { status: 503 }; }
    if (response.data !== undefined) return response.data;
    if (![429, 500, 502, 503, 504].includes(response.status) || attempt === config.githubReadAttempts - 1) fail('GITHUB_READ_FAILED');
    const delay = response.retryAfter ? /^\d+$/.test(response.retryAfter) ? Number(response.retryAfter) * 1000 : Date.parse(response.retryAfter) - transport.now() : 1000 * 2 ** attempt;
    if (!Number.isFinite(delay) || delay > 30000) fail('GITHUB_READ_FAILED');
    await transport.sleep(Math.max(0, delay));
  }
}
async function pages(path, field, transport, token) {
  const rows = []; let total;
  for (let page = 1; page <= 10; page++) {
    const data = await githubRead(`${path}?per_page=100&page=${page}`, transport, token), batch = field ? data[field] : data;
    if (!Array.isArray(batch) || batch.length > 100 || (field && (!integer(data.total_count) || (total !== undefined && total !== data.total_count)))) fail('HISTORY_INCOMPLETE');
    if (field) total = data.total_count;
    rows.push(...batch);
    if (batch.length < 100) { if (field && total !== rows.length) fail('HISTORY_INCOMPLETE'); return rows; }
  }
  fail('HISTORY_INCOMPLETE');
}
function provenance(run, c, current = false) {
  if (!record(run) || !positive(run.id) || run.run_attempt !== 1 || run.path !== workflow || !sha(run.head_sha) || run.head_branch !== c.defaultBranch || run.repository?.full_name !== c.repo || !['completed', 'queued', 'in_progress'].includes(run.status)) fail('HISTORY_INCOMPLETE');
  const dry = run.display_title === `issue-triage:${c.repo}:dry`;
  const match = new RegExp(`^issue-triage:${c.repo}:([1-9]\\d*):([1-9]\\d*):live$`).exec(run.display_title ?? '');
  if (dry ? run.event !== 'workflow_dispatch' : !match || run.event !== 'issues' || !positive(Number(match[1])) || !positive(Number(match[2]))) fail('HISTORY_INCOMPLETE');
  if (current && (run.id !== c.runId || run.head_sha !== c.workflowSha || run.actor?.login !== c.actor || run.display_title !== titleFor(c) || dry !== c.dryRun || run.status !== 'in_progress')) fail('RUN_ORIGIN_REFUSED');
  return { dry, number: match ? Number(match[1]) : null, id: match ? Number(match[2]) : null };
}
async function currentRun(c, transport, token) {
  provenance(await githubRead(`/repos/${c.repo}/actions/runs/${c.runId}`, transport, token), c, true);
}
async function history(c, transport, token, admit = true) {
  const root = `/repos/${c.repo}`;
  await currentRun(c, transport, token);
  const runs = await pages(`${root}/actions/workflows/issue-triage.yml/runs`, 'workflow_runs', transport, token);
  if (new Set(runs.map(r => r?.id)).size !== runs.length || !runs.some(r => r.id === c.runId)) fail('HISTORY_INCOMPLETE');
  const reservations = [];
  for (const run of runs) {
    const p = provenance(run, c, run.id === c.runId);
    if (run.id === c.runId) continue;
    if (run.status === 'in_progress') fail('CONCURRENT_RUN');
    if (p.dry || run.status === 'queued') continue;
    const jobs = await pages(`${root}/actions/runs/${run.id}/jobs`, 'jobs', transport, token);
    if (!jobs.length || jobs.some(j => !Array.isArray(j.steps))) fail('HISTORY_INCOMPLETE');
    const steps = jobs.flatMap(j => j.steps).filter(s => s.name === generationStep);
    if (steps.length !== 1 || steps[0].status !== 'completed' || typeof steps[0].conclusion !== 'string') fail('HISTORY_INCOMPLETE');
    const step = steps[0]; if (step.conclusion === 'skipped') continue;
    const started = Date.parse(step.started_at);
    if (!Number.isFinite(started) || started > transport.now()) fail('HISTORY_INCOMPLETE');
    reservations.push({ ...p, started });
  }
  if (!admit || c.dryRun) return;
  if (reservations.some(r => r.number === c.issueNumber || r.id === c.issueId)) fail('GENERATION_RESERVED');
  const now = transport.now(), month = new Date(now); month.setUTCDate(1); month.setUTCHours(0, 0, 0, 0);
  if (reservations.filter(r => r.started > now - 3600000).length >= config.maxHourlyGenerations || reservations.filter(r => r.started >= month.getTime()).length >= config.maxMonthlyGenerations) fail('BUDGET_EXHAUSTED');
}
function botComment(c) { return positive(c.id) && c.user?.login === 'github-actions[bot]' && c.user?.type === 'Bot' && c.user?.id === 41898282; }
function markerComments(comments, s) {
  const marker = `<!-- cubeship-issue-triage:${s.contentHash} -->`, found = comments.filter(c => typeof c.body === 'string' && c.body.includes(marker));
  if (found.some(c => !botComment(c)) || found.length > 1) fail('MARKER_REFUSED');
  return found[0];
}
async function current(c, transport, token) {
  const root = `/repos/${c.repo}`, repo = await githubRead(root, transport, token), issue = await githubRead(`${root}/issues/${c.issueNumber}`, transport, token);
  let actorPermission;
  if (c.event === 'manualdry') { const permission = await githubRead(`${root}/collaborators/${c.actor}/permission`, transport, token); actorPermission = permission.role_name; }
  const decision = eligibility({ ...c, actorPermission }, repo, issue);
  if (decision.state !== 'ready') fail(decision.reason);
  return { repo, issue, snapshot: { repo: c.repo, issueNumber: issue.number, issueId: issue.id, contentHash: fingerprint(issue), workflowSha: c.workflowSha, runId: c.runId } };
}
async function catalog(c, transport, token, verifiedTypes) {
  const root = `/repos/${c.repo}`, types = verifiedTypes ?? await pages(`${root}/issue-types`, null, transport, token), labels = await pages(`${root}/labels`, null, transport, token);
  // Native Priority identities were verified out of band and pinned in trusted
  // source. Repository-token runtime needs neither organization nor Projects scope.
  return { types: types.map(t => ({ id: t.id, name: t.name })), priority: structuredClone(config.priority), labels: labels.map(l => l.name).filter(name => config.labels.includes(name)) };
}
function excerpt(prose) {
  // Explicitly choose the first 400 UTF-8 bytes only after scanning the full body.
  let value = ''; for (const char of prose) { if (Buffer.byteLength(value + char) > config.maxCandidateBytes) break; value += char; } return value;
}
async function candidate(c, number, transport, token, expected) {
  // prepared established this repository's public identity before candidate reads.
  const issue = await githubRead(`/repos/${c.repo}/issues/${number}`, transport, token);
  if (issue.number !== number || !positive(issue.id) || issue.id === c.issueId || number === c.issueNumber || !['open', 'closed'].includes(issue.state) || Object.hasOwn(issue, 'pull_request') || !hash(fingerprint(issue))) fail('CANDIDATE_REFUSED');
  const title = sanitizeProse(issue.title), body = issue.body ? sanitizeProse(issue.body) : { state: 'ready', prose: '' };
  if (title.state !== 'ready' || body.state !== 'ready') fail('CANDIDATE_REFUSED');
  const contentHash = fingerprint(issue);
  if (expected && (issue.id !== expected.id || contentHash !== expected.expectedContentHash || expected.repo !== c.repo)) fail('CANDIDATE_CHANGED');
  return { repo: c.repo, number, id: issue.id, isPublic: true, state: issue.state, contentHash, expectedContentHash: expected?.expectedContentHash ?? contentHash, title: title.prose, excerpt: excerpt(body.prose) };
}
async function candidates(c, transport, token, expected) {
  if (expected) {
    if (!Array.isArray(expected) || expected.length > config.maxCandidates || new Set(expected.map(x => x.number)).size !== expected.length || expected.some(x => !exact(x, ['repo', 'number', 'id', 'expectedContentHash']) || x.repo !== c.repo || !positive(x.number) || !positive(x.id) || !hash(x.expectedContentHash))) fail('ARTIFACT_REFUSED');
    const out = []; for (const e of expected) out.push(await candidate(c, e.number, transport, token, e)); return out;
  }
  const search = await githubRead(`/search/issues?${new URLSearchParams({ q: `repo:${c.repo} is:issue`, sort: 'updated', order: 'desc', per_page: String(config.maxCandidates) })}`, transport, token);
  if (!integer(search.total_count) || search.incomplete_results !== false || !Array.isArray(search.items) || search.items.length > config.maxCandidates || search.items.length !== Math.min(search.total_count, config.maxCandidates)) fail('CANDIDATE_SEARCH_INCOMPLETE');
  const out = [];
  for (const item of search.items) {
    if (!positive(item.number) || !positive(item.id) || Object.hasOwn(item, 'pull_request') || item.repository_url !== `https://api.github.com/repos/${c.repo}`) fail('CANDIDATE_REFUSED');
    if (item.number === c.issueNumber && item.id === c.issueId) continue;
    const fresh = await candidate(c, item.number, transport, token);
    if (fresh.id !== item.id) fail('CANDIDATE_REFUSED'); out.push(fresh);
  }
  return out;
}
function candidateMetadata(cs) { return cs.map(c => ({ repo: c.repo, number: c.number, id: c.id, expectedContentHash: c.expectedContentHash })); }
async function prepared(c, transport, token, expectedSnapshot, expectedCandidates, verifiedTypes) {
  const fresh = await current(c, transport, token);
  if (expectedSnapshot && !same(expectedSnapshot, fresh.snapshot)) fail('SNAPSHOT_CHANGED');
  const cs = await candidates(c, transport, token, expectedCandidates), cat = await catalog(c, transport, token, verifiedTypes);
  const input = prepareInput({ ...fresh.snapshot, issue: fresh.issue }, cs, cat);
  if (input.decision.state !== 'ready') fail(input.decision.reason);
  return { ...input, context: c, issue: fresh.issue };
}
export async function collectSnapshot(c, transport = createTransport(), env = {}) {
  if (!config.enabled && !c?.dryRun) return result('DISABLED');
  try {
    validateContext(c); const token = env.GITHUB_TOKEN;
    await history(c, transport, token);
    const input = await prepared(c, transport, token);
    const comments = await pages(`/repos/${c.repo}/issues/${c.issueNumber}/comments`, null, transport, token);
    if (markerComments(comments, input.snapshot)) return result('ALREADY_PUBLISHED');
    await currentUnchanged(input, transport, token);
    return { state: 'ready', reason: 'COLLECTED', context: c, snapshot: input.snapshot, candidates: candidateMetadata(input.candidates) };
  } catch (e) { return safeFailure(e); }
}
async function currentUnchanged(input, transport, token) { const fresh = await current(input.context, transport, token); if (!same(input.snapshot, fresh.snapshot)) fail('SNAPSHOT_CHANGED'); return fresh.issue; }
const reasons = new Set(['CONTEXT_REFUSED', 'ARTIFACT_REFUSED', 'ROUTE_REFUSED', 'REQUEST_FAILED', 'INVALID_RESPONSE', 'GITHUB_READ_FAILED', 'HISTORY_INCOMPLETE', 'CONCURRENT_RUN', 'RUN_ORIGIN_REFUSED', 'GENERATION_RESERVED', 'BUDGET_EXHAUSTED', 'MARKER_REFUSED', 'PUBLIC_REPOSITORY_REQUIRED', 'UNTRUSTED_CONTEXT', 'UNTRUSTED_EVENT', 'OPEN_HUMAN_ISSUE_REQUIRED', 'CANDIDATE_REFUSED', 'CANDIDATE_CHANGED', 'CANDIDATE_SEARCH_INCOMPLETE', 'SNAPSHOT_CHANGED', 'INVALID_INPUT_METADATA', 'INVALID_PRIMARY_INPUT', 'UNSAFE_PRIMARY_INPUT', 'UNSAFE_CANDIDATE', 'INVALID_CANDIDATE', 'SERIALIZED_INPUT_TOO_LARGE', 'WRITE_UNCONFIRMED', 'CURRENT_METADATA_REFUSED', 'CREDENTIAL_ISOLATION']);
function safeFailure(e) { return result(reasons.has(e?.message) ? e.message : 'REQUEST_FAILED'); }
function inputArtifact(a) { if (!exact(a, ['state', 'reason', 'context', 'snapshot', 'candidates']) || a.state !== 'ready' || a.reason !== 'COLLECTED') fail('ARTIFACT_REFUSED'); validateContext(a.context); validateSnapshot(a.snapshot); if (keys.filter(k => k !== 'contentHash').some(k => a.snapshot[k] !== a.context[k])) fail('ARTIFACT_REFUSED'); }
function usage(metadata, counted) {
  const fields = ['promptTokenCount', 'candidatesTokenCount', 'thoughtsTokenCount', 'totalTokenCount'], u = Object.fromEntries(fields.filter(k => integer(metadata?.[k])).map(k => [k, metadata[k]]));
  u.measurement = fields.every(k => integer(metadata?.[k])) && metadata.promptTokenCount === counted && metadata.totalTokenCount === metadata.promptTokenCount + metadata.candidatesTokenCount + metadata.thoughtsTokenCount ? 'measured' : 'uncertain';
  if (fields.slice(0, 3).every(k => integer(metadata?.[k]))) u.estimatedCostUSD = (metadata.promptTokenCount * config.inputUSDPerMillionTokens + (metadata.candidatesTokenCount + metadata.thoughtsTokenCount) * config.outputUSDPerMillionTokens) / 1e6;
  return u;
}
function toolContent(value) {
  return value && typeof value === 'object' && Object.entries(value).some(([key, child]) => /^(?:thought|functionCall|functionResponse|executableCode|codeExecutionResult|toolCalls|tools)$/.test(key) || toolContent(child));
}
function finalText(data) {
  if (toolContent(data)) fail('INVALID_RESPONSE');
  if (!record(data) || Object.keys(data).some(k => !['candidates', 'usageMetadata', 'modelVersion', 'responseId'].includes(k)) || !Array.isArray(data.candidates) || data.candidates.length !== 1) fail('INVALID_RESPONSE');
  const c = data.candidates[0];
  if (!record(c) || Object.keys(c).some(k => !['content', 'finishReason', 'index', 'safetyRatings'].includes(k)) || c.finishReason !== 'STOP' || !exact(c.content, ['role', 'parts']) || c.content.role !== 'model' || !Array.isArray(c.content.parts) || c.content.parts.length !== 1) fail('INVALID_RESPONSE');
  const p = c.content.parts[0];
  if ((!exact(p, ['text']) && !exact(p, ['text', 'thoughtSignature'])) || typeof p.text !== 'string' || Buffer.byteLength(p.text) > config.maxOutputBytes || (Object.hasOwn(p, 'thoughtSignature') && (typeof p.thoughtSignature !== 'string' || !p.thoughtSignature || Buffer.byteLength(p.thoughtSignature) > 16384))) fail('INVALID_RESPONSE');
  return p.text;
}
export async function generateOnce(a, transport = createTransport(), key) {
  if (!config.enabled && !a?.context?.dryRun) return result('DISABLED');
  try {
    inputArtifact(a);
    const c = a.context;
    // The serialized trusted collector admitted this exact same-run artifact.
    await currentRun(c, transport);
    // GitHub requires authentication for types; this same-run collector verified the pinned catalog.
    const input = await prepared(c, transport, undefined, a.snapshot, a.candidates, config.types);
    if (c.dryRun) return { state: 'ready', reason: 'DRY_RUN_VALIDATED' };
    if (typeof key !== 'string' || !key) return result('KEY_REQUIRED');
    const body = { systemInstruction: { parts: [{ text: SYSTEM_INSTRUCTION }] }, contents: [{ role: 'user', parts: [{ text: input.prompt }] }], generationConfig: { candidateCount: 1, responseMimeType: 'application/json', responseJsonSchema: responseSchema(input), maxOutputTokens: config.maxOutputTokens, thinkingConfig: { includeThoughts: false } } };
    const base = `https://generativelanguage.googleapis.com/v1beta/models/${config.model}`, options = { method: 'POST', headers: { 'Content-Type': 'application/json', 'x-goog-api-key': key } };
    await currentUnchanged(input, transport);
    const count = await request(`${base}:countTokens`, { ...options, body: JSON.stringify({ generateContentRequest: { model: `models/${config.model}`, ...body } }), signal: AbortSignal.timeout(config.countTimeoutSeconds * 1000) }, transport, [200], 16384);
    if (!integer(count.data?.totalTokens)) return result('TOKEN_COUNT_FAILED');
    if (count.data.totalTokens > config.maxInputTokens) return result('TOKEN_LIMIT');
    await currentUnchanged(input, transport);
    const response = await request(`${base}:generateContent`, { ...options, body: JSON.stringify(body), signal: AbortSignal.timeout(config.generationTimeoutSeconds * 1000) }, transport, [200], 65536);
    if (!response.data) return { ...result('GENERATION_FAILED'), httpStatus: response.status };
    const decision = validateProposal(finalText(response.data), input);
    if (decision.state !== 'ready') return decision;
    return { state: 'ready', reason: 'INFERRED', proposal: decision.proposal, usage: usage(response.data.usageMetadata, count.data.totalTokens) };
  } catch (e) { return safeFailure(e); }
}
function fields(raw) {
  if (!Array.isArray(raw)) fail('CURRENT_METADATA_REFUSED');
  const ids = new Set();
  return raw.map(v => {
    if (!record(v) || !positive(v.issue_field_id) || ids.has(v.issue_field_id) || typeof v.issue_field_name !== 'string' || typeof v.data_type !== 'string' || !Object.hasOwn(v, 'value')) fail('CURRENT_METADATA_REFUSED');
    ids.add(v.issue_field_id);
    if (v.issue_field_id === config.priority.id && (v.issue_field_name !== config.priority.name || v.data_type !== 'single_select' || !config.priority.options.some(o => o.id === v.value && o.id === v.single_select_option?.id && o.name === v.single_select_option?.name))) fail('CURRENT_METADATA_REFUSED');
    return { field_id: v.issue_field_id, value: v.single_select_option?.name ?? v.value, native: structuredClone(v) };
  });
}
async function publicationCurrent(input, transport, token) {
  const issue = await currentUnchanged(input, transport, token);
  const cat = await catalog(input.context, transport, token);
  if (prepareInput({ ...input.snapshot, issue }, input.candidates, cat).decision.state !== 'ready') fail('CURRENT_METADATA_REFUSED');
  if (!Object.hasOwn(issue, 'type') || (issue.type !== null && !config.types.some(t => t.id === issue.type?.id && t.name === issue.type?.name)) || !Array.isArray(issue.labels) || issue.labels.some(l => typeof l?.name !== 'string')) fail('CURRENT_METADATA_REFUSED');
  const values = fields(await pages(`/repos/${input.context.repo}/issues/${input.context.issueNumber}/issue-field-values`, null, transport, token));
  return { ...issue, repo: input.context.repo, fieldValues: values };
}
async function mutation(path, method, body, transport, token, statuses) {
  if (!token || !['PATCH', 'POST'].includes(method)) fail('CREDENTIAL_ISOLATION');
  const response = await request(githubURL(path), { method, headers: { ...headers(token), 'Content-Type': 'application/json' }, body: JSON.stringify(body), signal: AbortSignal.timeout(30000) }, transport, statuses);
  if (!response.data) fail('WRITE_UNCONFIRMED');
  return response.data;
}
function unchangedHuman(before, after) {
  if ((before.type !== null && (before.type.id !== after.type?.id || before.type.name !== after.type?.name)) || before.title !== after.title || before.body !== after.body || before.state !== after.state || before.labels.some(l => !after.labels.some(a => a.name === l.name)) || before.fieldValues.some(v => !after.fieldValues.some(a => a.field_id === v.field_id && JSON.stringify(a.native) === JSON.stringify(v.native)))) fail('WRITE_UNCONFIRMED');
}
export async function publishProposal(proposal, a, transport = createTransport(), env = {}) {
  if (!config.enabled || !config.publishEnabled) return result('DISABLED');
  if (a?.context?.dryRun) return result('DRY_RUN_VALIDATED', 'ready');
  try {
    if (Object.hasOwn(env, 'GEMINI_API_KEY')) fail('CREDENTIAL_ISOLATION');
    inputArtifact(a); const token = env.GITHUB_TOKEN; if (typeof token !== 'string' || !token) fail('CREDENTIAL_ISOLATION');
    await currentRun(a.context, transport, token);
    const input = await prepared(a.context, transport, token, a.snapshot, a.candidates);
    const checked = validateProposal(proposal, input); if (checked.state !== 'ready') return checked;
    const root = `/repos/${a.context.repo}/issues/${a.context.issueNumber}`, comments = await pages(`${root}/comments`, null, transport, token), prior = markerComments(comments, input.snapshot);
    if (prior) return result('ALREADY_PUBLISHED', 'ready');
    let before;
    for (const kind of ['type', 'priority', 'labels']) {
      before = await publicationCurrent(input, transport, token);
      const effects = planEffects(checked.proposal, before, input.candidates);
      let body, path, method;
      if (kind === 'type' && effects.typeToSet) { body = { type: effects.typeToSet }; path = root; method = 'PATCH'; }
      if (kind === 'priority' && effects.priorityToSet) { body = { issue_field_values: [effects.priorityToSet] }; path = `${root}/issue-field-values`; method = 'POST'; }
      if (kind === 'labels' && effects.labelsToAdd.length) { body = { labels: effects.labelsToAdd }; path = `${root}/labels`; method = 'POST'; }
      if (!body) continue;
      try { await mutation(path, method, body, transport, token, [200]); } catch { fail('WRITE_UNCONFIRMED'); }
      const after = await publicationCurrent(input, transport, token); unchangedHuman(before, after);
      if ((kind === 'type' && (after.type?.name !== body.type || !config.types.some(t => t.id === after.type?.id && t.name === body.type))) || (kind === 'priority' && !after.fieldValues.some(v => v.field_id === config.priority.id && v.value === body.issue_field_values[0].value)) || (kind === 'labels' && body.labels.some(name => !after.labels.some(l => l.name === name)))) fail('WRITE_UNCONFIRMED');
    }
    // Refresh original candidate expectations immediately before the comment.
    const final = await prepared(a.context, transport, token, a.snapshot, a.candidates), latest = await publicationCurrent(final, transport, token), effects = planEffects(checked.proposal, latest, final.candidates);
    if (!effects.comment) return result('PUBLISHED', 'ready');
    const existing = markerComments(await pages(`${root}/comments`, null, transport, token), input.snapshot);
    if (existing) return result('ALREADY_PUBLISHED', 'ready');
    await currentUnchanged(final, transport, token);
    let receipt;
    try { receipt = await mutation(`${root}/comments`, 'POST', { body: effects.comment }, transport, token, [201]); } catch { fail('WRITE_UNCONFIRMED'); }
    if (!botComment(receipt) || receipt.body !== effects.comment) fail('WRITE_UNCONFIRMED');
    const confirmed = markerComments(await pages(`${root}/comments`, null, transport, token), input.snapshot);
    if (!confirmed || confirmed.id !== receipt.id || confirmed.body !== effects.comment) fail('WRITE_UNCONFIRMED');
    return result('PUBLISHED', 'ready');
  } catch (e) { return safeFailure(e); }
}
async function atomicJSON(path, value) {
  const temporary = `${path}.tmp`;
  try { await writeFile(temporary, JSON.stringify(value), { mode: 0o600, flag: 'wx' }); await rename(temporary, path); } finally { await rm(temporary, { force: true }); }
}
async function readJSON(path, cap = 32768) { const stat = await lstat(path); if (!stat.isFile() || stat.isSymbolicLink() || stat.nlink !== 1 || stat.size > cap) fail('ARTIFACT_REFUSED'); return JSON.parse(await readFile(path, 'utf8')); }
export async function readArtifactDirectory(directory) {
  const stat = await lstat(directory), entries = await readdir(directory);
  if (!stat.isDirectory() || stat.isSymbolicLink() || entries.length !== 1 || entries[0] !== 'artifact.json') fail('ARTIFACT_REFUSED');
  return readJSON(`${directory}/artifact.json`);
}
async function readTransfer(path) { return (await lstat(path)).isDirectory() ? readArtifactDirectory(path) : readJSON(path); }
function proposalArtifact(a) {
  if (!exact(a, ['state', 'reason', 'origin', 'proposal', 'usage']) || a.state !== 'ready' || a.reason !== 'INFERRED') fail('ARTIFACT_REFUSED');
  inputArtifact(a.origin);
  const ks = ['promptTokenCount', 'candidatesTokenCount', 'thoughtsTokenCount', 'totalTokenCount'];
  if (!record(a.usage) || !['measured', 'uncertain'].includes(a.usage.measurement) || Object.keys(a.usage).some(k => ![...ks, 'measurement', 'estimatedCostUSD'].includes(k)) || ks.some(k => Object.hasOwn(a.usage, k) && !integer(a.usage[k])) || (Object.hasOwn(a.usage, 'estimatedCostUSD') && (!Number.isFinite(a.usage.estimatedCostUSD) || a.usage.estimatedCostUSD < 0))) fail('ARTIFACT_REFUSED');
}
export async function runCLI(argv, { env = process.env, transport = createTransport() } = {}) {
  const [command, contextPath, inputPath, outputPath] = argv;
  try {
    if (!['collect', 'infer', 'publish'].includes(command) || argv.length !== (command === 'infer' ? 4 : 3)) return result('COMMAND_REFUSED');
    const c = await readJSON(contextPath, 4096); validateContext(c);
    if ((!config.enabled && !c.dryRun) || (command === 'publish' && (!config.enabled || !config.publishEnabled))) return result('DISABLED');
    if (command === 'collect') { const a = await collectSnapshot(c, transport, env); if (a.state === 'ready') await atomicJSON(inputPath, a); return result(a.reason, a.state); }
    if (command === 'infer') {
      if (Object.hasOwn(env, 'GITHUB_TOKEN')) return result('CREDENTIAL_ISOLATION');
      const a = await readTransfer(inputPath); inputArtifact(a); if (contextKeys.some(k => c[k] !== a.context[k])) fail('ARTIFACT_REFUSED');
      const generated = await generateOnce(a, transport, c.dryRun ? undefined : env.GEMINI_API_KEY);
      if (generated.state === 'ready') await atomicJSON(outputPath, { ...generated, origin: a });
      return { ...result(generated.reason, generated.state), ...(integer(generated.httpStatus) && generated.httpStatus >= 100 && generated.httpStatus <= 599 ? { httpStatus: generated.httpStatus } : {}) };
    }
    if (c.dryRun) return result('DRY_RUN_VALIDATED', 'ready');
    const artifact = await readTransfer(inputPath); proposalArtifact(artifact); if (contextKeys.some(k => c[k] !== artifact.origin.context[k])) fail('ARTIFACT_REFUSED');
    return publishProposal(artifact.proposal, artifact.origin, transport, env);
  } catch (e) { return safeFailure(e); }
}
if (process.argv[1] && import.meta.url === pathToFileURL(await realpath(process.argv[1])).href) {
  const decision = await runCLI(process.argv.slice(2)); process.stdout.write(`${JSON.stringify(decision)}\n`); if (decision.state !== 'ready') process.exitCode = 1;
}
