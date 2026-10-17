'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { classify, evaluate, run, client } = require('./review-policy.cjs');
const sha = 'a'.repeat(40);
const person = (login) => ({ login, type: 'User' });
const bot = { login: 'agent[bot]', type: 'Bot' };
const pr = { number: 1, title: 'docs: [low] clarify usage', user: person('sponsor'),
  head: { sha }, base: { ref: 'main', sha: 'b'.repeat(40) }, state: 'open', changed_files: 1 };
const files = [{ filename: 'docs/usage.md' }];
const review = (login, state = 'APPROVED', commit = sha, id = 1) =>
  ({ user: person(login), state, commit_id: commit, id });
const evaluatePR = (overrides = {}) => evaluate({ pr, files, reviews: [], comments: [],
  canWrite: async () => true, ...overrides });

test('risk floors override misleading titles, including renamed paths', () => {
  for (const path of ['app/foo.go', 'x/foo/keeper.go', 'proto/foo.proto', 'fibre/server.go',
    '.github/workflows/ci.yml', 'go.mod', 'test/e2e/go.mod', 'docs/ai/invariants.md',
    'AGENTS.md', 'specs/src/protocol.md', 'new-production-dir/main.go']) {
    assert.equal(classify(pr.title, [{ filename: path }]), 'high', path);
  }
  assert.equal(classify(pr.title, [{ filename: 'app/foo_test.go' }]), 'medium');
  assert.equal(classify('docs: [high] new protocol', files), 'high');
  assert.equal(classify(pr.title, [{ filename: 'docs/foo.md', previous_filename: 'app/foo.go' }]), 'high');
  assert.throws(() => classify('docs: missing tag', files));
  assert.throws(() => classify(pr.title, []));
});

test('trusted low-risk author may merge without another reviewer', async () => {
  assert.equal((await evaluatePR()).ok, true);
  assert.equal((await evaluatePR({ pr: { ...pr, draft: true } })).ok, false);
});

test('untrusted low-risk author needs an independent maintainer', async () => {
  const canWrite = async (login) => login === 'reviewer';
  assert.equal((await evaluatePR({ canWrite })).required, 1);
  assert.equal((await evaluatePR({ canWrite, reviews: [review('reviewer')] })).ok, true);
});

test('medium requires one other human, high requires two distinct other humans', async () => {
  const medium = [{ filename: 'app/handler_test.go' }];
  assert.equal((await evaluatePR({ files: medium })).ok, false);
  assert.equal((await evaluatePR({ files: medium, reviews: [review('alice')] })).ok, true);
  const high = [{ filename: 'app/handler.go' }];
  assert.equal((await evaluatePR({ files: high, reviews: [review('alice'), review('alice', 'APPROVED', sha, 2)] })).ok, false);
  assert.equal((await evaluatePR({ files: high, reviews: [review('alice'), review('bob', 'APPROVED', sha, 2)] })).ok, true);
});

test('bots, sponsor, read-only users, and old-head approvals cannot fill slots', async () => {
  const result = await evaluatePR({ files: [{ filename: 'test/example.go' }],
    reviews: [review('SPONSOR'), { ...review('agent[bot]'), user: bot },
      review('reader'), review('alice', 'APPROVED', 'old')],
    canWrite: async (login) => login !== 'reader' });
  assert.deepEqual(result.approved, []);
  assert.equal(result.ok, false);
});

test('latest decisive review wins; comments do not erase approval', async () => {
  const medium = [{ filename: 'test/example.go' }];
  for (const state of ['DISMISSED', 'CHANGES_REQUESTED']) {
    assert.equal((await evaluatePR({ files: medium,
      reviews: [review('alice'), review('alice', state, sha, 2)] })).ok, false);
  }
  assert.equal((await evaluatePR({ files: medium,
    reviews: [review('alice'), review('alice', 'COMMENTED', sha, 2)] })).ok, true);
  assert.equal((await evaluatePR({ reviews: [review('alice', 'CHANGES_REQUESTED', 'old')] })).ok, false);
});

test('agent sponsor is authenticated, commit-bound, unique, and excluded', async () => {
  const agentPR = { ...pr, user: bot };
  await assert.rejects(evaluatePR({ pr: agentPR }), /exactly one maintainer/);
  const claim = (login, commit = sha) => ({ user: person(login), body: `/review-policy sponsor ${commit}` });
  await assert.rejects(evaluatePR({ pr: agentPR, comments: [claim('alice', 'old')] }));
  await assert.rejects(evaluatePR({ pr: agentPR, comments: [claim('alice')], canWrite: async () => false }));
  await assert.rejects(evaluatePR({ pr: agentPR, comments: [claim('alice'), claim('bob')] }));
  const result = await evaluatePR({ pr: agentPR, files: [{ filename: 'test/example.go' }],
    comments: [claim('alice')], reviews: [review('alice'), review('bob', 'APPROVED', sha, 2)] });
  assert.equal(result.sponsor, 'alice');
  assert.deepEqual(result.approved, ['bob']);
  assert.equal(result.ok, true);
});

function harness({ pulls = [pr], fileList = files, reviews = [], comparison = 'ahead', mutate = false } = {}) {
  const statuses = [];
  let reads = 0;
  const apiClient = {
    async api(path, method, body) {
      if (method === 'POST') { statuses.push({ path, ...body }); return {}; }
      if (path.includes('/permission')) return { permission: 'write' };
      if (path.startsWith('/compare/')) return { status: comparison };
      if (/^\/pulls\/\d+$/.test(path)) {
        const found = pulls.find((p) => p.number === Number(path.split('/').pop()));
        reads++;
        return mutate && reads > 2 ? { ...found, head: { sha: 'c'.repeat(40) } } : found;
      }
      throw new Error(`Unexpected API path ${path}`);
    },
    async pages(path) {
      if (path.startsWith('/pulls?')) return pulls;
      if (path.endsWith('/files')) return fileList;
      if (path.endsWith('/reviews')) return reviews;
      if (path.endsWith('/comments')) return [];
      throw new Error(`Unexpected pages path ${path}`);
    },
  };
  const invoke = (eventName, extra = {}) => run({ eventName,
    event: { repository: { default_branch: 'main' }, ...extra },
    repository: 'celestiaorg/celestia-app', apiClient, report: () => {} });
  return { statuses, invoke, apiClient };
}

test('head status fails closed for truncated diffs and changes during evaluation', async () => {
  for (const options of [{ fileList: [] }, { mutate: true }]) {
    const h = harness(options);
    await h.invoke('pull_request_target', { pull_request: pr });
    assert.equal(h.statuses.at(-1).state, 'failure');
  }
});

test('PRs sharing a commit cannot overwrite each other with a weaker result', async () => {
  const h = harness({ pulls: [pr, { ...pr, number: 2, title: 'docs: [high] protocol note' }] });
  await h.invoke('pull_request_target', { pull_request: pr });
  assert.deepEqual(h.statuses.map((s) => s.state), ['pending', 'failure']);
});

const queue = { path: '.github/workflows/review-policy-events.yml', event: 'merge_group',
  head_repository: { full_name: 'celestiaorg/celestia-app' },
  head_branch: 'gh-readonly-queue/main/pr-1-abc', head_sha: 'd'.repeat(40), conclusion: 'success' };

test('queue revalidates current reviews and publishes on the merge-group SHA', async () => {
  const h = harness();
  await h.invoke('workflow_run', { workflow_run: queue });
  assert.equal(h.statuses.at(-1).state, 'success');
  assert.equal(h.statuses.at(-1).path, `/statuses/${queue.head_sha}`);
  const failing = harness({ pulls: [{ ...pr, title: 'docs: [high] policy change' }] });
  await failing.invoke('workflow_run', { workflow_run: queue });
  assert.equal(failing.statuses.at(-1).state, 'failure');
});

test('unidentifiable merge groups fail closed, and unrelated workflows cannot report', async () => {
  const h = harness({ comparison: 'diverged' });
  await h.invoke('workflow_run', { workflow_run: queue });
  assert.equal(h.statuses.at(-1).state, 'failure');
  const unrelated = harness();
  await unrelated.invoke('workflow_run', { workflow_run: { ...queue, path: 'attacker.yml' } });
  assert.equal(unrelated.statuses.length, 0);
});

test('fork review notifications refresh through trusted API data, with no artifacts', async () => {
  const h = harness();
  await h.invoke('workflow_run', { workflow_run: { ...queue, event: 'pull_request_review',
    head_repository: { full_name: 'contributor/fork' }, pull_requests: [] } });
  assert.equal(h.statuses.at(-1).state, 'success');
});

test('REST pagination includes later pages and API failures are not approvals', async () => {
  const request = async (url) => ({ ok: true, json: async () =>
    url.endsWith('page=1') ? Array.from({ length: 100 }, (_, i) => i) : [100] });
  assert.equal((await client('token', 'owner/repo', request).pages('/pulls')).length, 101);
  await assert.rejects(client('token', 'owner/repo', async () => ({ ok: false, status: 403 })).api('/pulls'), /HTTP 403/);
});


test('failed queue tests cannot produce a successful policy status', async () => {
  const h = harness();
  await h.invoke('workflow_run', { workflow_run: { ...queue, conclusion: 'failure' } });
  assert.equal(h.statuses.at(-1).state, 'failure');
});

test('review dismissal refreshes a previously sufficient approval set', async () => {
  const h = harness({ pulls: [{ ...pr, title: 'docs: [medium] update' }],
    reviews: [review('alice'), review('alice', 'DISMISSED', sha, 2)] });
  await h.invoke('workflow_run', { workflow_run: { ...queue,
    event: 'pull_request_review', pull_requests: [{ number: 1 }] } });
  assert.equal(h.statuses.at(-1).state, 'failure');
});

test('removing an agent sponsorship claim invalidates the current head', async () => {
  const h = harness({ pulls: [{ ...pr, user: bot }] });
  await h.invoke('issue_comment', { issue: { number: 1, pull_request: {} },
    comment: { body: `/review-policy sponsor ${sha}` } });
  assert.equal(h.statuses.at(-1).state, 'failure');
});
