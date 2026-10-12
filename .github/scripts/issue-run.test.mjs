import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, readFile, writeFile, cp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

const sha = 'a'.repeat(40), repo = 'cubeshipd/cubeship';
const context = { repo, issueNumber: 12, issueId: 120, event: 'opened', ref: 'refs/heads/master', defaultBranch: 'master', workflowSha: sha, runId: 500, runAttempt: 1, actor: 'lucasaarch', dryRun: false };
async function modules(t, enabled = true) {
  const dir = await mkdtemp(join(tmpdir(), 'issue-run-test-')); t.after(() => rm(dir, { recursive: true, force: true }));
  for (const name of ['issue-run.mjs', 'issue-core.mjs', 'issue-config.json']) await cp(new URL(name, import.meta.url), join(dir, name));
  const config = JSON.parse(await readFile(join(dir, 'issue-config.json'))); config.enabled = enabled; config.publishEnabled = enabled;
  await writeFile(join(dir, 'issue-config.json'), JSON.stringify(config));
  return { dir, config, run: await import(pathToFileURL(join(dir, 'issue-run.mjs'))), core: await import(pathToFileURL(join(dir, 'issue-core.mjs'))) };
}
function fixture(config, core) {
  const issue = { number: 12, id: 120, state: 'open', title: 'Falha ao salvar', body: 'A operação falha. npm test', user: { login: 'lucas', type: 'User' }, labels: [{ name: 'human' }], type: null };
  const fields = [{ issue_field_id: 46483204, issue_field_name: 'Effort', data_type: 'single_select', value: 81364770, single_select_option: { id: 81364770, name: 'Low' } }];
  const comments = [], mutations = [], calls = [], history = [];
  let count = 300, mode = 'ok', generation = 0;
  const origin = () => ({ repo, issueNumber: 12, issueId: 120, contentHash: core.fingerprint(issue), workflowSha: sha, runId: 500 });
  const proposal = () => ({ snapshot: origin(), type: { name: 'Bug', confidence: .99 }, priority: { name: 'High', confidence: .99 }, labels: [{ name: 'bug', confidence: .99 }], translation: { title: 'Save failure', body: 'The operation fails. npm test', sourceLanguage: 'pt', targetLanguage: 'en' }, duplicates: [] });
  const runRecord = (id, number = 12, status = 'completed') => ({ id, run_attempt: 1, event: 'issues', path: '.github/workflows/issue-triage.yml', head_sha: sha, head_branch: 'master', actor: { login: context.actor }, repository: { full_name: repo }, display_title: `issue-triage:${repo}:${number}:${number * 10}:live`, status });
  const own = { ...runRecord(500, 12, 'in_progress'), display_title: `issue-triage:${repo}:12:120:live` };
  const jobs = new Map();
  const fetch = async (url, opts) => {
    calls.push({ url, opts }); const u = new URL(url), path = u.pathname, method = opts.method;
    assert.equal(opts.redirect, 'error');
    if (u.hostname === 'generativelanguage.googleapis.com') {
      assert.equal(opts.headers.Authorization, undefined);
      if (path.endsWith(':countTokens')) { if (mode === 'change-after-count') issue.body += ' changed'; return Response.json({ totalTokens: count }); }
      generation++; if (mode === 'abort') throw new Error('key and payload MUST NOT ESCAPE');
      if (/^status-/.test(mode)) return Response.json({ error: 'private provider payload' }, { status: Number(mode.slice(7)) });
      const p = proposal(); if (core.sanitizeProse(issue.body).reason === 'EMPTY_PROSE' || issue.body === '') p.translation.body = ''; if (mode === 'invalid') p.tools = [];
      const part = { text: JSON.stringify(p), thoughtSignature: 'opaque' };
      if (mode === 'thought') part.thought = true;
      return Response.json({ candidates: [{ finishReason: 'STOP', content: { role: 'model', parts: [part] } }], usageMetadata: { promptTokenCount: count, candidatesTokenCount: 100, thoughtsTokenCount: 0, totalTokenCount: count + 100 } });
    }
    assert.equal(u.hostname, 'api.github.com');
    if (method !== 'GET') {
      const body = JSON.parse(opts.body); mutations.push({ path, method, body });
      if (mode === 'write-abort') throw new Error('Bearer token secret');
      if (path.endsWith('/issue-field-values')) { assert.deepEqual(Object.keys(body), ['issue_field_values']); assert.ok(Array.isArray(body.issue_field_values) && body.issue_field_values.length === 1); if (mode !== 'ignored-priority') fields.push({ issue_field_id: body.issue_field_values[0].field_id, issue_field_name: 'Priority', data_type: 'single_select', value: 81364765, single_select_option: { id: 81364765, name: body.issue_field_values[0].value } }); return Response.json(fields); }
      if (path.endsWith('/labels')) { issue.labels.push(...body.labels.map(name => ({ name }))); return Response.json(issue.labels); }
      if (path.endsWith('/issues/12')) { if (mode !== 'ignored-type') issue.type = config.types.find(t => t.name === body.type); return Response.json(issue); }
      if (path.endsWith('/comments')) { const c = { id: 77, body: body.body, user: { login: 'github-actions[bot]', type: 'Bot', id: 41898282 } }; comments.push(c); return Response.json(c, { status: 201 }); }
      throw Error('unexpected mutation');
    }
    if (path === `/repos/${repo}`) return Response.json({ full_name: repo, private: mode === 'private', visibility: 'public', default_branch: 'master' });
    if (path.endsWith('/issues/12')) return Response.json(issue);
    if (path.endsWith('/issue-types')) return Response.json(config.types);
    if (path.endsWith('/labels')) return Response.json(config.labels.map(name => ({ name })));
    if (path.endsWith('/issue-field-values')) return Response.json(fields);
    if (path.endsWith('/comments')) return Response.json(comments);
    if (path.endsWith('/permission')) return Response.json({ role_name: 'maintain' });
    if (path === '/search/issues') return Response.json({ total_count: 0, incomplete_results: false, items: [] });
    if (path.endsWith('/runs/500')) return Response.json(own);
    if (path.endsWith('/issue-triage.yml/runs')) { const page = Number(u.searchParams.get('page')); return Response.json({ total_count: history.length + 1, workflow_runs: [own, ...history].slice((page - 1) * 100, page * 100) }); }
    const match = /\/runs\/(\d+)\/jobs$/.exec(path);
    if (match) return Response.json(jobs.get(Number(match[1])) ?? { total_count: 1, jobs: [{ steps: [{ name: 'Generate Gemini proposal', status: 'completed', conclusion: 'skipped', started_at: null }] }] });
    throw Error(`unexpected read ${path}`);
  };
  return { issue, fields, comments, mutations, calls, history, jobs, own, fetch, proposal, runRecord, origin, get generation() { return generation; }, set mode(x) { mode = x; }, set count(x) { count = x; } };
}
const transport = (run, f) => run.createTransport(f.fetch, { now: () => Date.parse('2026-10-12T06:00:00Z'), sleep: async () => {} });

test('disabled CLI refuses before transport or credential getter', async t => {
  const { dir, run } = await modules(t, false); const ctx = join(dir, 'context.json'); await writeFile(ctx, JSON.stringify(context));
  const env = {}; Object.defineProperty(env, 'GEMINI_API_KEY', { get() { throw Error('read key'); } });
  for (const command of ['collect', 'infer', 'publish']) assert.equal((await run.runCLI(command === 'infer' ? [command, ctx, join(dir, 'artifact.json'), join(dir, 'proposal.json')] : [command, ctx, join(dir, 'artifact.json')], { env, transport: run.createTransport(() => { throw Error('network'); }) })).reason, 'DISABLED');
});
test('actual CLI isolates credentials and adds native metadata preserving originals and Effort', async t => {
  const { dir, run, core, config } = await modules(t); const f = fixture(config, core), tr = transport(run, f); const original = structuredClone(f.issue), ctx = join(dir, 'context.json'), snapshot = join(dir, 'snapshot.json'), artifact = join(dir, 'proposal.json'); await writeFile(ctx, JSON.stringify(context));
  assert.equal((await run.runCLI(['collect', ctx, snapshot], { env: { GITHUB_TOKEN: 'fixture-github' }, transport: tr })).state, 'ready');
  const collected = await readFile(snapshot, 'utf8'); assert.ok(!collected.includes(original.title) && !collected.includes(original.body));
  assert.equal((await run.runCLI(['infer', ctx, snapshot, artifact], { env: { GEMINI_API_KEY: 'fixture-google' }, transport: tr })).state, 'ready');
  assert.equal(f.generation, 1); const output = await readFile(artifact, 'utf8'); assert.ok(!output.includes('fixture-google') && !output.includes('opaque') && !output.includes(original.body));
  assert.equal((await run.runCLI(['publish', ctx, artifact], { env: { GITHUB_TOKEN: 'fixture-github' }, transport: tr })).state, 'ready');
  assert.deepEqual(f.mutations.map(m => [m.method, m.body]), [['PATCH', { type: 'Bug' }], ['POST', { issue_field_values: [{ field_id: 46483201, value: 'High' }] }], ['POST', { labels: ['bug'] }], ['POST', { body: f.comments[0].body }]]);
  assert.equal(f.issue.title, original.title); assert.equal(f.issue.body, original.body); assert.equal(f.fields[0].issue_field_id, 46483204);
  const before = f.mutations.length; assert.equal((await run.runCLI(['publish', ctx, artifact], { env: { GITHUB_TOKEN: 'fixture-github' }, transport: tr })).state, 'ready'); assert.equal(f.mutations.length, before);
});
async function setup(t) { const m = await modules(t); const f = fixture(m.config, m.core); return { ...m, f, tr: transport(m.run, f) }; }
async function collected(s, c = context) { const a = await s.run.collectSnapshot(c, s.tr, {}); assert.equal(a.state, 'ready', JSON.stringify(a)); return a; }
test('inference reuses collector-verified types without anonymous catalog access', async t => {
  const s = await setup(t), anonymousTypes = [];
  const tr = s.run.createTransport(async (url, opts) => {
    if (new URL(url).pathname.endsWith('/issue-types') && !opts.headers.Authorization) {
      anonymousTypes.push(url); return Response.json({}, { status: 404 });
    }
    return s.f.fetch(url, opts);
  }, { now: s.tr.now, sleep: s.tr.sleep });
  const a = await s.run.collectSnapshot(context, tr, { GITHUB_TOKEN: 'fixture-github' });
  assert.equal(a.state, 'ready');
  assert.equal((await s.run.generateOnce(a, tr, 'fixture-google')).state, 'ready');
  assert.deepEqual(anonymousTypes, []); assert.equal(s.f.generation, 1);
});
test('authenticated catalog failures still block collection and publication', async t => {
  const s = await setup(t), a = await collected(s);
  const tr = s.run.createTransport(async (url, opts) => new URL(url).pathname.endsWith('/issue-types') ? Response.json({}, { status: 404 }) : s.f.fetch(url, opts), { now: s.tr.now, sleep: s.tr.sleep });
  assert.equal((await s.run.collectSnapshot(context, tr, { GITHUB_TOKEN: 'fixture-github' })).reason, 'GITHUB_READ_FAILED');
  assert.equal((await s.run.publishProposal(s.f.proposal(), a, tr, { GITHUB_TOKEN: 'fixture-github' })).reason, 'GITHUB_READ_FAILED');
  assert.equal(s.f.mutations.length, 0); assert.equal(s.f.generation, 0);
});
test('inference CLI reports only a bounded HTTP status for provider failures', async t => {
  const s = await setup(t), a = await collected(s), ctx = join(s.dir, 'context.json'), snapshot = join(s.dir, 'snapshot.json');
  await writeFile(ctx, JSON.stringify(context)); await writeFile(snapshot, JSON.stringify(a)); s.f.mode = 'status-503';
  const out = await s.run.runCLI(['infer', ctx, snapshot, join(s.dir, 'proposal.json')], { env: { GEMINI_API_KEY: 'fixture-google' }, transport: s.tr });
  assert.deepEqual(out, { state: 'blocked', reason: 'GENERATION_FAILED', httpStatus: 503 });
  await assert.rejects(readFile(join(s.dir, 'proposal.json')), { code: 'ENOENT' });
});
test('provider diagnostics expose only fixed codes and parameter names', async t => {
  const s = await setup(t), a = await collected(s), ctx = join(s.dir, 'context.json'), snapshot = join(s.dir, 'snapshot.json');
  await writeFile(ctx, JSON.stringify(context)); await writeFile(snapshot, JSON.stringify(a));
  const tr = s.run.createTransport(async (url, opts) => url.endsWith(':countTokens') ? Response.json({ error: { status: 'INVALID_ARGUMENT', message: 'Unknown name responseFormat; private provider payload and fixture-google MUST NOT ESCAPE' } }, { status: 400 }) : s.f.fetch(url, opts), { now: s.tr.now, sleep: s.tr.sleep });
  assert.deepEqual(await s.run.runCLI(['infer', ctx, snapshot, join(s.dir, 'proposal.json')], { env: { GEMINI_API_KEY: 'fixture-google' }, transport: tr }), { state: 'blocked', reason: 'TOKEN_COUNT_FAILED', httpStatus: 400, providerStatus: 'INVALID_ARGUMENT', parameter: 'responseFormat' });
  assert.equal(s.f.generation, 0); assert.equal(s.f.mutations.length, 0);
});
test('complex schema refusals emit a fixed category without raw provider prose', async t => {
  const s = await setup(t), a = await collected(s);
  const tr = s.run.createTransport(async (url, opts) => url.endsWith(':generateContent') ? Response.json({ error: { status: 'INVALID_ARGUMENT', message: 'The specified schema produces a constraint that has too many states for serving; fixture-google and private provider payload MUST NOT ESCAPE' } }, { status: 400 }) : s.f.fetch(url, opts), { now: s.tr.now, sleep: s.tr.sleep });
  assert.deepEqual(await s.run.generateOnce(a, tr, 'fixture-google'), { state: 'blocked', reason: 'GENERATION_FAILED', httpStatus: 400, providerStatus: 'INVALID_ARGUMENT', category: 'SCHEMA_COMPLEXITY' });
});
test('structured provider violations remain filtered when the top-level error is generic', async t => {
  const s = await setup(t), a = await collected(s);
  const tr = s.run.createTransport(async (url, opts) => url.endsWith(':generateContent') ? Response.json({ error: { status: 'INVALID_ARGUMENT', message: 'Request contains an invalid argument.', details: [{ fieldViolations: [{ field: 'generation_config.response_json_schema', description: 'The schema has too many states. PRIVATE_SENTINEL' }] }] } }, { status: 400 }) : s.f.fetch(url, opts), { now: s.tr.now, sleep: s.tr.sleep });
  assert.deepEqual(await s.run.generateOnce(a, tr, 'fixture-google'), { state: 'blocked', reason: 'GENERATION_FAILED', httpStatus: 400, providerStatus: 'INVALID_ARGUMENT', parameter: 'response_json_schema', category: 'SCHEMA_COMPLEXITY' });
});
for (const mode of ['status-400', 'status-429', 'status-500', 'status-503', 'abort', 'invalid', 'thought']) {
  test(`generation ${mode} never retries or publishes raw responses`, async t => {
    const s = await setup(t), a = await collected(s); s.f.mode = mode;
    const r = await s.run.generateOnce(a, s.tr, 'fixture-google'); assert.equal(r.state, 'blocked'); assert.equal(s.f.generation, 1); assert.equal(s.f.mutations.length, 0);
    if (mode.startsWith('status-')) assert.equal(r.httpStatus, Number(mode.slice(7)));
    assert.ok(!/payload|key|SECRET|fixture-google|Bearer/.test(JSON.stringify(r)));
  });
}
for (const mode of ['overflow', 'change-after-count', 'private']) {
  test(`before generation ${mode} blocks the paid call`, async t => {
    const s = await setup(t), a = await collected(s); if (mode === 'overflow') s.f.count = 8001; else s.f.mode = mode;
    assert.equal((await s.run.generateOnce(a, s.tr, 'fixture-google')).state, 'blocked'); assert.equal(s.f.generation, 0); assert.equal(s.f.mutations.length, 0);
  });
}
test('count request includes exact system, schema and prose with bounded config and no tools', async t => {
  const s = await setup(t), a = await collected(s); assert.equal((await s.run.generateOnce(a, s.tr, 'fixture-google')).state, 'ready');
  const requests = s.f.calls.filter(c => new URL(c.url).hostname === 'generativelanguage.googleapis.com'); assert.equal(requests.length, 2);
  const counted = JSON.parse(requests[0].opts.body).generateContentRequest, generated = JSON.parse(requests[1].opts.body);
  assert.equal(counted.model, 'models/gemini-3.5-flash-lite'); delete counted.model; assert.deepEqual(counted, generated);
  assert.equal(generated.generationConfig.maxOutputTokens, 2048); assert.equal(generated.generationConfig.candidateCount, 1); assert.equal(generated.generationConfig.thinkingConfig.includeThoughts, false);
  assert.ok(generated.systemInstruction.parts[0].text.includes('untrusted data')); assert.equal(generated.generationConfig.responseJsonSchema.additionalProperties, false);
  assert.equal(generated.generationConfig.responseMimeType, 'application/json');
  assert.equal(generated.generationConfig.responseFormat, undefined);
  assert.ok(!/"tools"|"author"|Effort|github-actions/.test(JSON.stringify(generated))); assert.equal(requests[0].opts.headers.Authorization, undefined);
});
test('manual dry run validates trusted maintainer context without key getter provider or writes', async t => {
  const s = await setup(t), c = { ...context, event: 'manualdry', dryRun: true }; s.f.own.event = 'workflow_dispatch'; s.f.own.display_title = `issue-triage:${repo}:dry`;
  const path = join(s.dir, 'ctx.json'), a = join(s.dir, 'snapshot.json'), p = join(s.dir, 'proposal.json'); await writeFile(path, JSON.stringify(c));
  const env = {}; Object.defineProperty(env, 'GEMINI_API_KEY', { get() { throw Error('read key'); } });
  assert.equal((await s.run.runCLI(['collect', path, a], { env, transport: s.tr })).state, 'ready');
  assert.equal((await s.run.runCLI(['infer', path, a, p], { env, transport: s.tr })).reason, 'DRY_RUN_VALIDATED');
  assert.equal((await s.run.runCLI(['publish', path, p], { env, transport: s.tr })).state, 'ready'); assert.equal(s.f.calls.filter(c => new URL(c.url).hostname === 'generativelanguage.googleapis.com').length, 0); assert.equal(s.f.mutations.length, 0);
});
for (const kind of ['bot', 'PR', 'closed', 'private', 'rerun', 'ref', 'runOrigin', 'unknownHistory', 'incompleteJobs', 'concurrent']) {
  test(`collection refuses ${kind} without provider or writes`, async t => {
    const s = await setup(t), c = { ...context };
    if (kind === 'bot') s.f.issue.user = { type: 'Bot', login: 'helper[bot]' };
    if (kind === 'PR') s.f.issue.pull_request = {};
    if (kind === 'closed') s.f.issue.state = 'closed';
    if (kind === 'private') s.f.mode = 'private';
    if (kind === 'rerun') c.runAttempt = 2;
    if (kind === 'ref') c.ref = 'refs/heads/evil';
    if (kind === 'runOrigin') s.f.own.head_sha = 'b'.repeat(40);
    if (kind === 'unknownHistory') s.f.history.push({ ...s.f.runRecord(100), display_title: 'untrusted title' });
    if (kind === 'incompleteJobs') { s.f.history.push(s.f.runRecord(100)); s.f.jobs.set(100, { total_count: 1, jobs: [{ steps: [] }] }); }
    if (kind === 'concurrent') s.f.history.push(s.f.runRecord(100, 10, 'in_progress'));
    assert.equal((await s.run.collectSnapshot(c, s.tr)).state, 'blocked'); assert.equal(s.f.generation, 0); assert.equal(s.f.mutations.length, 0);
  });
}
for (const conclusion of ['failure', 'timed_out', 'cancelled', 'success']) {
  test(`persistent ${conclusion} generation step reserves Issue across fresh calls`, async t => {
    const s = await setup(t); s.f.history.push(s.f.runRecord(100)); s.f.jobs.set(100, { total_count: 1, jobs: [{ steps: [{ name: 'Generate Gemini proposal', status: 'completed', conclusion, started_at: '2026-10-12T05:59:00Z' }] }] });
    assert.equal((await s.run.collectSnapshot(context, s.tr)).reason, 'GENERATION_RESERVED');
    assert.equal((await s.run.collectSnapshot(context, transport(s.run, s.f))).reason, 'GENERATION_RESERVED'); assert.equal(s.f.generation, 0);
  });
}
test('skipped and provenance-verified dry histories consume no admission slots', async t => {
  const s = await setup(t);
  for (let id = 1; id <= 12; id++) { const r = s.f.runRecord(id); if (id % 2) { r.event = 'workflow_dispatch'; r.display_title = `issue-triage:${repo}:dry`; } s.f.history.push(r); }
  assert.equal((await s.run.collectSnapshot(context, s.tr)).state, 'ready');
  s.f.history[0].event = 'issues'; assert.equal((await s.run.collectSnapshot(context, s.tr)).reason, 'HISTORY_INCOMPLETE');
});
test('persistent sequential admission reaches hourly boundary and releases the exact old hour', async t => {
  const s = await setup(t);
  for (let index = 0; index < 10; index++) {
    assert.equal((await s.run.collectSnapshot(context, transport(s.run, s.f))).state, 'ready');
    const id = index + 1; s.f.history.push(s.f.runRecord(id, 100 + id));
    s.f.jobs.set(id, { total_count: 1, jobs: [{ steps: [{ name: 'Generate Gemini proposal', status: 'completed', conclusion: index % 2 ? 'timed_out' : 'failure', started_at: '2026-10-12T05:30:00Z' }] }] });
  }
  assert.equal((await s.run.collectSnapshot(context, transport(s.run, s.f))).reason, 'BUDGET_EXHAUSTED');
  const later = s.run.createTransport(s.f.fetch, { now: () => Date.parse('2026-10-12T06:30:00Z'), sleep: async () => {} });
  assert.equal((await s.run.collectSnapshot(context, later)).state, 'ready');
});
test('persistent monthly quota blocks the hundredth reservation and resets at UTC month boundary', async t => {
  const s = await setup(t);
  for (let id = 1; id <= 99; id++) { s.f.history.push(s.f.runRecord(id, 100 + id)); s.f.jobs.set(id, { total_count: 1, jobs: [{ steps: [{ name: 'Generate Gemini proposal', status: 'completed', conclusion: 'failure', started_at: '2026-10-01T00:00:00Z' }] }] }); }
  assert.equal((await s.run.collectSnapshot(context, s.tr)).state, 'ready');
  s.f.history.push(s.f.runRecord(100, 200)); s.f.jobs.set(100, { total_count: 1, jobs: [{ steps: [{ name: 'Generate Gemini proposal', status: 'completed', conclusion: 'timed_out', started_at: '2026-10-01T00:00:00Z' }] }] });
  assert.equal((await s.run.collectSnapshot(context, transport(s.run, s.f))).reason, 'BUDGET_EXHAUSTED');
  const later = s.run.createTransport(s.f.fetch, { now: () => Date.parse('2026-11-01T00:00:00Z'), sleep: async () => {} }); assert.equal((await s.run.collectSnapshot(context, later)).state, 'ready');
});
for (const mode of ['ignored-type', 'ignored-priority', 'write-abort']) {
  test(`publication ${mode} reports unconfirmed without blind retry or later effects`, async t => {
    const s = await setup(t), a = await collected(s); s.f.mode = mode;
    assert.equal((await s.run.publishProposal(s.f.proposal(), a, s.tr, { GITHUB_TOKEN: 'fixture-github' })).reason, 'WRITE_UNCONFIRMED');
    assert.equal(s.f.comments.length, 0); assert.equal(s.f.mutations.length, mode === 'ignored-priority' ? 2 : 1);
  });
}
test('writer preserves existing human type Priority Effort labels and legacy priority', async t => {
  const s = await setup(t), a = await collected(s); s.f.issue.type = s.config.types[2]; s.f.issue.labels.push({ name: 'P1' });
  assert.equal((await s.run.publishProposal(s.f.proposal(), a, s.tr, { GITHUB_TOKEN: 'fixture-github' })).state, 'ready');
  assert.ok(s.f.mutations.every(m => !m.body.type && !Array.isArray(m.body))); assert.equal(s.f.issue.type.name, 'Feature'); assert.equal(s.f.fields.length, 1);
});
test('human fingerprint marker spoof blocks every mutation', async t => {
  const s = await setup(t), a = await collected(s); s.f.comments.push({ id: 99, user: { login: 'lucas', type: 'User', id: 1 }, body: `<!-- cubeship-issue-triage:${a.snapshot.contentHash} -->` });
  assert.equal((await s.run.publishProposal(s.f.proposal(), a, s.tr, { GITHUB_TOKEN: 'fixture-github' })).reason, 'MARKER_REFUSED'); assert.equal(s.f.mutations.length, 0);
});
test('writer rejects unknown native metadata and field read shape before mutation', async t => {
  const s = await setup(t), a = await collected(s); delete s.f.issue.type;
  assert.equal((await s.run.publishProposal(s.f.proposal(), a, s.tr, { GITHUB_TOKEN: 'fixture-github' })).reason, 'CURRENT_METADATA_REFUSED'); assert.equal(s.f.mutations.length, 0);
  s.f.issue.type = null; s.f.fields[0] = { field_id: 46483204, value: 'Low' };
  assert.equal((await s.run.publishProposal(s.f.proposal(), a, s.tr, { GITHUB_TOKEN: 'fixture-github' })).reason, 'CURRENT_METADATA_REFUSED');
});
test('credential isolation refuses mixed infer and writer environments before key reads', async t => {
  const s = await setup(t), a = await collected(s), ctx = join(s.dir, 'ctx.json'), snapshot = join(s.dir, 'snapshot.json'); await writeFile(ctx, JSON.stringify(context)); await writeFile(snapshot, JSON.stringify(a));
  const env = { GITHUB_TOKEN: 'fixture-github' }; Object.defineProperty(env, 'GEMINI_API_KEY', { get() { throw Error('read key'); } });
  assert.equal((await s.run.runCLI(['infer', ctx, snapshot, join(s.dir, 'proposal.json')], { env, transport: s.tr })).reason, 'CREDENTIAL_ISOLATION');
  const before = s.f.calls.length; assert.equal((await s.run.publishProposal(s.f.proposal(), a, s.tr, env)).reason, 'CREDENTIAL_ISOLATION'); assert.equal(s.f.calls.length, before);
});
test('changed original hash blocks publication before native or comment writes', async t => {
  const s = await setup(t), a = await collected(s), p = s.f.proposal(); s.f.issue.body += ' edited'; assert.equal((await s.run.publishProposal(p, a, s.tr, { GITHUB_TOKEN: 'fixture-github' })).reason, 'SNAPSHOT_CHANGED'); assert.equal(s.f.mutations.length, 0);
});
test('provider nested tool output cannot hide under allowed envelope metadata', async t => {
  const s = await setup(t), a = await collected(s), base = s.f.fetch;
  const tr = s.run.createTransport(async (url, opts) => { const r = await base(url, opts); if (url.endsWith(':generateContent')) { const d = await r.json(); d.candidates[0].safetyRatings = [{ functionCall: { name: 'execute' } }]; return Response.json(d); } return r; }, { now: s.tr.now, sleep: s.tr.sleep });
  assert.equal((await s.run.generateOnce(a, tr, 'fixture-google')).state, 'blocked');
});
function candidateFixture(s, candidateBody = 'A similar failure.\n' + 'safe prose '.repeat(100)) {
  const candidate = { number: 13, id: 130, state: 'open', title: 'Similar failure', body: candidateBody, user: { login: 'human', type: 'User' } }, base = s.f.fetch;
  let candidateReads = 0;
  const tr = s.run.createTransport(async (url, opts) => {
    const u = new URL(url);
    if (u.pathname === '/search/issues') return Response.json({ total_count: 1, incomplete_results: false, items: [{ number: candidate.number, id: candidate.id, repository_url: `https://api.github.com/repos/${repo}` }] });
    if (u.pathname.endsWith('/issues/13')) { candidateReads++; return Response.json(candidate); }
    return base(url, opts);
  }, { now: s.tr.now, sleep: s.tr.sleep });
  return { candidate, tr, get reads() { return candidateReads; } };
}
test('candidate artifact hashes full original body and excludes prose after a documented bounded excerpt', async t => {
  const s = await setup(t), c = candidateFixture(s), a = await s.run.collectSnapshot(context, c.tr); assert.equal(a.state, 'ready');
  assert.equal(a.candidates[0].expectedContentHash, s.core.fingerprint(c.candidate)); assert.deepEqual(Object.keys(a.candidates[0]), ['repo', 'number', 'id', 'expectedContentHash']);
  const gen = await s.run.generateOnce(a, c.tr, 'fixture-google'); assert.equal(gen.state, 'ready');
  const contents = JSON.parse(s.f.calls.find(x => x.url.endsWith(':generateContent')).opts.body).contents;
  const input = JSON.parse(contents[0].parts[0].text); assert.ok(Buffer.byteLength(input.candidates[0].excerpt) <= 400); assert.ok(!JSON.stringify(a).includes(c.candidate.body));
});
test('candidate secret beyond excerpt or inside omitted code refuses before count', async t => {
  const s = await setup(t);
  for (const body of ['safe prose '.repeat(100) + '\npassword=SECRET_SENTINEL', 'Similar failure.\n```\n' + 'safe logs\n'.repeat(100) + 'api_key=SECRET_SENTINEL\n```']) {
    const c = candidateFixture(s, body), r = await s.run.collectSnapshot(context, c.tr); assert.equal(r.reason, 'CANDIDATE_REFUSED'); assert.ok(!JSON.stringify(r).includes('SECRET_SENTINEL'));
  }
  assert.equal(s.f.calls.filter(x => new URL(x.url).hostname === 'generativelanguage.googleapis.com').length, 0);
});
test('candidate original expected hash survives refetch and changed body blocks publication', async t => {
  const s = await setup(t), c = candidateFixture(s), a = await s.run.collectSnapshot(context, c.tr), p = s.f.proposal(); p.duplicates = [{ number: 13, confidence: .99 }]; c.candidate.body += ' changed';
  assert.equal((await s.run.publishProposal(p, a, c.tr, { GITHUB_TOKEN: 'fixture-github' })).reason, 'CANDIDATE_CHANGED'); assert.equal(s.f.mutations.length, 0);
});
test('candidate changed after metadata writes is checked again before comment', async t => {
  const s = await setup(t), c = candidateFixture(s), a = await s.run.collectSnapshot(context, c.tr), p = s.f.proposal(); p.duplicates = [{ number: 13, confidence: .99 }];
  const tr = s.run.createTransport(async (url, opts) => { const r = await c.tr.fetch(url, opts); if (opts.method === 'POST' && url.endsWith('/labels')) c.candidate.body += ' changed'; return r; }, { now: s.tr.now, sleep: s.tr.sleep });
  assert.equal((await s.run.publishProposal(p, a, tr, { GITHUB_TOKEN: 'fixture-github' })).reason, 'CANDIDATE_CHANGED'); assert.equal(s.f.comments.length, 0); assert.equal(s.f.mutations.length, 3);
});
test('private PR foreign self and unverified search candidates cannot expand read routes', async t => {
  const s = await setup(t);
  for (const item of [{ number: 13, id: 130, repository_url: 'https://api.github.com/repos/foreign/repo' }, { number: 13, id: 130, repository_url: `https://api.github.com/repos/${repo}`, pull_request: {} }, { number: '../evil', id: 130, repository_url: `https://api.github.com/repos/${repo}` }]) {
    const tr = s.run.createTransport(async (url, opts) => new URL(url).pathname === '/search/issues' ? Response.json({ total_count: 1, incomplete_results: false, items: [item] }) : s.f.fetch(url, opts), { now: s.tr.now, sleep: s.tr.sleep }); assert.equal((await s.run.collectSnapshot(context, tr)).reason, 'CANDIDATE_REFUSED');
  }
  const c = candidateFixture(s); c.candidate.pull_request = {}; assert.equal((await s.run.collectSnapshot(context, c.tr)).reason, 'CANDIDATE_REFUSED');
  assert.ok(s.f.calls.every(x => new URL(x.url).pathname.startsWith(`/repos/${repo}`)));
});
test('read backoff caps at three attempts while mutations and provider POST have no retry', async t => {
  const s = await setup(t); let attempts = 0; const delays = [];
  const tr = s.run.createTransport(async (url, opts) => { if (url.endsWith('/actions/runs/500') && ++attempts < 3) return Response.json({}, { status: 503 }); return s.f.fetch(url, opts); }, { now: s.tr.now, sleep: async ms => delays.push(ms) });
  assert.equal((await s.run.collectSnapshot(context, tr)).state, 'ready'); assert.equal(attempts, 3); assert.deepEqual(delays, [1000, 2000]);
  attempts = 0; const failed = s.run.createTransport(async () => { attempts++; return Response.json({}, { status: 429, headers: { 'retry-after': '31' } }); }, { now: s.tr.now, sleep: async () => { throw Error('must not wait'); } });
  assert.equal((await s.run.collectSnapshot(context, failed)).reason, 'GITHUB_READ_FAILED'); assert.equal(attempts, 1);
});
test('oversized native response and incomplete pagination fail closed without raw payload', async t => {
  const s = await setup(t); let requests = 0;
  const tr = s.run.createTransport(async () => { requests++; return new Response('"' + 'PRIVATE_SENTINEL'.repeat(150000) + '"'); }, { now: s.tr.now, sleep: s.tr.sleep });
  const r = await s.run.collectSnapshot(context, tr); assert.equal(r.state, 'blocked'); assert.ok(!JSON.stringify(r).includes('PRIVATE_SENTINEL')); assert.equal(requests, 3);
  const bad = s.run.createTransport(async (url, opts) => url.includes('issue-triage.yml/runs') ? Response.json({ total_count: 2, workflow_runs: [s.f.own] }) : s.f.fetch(url, opts), { now: s.tr.now, sleep: s.tr.sleep }); assert.equal((await s.run.collectSnapshot(context, bad)).reason, 'HISTORY_INCOMPLETE');
});
test('unrecognized Google usage is numeric filtered and uncertain rather than exposing metadata', async t => {
  const s = await setup(t), a = await collected(s);
  const tr = s.run.createTransport(async (url, opts) => { const r = await s.f.fetch(url, opts); if (url.endsWith(':generateContent')) { const d = await r.json(); d.usageMetadata = { promptTokenCount: 300, candidatesTokenCount: 'SECRET_SENTINEL', totalTokenCount: 400, internalText: 'PRIVATE_SENTINEL' }; return Response.json(d); } return r; }, { now: s.tr.now, sleep: s.tr.sleep });
  const r = await s.run.generateOnce(a, tr, 'fixture-google'); assert.equal(r.state, 'ready'); assert.deepEqual(r.usage, { promptTokenCount: 300, totalTokenCount: 400, measurement: 'uncertain' }); assert.ok(!/SENTINEL/.test(JSON.stringify(r)));
});
test('downloaded artifact directory refuses extra files links and oversized payloads', async t => {
  const s = await setup(t), dir = join(s.dir, 'download'); await (await import('node:fs/promises')).mkdir(dir); await writeFile(join(dir, 'artifact.json'), '{}');
  await writeFile(join(dir, 'extra.json'), '{}'); await assert.rejects(s.run.readArtifactDirectory(dir), /ARTIFACT_REFUSED/); await rm(join(dir, 'extra.json'));
  await writeFile(join(dir, 'artifact.json'), 'x'.repeat(32769)); await assert.rejects(s.run.readArtifactDirectory(dir), /ARTIFACT_REFUSED/);
  await rm(join(dir, 'artifact.json')); await (await import('node:fs/promises')).symlink(join(s.dir, 'issue-config.json'), join(dir, 'artifact.json')); await assert.rejects(s.run.readArtifactDirectory(dir), /ARTIFACT_REFUSED/);
});
test('code-only and long omitted primary logs still allow one native generation', async t => {
  const s = await setup(t);
  for (const body of ['```\nnpm test\n```', 'Helpful prose.\n```\n' + 'log\n'.repeat(2000) + '```']) {
    s.f.issue.body = body; const a = await collected(s); assert.equal((await s.run.generateOnce(a, s.tr, 'fixture-google')).state, 'ready');
  }
});
test('original metadata artifact must reject injected raw prose and context keys', async t => {
  const s = await setup(t), a = await collected(s);
  for (const altered of [{ ...a, body: 'PRIVATE_SENTINEL' }, { ...a, context: { ...context, arbitrary: 'PRIVATE_SENTINEL' } }, { ...a, candidates: [{ repo, number: 13, id: 130, expectedContentHash: 'b'.repeat(64), excerpt: 'PRIVATE_SENTINEL' }] }]) assert.equal((await s.run.generateOnce(altered, s.tr, 'fixture-google')).state, 'blocked');
  assert.equal(s.f.generation, 0);
});
test('removed configured label catalog is refreshed before writes and never recreated', async t => {
  const s = await setup(t), a = await collected(s); let catalogs = 0;
  const tr = s.run.createTransport(async (url, opts) => { if (new URL(url).pathname === `/repos/${repo}/labels` && ++catalogs > 1) return Response.json(s.config.labels.filter(x => x !== 'bug').map(name => ({ name }))); return s.f.fetch(url, opts); }, { now: s.tr.now, sleep: s.tr.sleep });
  assert.equal((await s.run.publishProposal(s.f.proposal(), a, tr, { GITHUB_TOKEN: 'fixture-github' })).state, 'blocked'); assert.equal(s.f.mutations.length, 0);
});
test('receipt verification catches unrelated human type loss on additive labels', async t => {
  const s = await setup(t), a = await collected(s); s.f.issue.type = s.config.types[2]; s.f.issue.labels.push({ name: 'P1' });
  const tr = s.run.createTransport(async (url, opts) => { const r = await s.f.fetch(url, opts); if (opts.method === 'POST' && url.endsWith('/labels')) s.f.issue.type = null; return r; }, { now: s.tr.now, sleep: s.tr.sleep });
  assert.equal((await s.run.publishProposal(s.f.proposal(), a, tr, { GITHUB_TOKEN: 'fixture-github' })).reason, 'WRITE_UNCONFIRMED'); assert.equal(s.f.comments.length, 0);
});
test('historical rerun cannot hide an earlier generation behind a later skipped attempt', async t => {
  const s = await setup(t); s.f.history.push({ ...s.f.runRecord(100, 30), run_attempt: 2 });
  s.f.jobs.set(100, { total_count: 1, jobs: [{ steps: [{ name: 'Generate Gemini proposal', status: 'completed', conclusion: 'skipped', started_at: null }] }] });
  assert.equal((await s.run.collectSnapshot(context, s.tr)).reason, 'HISTORY_INCOMPLETE'); assert.equal((await s.run.collectSnapshot(context, transport(s.run, s.f))).reason, 'HISTORY_INCOMPLETE'); assert.equal(s.f.generation, 0);
});
test('actual verified native field read fixture preserves Bug High and Effort Low', async t => {
  const s = await setup(t), a = await collected(s); s.f.issue.type = s.config.types[1];
  const native = [{ data_type: 'single_select', issue_field_id: 46483201, issue_field_name: 'Priority', single_select_option: { id: 81364765, name: 'High' }, value: 81364765 }, { data_type: 'single_select', issue_field_id: 46483204, issue_field_name: 'Effort', single_select_option: { id: 81364770, name: 'Low' }, value: 81364770 }]; s.f.fields.splice(0, s.f.fields.length, ...native);
  const before = structuredClone(s.f.fields); assert.equal((await s.run.publishProposal(s.f.proposal(), a, s.tr, { GITHUB_TOKEN: 'fixture-github' })).state, 'ready');
  assert.deepEqual(s.f.fields, before); assert.ok(s.f.mutations.every(m => !m.body.type && !Array.isArray(m.body)));
});
test('native CLI stdout emits only fixed disabled decision and never synthetic credentials or issue data', async t => {
  const { dir } = await modules(t, false), ctx = join(dir, 'ctx.json'); await writeFile(ctx, JSON.stringify(context));
  const { promisify } = await import('node:util'), { execFile } = await import('node:child_process');
  try { await promisify(execFile)(process.execPath, [join(dir, 'issue-run.mjs'), 'infer', ctx, join(dir, 'snapshot.json'), join(dir, 'proposal.json')], { env: { GEMINI_API_KEY: 'SECRET_SENTINEL', GITHUB_TOKEN: 'TOKEN_SENTINEL' } }); assert.fail('disabled must exit nonzero'); }
  catch (e) { assert.equal(e.code, 1); assert.equal(e.stdout, '{"state":"blocked","reason":"DISABLED"}\n'); assert.equal(e.stderr, ''); }
});
test('generation step start crosses quota boundary even when the run was queued earlier', async t => {
  const s = await setup(t);
  for (let id = 1; id <= 10; id++) {
    s.f.history.push({ ...s.f.runRecord(id, 100 + id), created_at: '2026-09-30T23:59:00Z' });
    s.f.jobs.set(id, { total_count: 1, jobs: [{ steps: [{ name: 'Generate Gemini proposal', status: 'completed', conclusion: 'failure', started_at: '2026-10-12T05:59:00Z' }] }] });
  }
  assert.equal((await s.run.collectSnapshot(context, s.tr)).reason, 'BUDGET_EXHAUSTED'); assert.equal(s.f.generation, 0);
});
test('actual CLI accepts a guarded single-file downloaded artifact directory and refuses extra entries before key read', async t => {
  const s = await setup(t), a = await collected(s), ctx = join(s.dir, 'ctx.json'), dir = join(s.dir, 'download'); await writeFile(ctx, JSON.stringify(context)); await (await import('node:fs/promises')).mkdir(dir); await writeFile(join(dir, 'artifact.json'), JSON.stringify(a));
  assert.equal((await s.run.runCLI(['infer', ctx, dir, join(s.dir, 'proposal.json')], { env: { GEMINI_API_KEY: 'fixture-google' }, transport: s.tr })).state, 'ready');
  await writeFile(join(dir, 'extra.json'), '{}'); const env = {}; Object.defineProperty(env, 'GEMINI_API_KEY', { get() { throw Error('must not read key'); } });
  assert.equal((await s.run.runCLI(['infer', ctx, dir, join(s.dir, 'blocked.json')], { env, transport: s.tr })).reason, 'ARTIFACT_REFUSED'); assert.equal(s.f.generation, 1);
});
test('source changed after count is checked immediately before generation', async t => {
  const s = await setup(t), a = await collected(s);
  const tr = s.run.createTransport(async (url, opts) => { const response = await s.f.fetch(url, opts); if (url.endsWith(':countTokens')) s.f.issue.body += ' edited after count'; return response; }, { now: s.tr.now, sleep: s.tr.sleep });
  assert.equal((await s.run.generateOnce(a, tr, 'fixture-google')).reason, 'SNAPSHOT_CHANGED'); assert.equal(s.f.generation, 0);
});
test('independently reconstructed trusted context tolerates JSON property order across stages', async t => {
  const s = await setup(t), a = await collected(s), ctx = join(s.dir, 'ctx.json'), snapshot = join(s.dir, 'snapshot.json'); await writeFile(ctx, JSON.stringify(Object.fromEntries(Object.entries(context).reverse()))); await writeFile(snapshot, JSON.stringify(a));
  assert.equal((await s.run.runCLI(['infer', ctx, snapshot, join(s.dir, 'proposal.json')], { env: { GEMINI_API_KEY: 'fixture-google' }, transport: s.tr })).state, 'ready');
});
for (const priorRuns of [0, 1, 2]) {
  test(`serialized collector admission gives inference 18 anonymous reads independent of ${priorRuns} prior runs`, async t => {
    const s = await setup(t), cs = Array.from({ length: 10 }, (_, i) => ({ number: 1001 + i, id: 10010 + i, title: 'Similar public issue', body: 'A similar public failure.', state: 'open' }));
    for (let id = 1; id <= priorRuns; id++) { s.f.history.push(s.f.runRecord(id, 30 + id)); s.f.jobs.set(id, { total_count: 1, jobs: [{ steps: [{ name: 'Generate Gemini proposal', status: 'completed', conclusion: 'failure', started_at: '2026-10-12T05:59:00Z' }] }] }); }
    let infer = false, reads = 0, beforeCount, beforeGeneration; const inferPaths = [];
    const tr = s.run.createTransport(async (url, opts) => {
      const u = new URL(url);
      if (infer && u.hostname === 'api.github.com' && opts.method === 'GET') { assert.equal(opts.headers.Authorization, undefined); reads++; inferPaths.push(u.pathname); }
      if (infer && url.endsWith(':countTokens')) beforeCount = reads;
      if (infer && url.endsWith(':generateContent')) beforeGeneration = reads;
      if (u.pathname === '/search/issues') return Response.json({ total_count: cs.length, incomplete_results: false, items: cs.map(c => ({ number: c.number, id: c.id, repository_url: `https://api.github.com/repos/${repo}` })) });
      const c = cs.find(c => u.pathname === `/repos/${repo}/issues/${c.number}`); if (c) return Response.json(c);
      return s.f.fetch(url, opts);
    }, { now: s.tr.now, sleep: s.tr.sleep });
    const a = await s.run.collectSnapshot(context, tr, { GITHUB_TOKEN: 'fixture-github' }); assert.equal(a.state, 'ready'); assert.equal(a.candidates.length, 10);
    infer = true; assert.equal((await s.run.generateOnce(a, tr, 'fixture-google')).state, 'ready');
    assert.deepEqual({ beforeCount, beforeGeneration, total: reads }, { beforeCount: 16, beforeGeneration: 18, total: 18 });
    assert.equal(inferPaths.filter(p => p.endsWith('/actions/runs/500')).length, 1); assert.ok(inferPaths.every(p => !p.endsWith('/jobs') && !p.endsWith('/issue-triage.yml/runs')));
    assert.equal(inferPaths.filter(p => p === `/repos/${repo}`).length, 3); assert.equal(s.f.generation, 1);
  });
}
test('writer verifies current run without rereading admitted full history', async t => {
  const s = await setup(t), a = await collected(s), paths = [];
  const tr = s.run.createTransport(async (url, opts) => { paths.push(new URL(url).pathname); if (url.includes('/issue-triage.yml/runs') || url.includes('/jobs')) return Response.json({}, { status: 403 }); return s.f.fetch(url, opts); }, { now: s.tr.now, sleep: s.tr.sleep });
  assert.equal((await s.run.publishProposal(s.f.proposal(), a, tr, { GITHUB_TOKEN: 'fixture-github' })).state, 'ready');
  assert.equal(paths.filter(p => p.endsWith('/actions/runs/500')).length, 1); assert.ok(paths.every(p => !p.endsWith('/jobs') && !p.endsWith('/issue-triage.yml/runs')));
});
for (const reason of ['HISTORY_INCOMPLETE', 'BUDGET_EXHAUSTED']) {
  test(`actual CLI ${reason} admission cannot produce an inference artifact or provider call`, async t => {
    const s = await setup(t), ctx = join(s.dir, 'ctx.json'), snapshot = join(s.dir, 'snapshot.json'), proposal = join(s.dir, 'proposal.json'); await writeFile(ctx, JSON.stringify(context));
    if (reason === 'HISTORY_INCOMPLETE') s.f.history.push({ ...s.f.runRecord(1, 30), display_title: 'ambiguous' });
    else for (let id = 1; id <= 10; id++) { s.f.history.push(s.f.runRecord(id, 30 + id)); s.f.jobs.set(id, { total_count: 1, jobs: [{ steps: [{ name: 'Generate Gemini proposal', status: 'completed', conclusion: 'failure', started_at: '2026-10-12T05:59:00Z' }] }] }); }
    assert.equal((await s.run.runCLI(['collect', ctx, snapshot], { env: { GITHUB_TOKEN: 'fixture-github' }, transport: s.tr })).reason, reason);
    await assert.rejects(readFile(snapshot), { code: 'ENOENT' });
    const env = {}; Object.defineProperty(env, 'GEMINI_API_KEY', { get() { throw Error('key cannot be read without artifact'); } });
    assert.equal((await s.run.runCLI(['infer', ctx, snapshot, proposal], { env, transport: s.tr })).state, 'blocked'); await assert.rejects(readFile(proposal), { code: 'ENOENT' }); assert.equal(s.f.generation, 0);
  });
}
for (const stage of ['infer', 'publish']) {
  test(`${stage} refuses collected origin when current run provenance changes`, async t => {
    const s = await setup(t), a = await collected(s); s.f.own.head_sha = 'b'.repeat(40);
    const r = stage === 'infer' ? await s.run.generateOnce(a, s.tr, 'fixture-google') : await s.run.publishProposal(s.f.proposal(), a, s.tr, { GITHUB_TOKEN: 'fixture-github' });
    assert.equal(r.reason, 'RUN_ORIGIN_REFUSED'); assert.equal(s.f.generation, 0); assert.equal(s.f.mutations.length, 0);
  });
}
