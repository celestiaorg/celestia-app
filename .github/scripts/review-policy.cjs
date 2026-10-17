'use strict';

// This module is executed only from the protected default branch, never a PR.
const levels = { low: 0, medium: 1, high: 2 };
const normalize = (login) => login.toLowerCase();
const human = (user) => user?.type === 'User' && !user.login.endsWith('[bot]');

function pathRisk(path) {
  // Review instructions and protocol specifications are policy, not ordinary docs.
  if (/^(docs\/(ai|plans)\/|specs\/|\.github\/|\.agents\/|\.claude\/|\.cursor\/)/.test(path)
      || /(^|\/)(AGENTS|CLAUDE|REVIEW)\.md$/.test(path)) return 'high';
  if (/^(README|CONTRIBUTING|CHANGELOG)\.md$/.test(path)
      || /^docs\/.*\.md$/.test(path)) return 'low';
  if (/(^|\/)(go\.mod|go\.sum)$/.test(path)) return 'high';
  if (path.endsWith('_test.go') || /^test\//.test(path)) return 'medium';
  // Unknown paths fail conservatively, including new production directories.
  return 'high';
}

function classify(title, files) {
  const tag = title.match(/^[a-z]+(?:\([^\r\n)]+\))?!?: \[(low|medium|high)\] \S/);
  if (!tag) throw new Error('PR title must contain a conventional-commit [low], [medium], or [high] tag.');
  if (!files.length) throw new Error('Cannot classify an empty or unavailable diff.');
  let risk = tag[1];
  for (const file of files) {
    // A rename must satisfy the old and new path rules.
    for (const path of [file.filename, file.previous_filename].filter(Boolean)) {
      const floor = pathRisk(path);
      if (levels[floor] > levels[risk]) risk = floor;
    }
  }
  return risk;
}

async function evaluate({ pr, files, reviews, comments, canWrite }) {
  const risk = classify(pr.title, files);
  let sponsor = human(pr.user) ? pr.user.login : null;
  if (!sponsor) {
    // A PR-body field cannot forge the identity of a human sponsor. A sponsor
    // must attest through their own comment and renew it for every new head.
    const command = `/review-policy sponsor ${pr.head.sha}`;
    const claims = comments.filter((c) => human(c.user) && c.body.trim() === command);
    const sponsors = new Set();
    for (const claim of claims) {
      if (await canWrite(claim.user.login)) sponsors.add(normalize(claim.user.login));
    }
    if (sponsors.size !== 1) {
      throw new Error(`Agent PR needs exactly one maintainer to comment: ${command}`);
    }
    [sponsor] = sponsors;
  }
  const trusted = await canWrite(sponsor);
  // Outside contributors always need at least one independent maintainer.
  const required = Math.max(levels[risk], trusted ? 0 : 1);
  const excluded = new Set([normalize(pr.user.login), normalize(sponsor)]);
  const latest = new Map();
  for (const review of [...reviews].sort((a, b) => a.id - b.id)) {
    if (!human(review.user) || ['COMMENTED', 'PENDING'].includes(review.state)) continue;
    latest.set(normalize(review.user.login), review);
  }
  const approved = [];
  const blocking = [];
  for (const [login, review] of latest) {
    if (excluded.has(login) || !(await canWrite(login))) continue;
    if (review.state === 'CHANGES_REQUESTED') blocking.push(login);
    if (review.state === 'APPROVED' && review.commit_id === pr.head.sha) approved.push(login);
  }
  const ok = !pr.draft && !blocking.length && approved.length >= required;
  return { ok, risk, sponsor, required, approved, blocking,
    summary: `${risk}: ${approved.length}/${required} independent approvals; sponsor ${sponsor}`
      + (pr.draft ? '; draft' : '') + (blocking.length ? '; changes requested' : '') };
}

// Minimal REST client: no npm dependencies and no PR-controlled programs.
function client(token, repository, request = fetch) {
  const root = `https://api.github.com/repos/${repository}`;
  async function api(path, method = 'GET', body) {
    const response = await request(`${root}${path}`, {
      method,
      headers: { Authorization: `Bearer ${token}`, Accept: 'application/vnd.github+json',
        'X-GitHub-Api-Version': '2022-11-28', 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: AbortSignal.timeout(30000),
    });
    if (!response.ok) throw new Error(`GitHub API ${method} ${path}: HTTP ${response.status}`);
    return response.json();
  }
  async function pages(path) {
    const items = [];
    for (let page = 1; ; page++) {
      const batch = await api(`${path}${path.includes('?') ? '&' : '?'}per_page=100&page=${page}`);
      items.push(...batch);
      if (batch.length < 100) return items;
    }
  }
  return { api, pages };
}

async function run({ event, eventName, repository, token, apiClient, report = console.log }) {
  const { api, pages } = apiClient || client(token, repository);
  const defaultBranch = event.repository.default_branch;
  const permissions = new Map();
  async function canWrite(login) {
    const key = normalize(login);
    if (!permissions.has(key)) {
      const data = await api(`/collaborators/${encodeURIComponent(login)}/permission`);
      permissions.set(key, ['admin', 'maintain', 'write'].includes(data.permission));
    }
    return permissions.get(key);
  }
  const target_url = `https://github.com/${repository}/actions/runs/${process.env.GITHUB_RUN_ID}`;
  async function status(sha, state, description) {
    await api(`/statuses/${sha}`, 'POST', {
      context: 'review-policy', state, description: description.slice(0, 140), target_url,
    });
  }
  async function check(number, publish = true) {
    const pr = await api(`/pulls/${number}`);
    if (pr.state !== 'open' || pr.base.ref !== defaultBranch) return null;
    if (publish) await status(pr.head.sha, 'pending', 'Evaluating risk and independent approvals');
    try {
      const [files, reviews, comments] = await Promise.all([
        pages(`/pulls/${number}/files`), pages(`/pulls/${number}/reviews`),
        pages(`/issues/${number}/comments`),
      ]);
      // GitHub caps the files endpoint at 3,000 entries. Never accept a partial diff.
      if (files.length !== pr.changed_files) throw new Error('Incomplete changed-file list.');
      const result = await evaluate({ pr, files, reviews, comments, canWrite });
      const fresh = await api(`/pulls/${number}`);
      if (fresh.head.sha !== pr.head.sha || fresh.base.sha !== pr.base.sha
          || fresh.title !== pr.title || fresh.draft !== pr.draft || fresh.state !== 'open') {
        throw new Error('PR changed during evaluation; rerun review-policy.');
      }
      if (publish) await status(pr.head.sha, result.ok ? 'success' : 'failure', result.summary);
      report(`#${number}: ${result.summary}${result.blocking.length ? '; changes requested' : ''}`);
      return { ...result, sha: pr.head.sha };
    } catch (error) {
      if (publish) await status(pr.head.sha, 'failure', error.message);
      report(`#${number}: ${error.message}`);
      return { ok: false, sha: pr.head.sha };
    }
  }

  async function checkHeads(pulls) {
    const groups = new Map();
    for (const pr of pulls) {
      if (!groups.has(pr.head.sha)) groups.set(pr.head.sha, []);
      groups.get(pr.head.sha).push(pr);
    }
    for (const [sha, group] of groups) {
      await status(sha, 'pending', 'Evaluating risk and independent approvals');
      const results = [];
      for (const pr of group) results.push(await check(pr.number, false));
      const ok = results.every((r) => r?.ok && r.sha === sha);
      const description = group.length === 1 && results[0]?.summary
        ? results[0].summary : `${group.length} PR(s) sharing this commit: ${ok ? 'passed' : 'review required'}`;
      await status(sha, ok ? 'success' : 'failure', description);
    }
  }
  async function refresh(number) {
    const pr = await api(`/pulls/${number}`);
    if (pr.base.ref !== defaultBranch || pr.state !== 'open') return;
    // Statuses belong to commits, not PRs. A second PR sharing this SHA must
    // not overwrite a stricter policy result with a less restrictive result.
    const pulls = await pages(`/pulls?state=open&base=${encodeURIComponent(defaultBranch)}`);
    await checkHeads(pulls.filter((p) => p.head.sha === pr.head.sha));
  }

  const workflow = event.workflow_run;
  if (eventName === 'workflow_run') {
    if (workflow.path !== '.github/workflows/review-policy-events.yml') return;
    if (workflow.event === 'merge_group') {
      if (workflow.head_repository?.full_name !== repository) return;
      const prefix = `gh-readonly-queue/${defaultBranch}/pr-`;
      if (!workflow.head_branch.startsWith(prefix)) return;
      const sha = workflow.head_sha;
      await status(sha, 'pending', 'Revalidating every queued PR against current reviews');
      try {
        if (workflow.conclusion !== 'success') throw new Error('Merge-group policy tests did not pass.');
        const candidates = await pages(`/pulls?state=open&base=${encodeURIComponent(defaultBranch)}`);
        const included = [];
        for (const pr of candidates) {
          const comparison = await api(`/compare/${pr.head.sha}...${sha}`);
          if (['ahead', 'identical'].includes(comparison.status)) included.push(pr);
        }
        if (!included.length) throw new Error('Cannot identify PRs in merge group.');
        for (const pr of included) {
          const result = await check(pr.number, false);
          if (!result?.ok || result.sha !== pr.head.sha) {
            throw new Error(`PR #${pr.number} no longer satisfies review policy.`);
          }
        }
        await status(sha, 'success', `${included.length} queued PR(s) satisfy review policy`);
      } catch (error) {
        await status(sha, 'failure', error.message);
      }
      return;
    }
    if (workflow.event !== 'pull_request_review') return;
    // Never trust artifacts or PR-provided output from the read-only notifier.
    if (workflow.pull_requests?.length) {
      for (const pr of workflow.pull_requests) await refresh(pr.number);
      return;
    }
    // Sweep to handle fork events whose workflow_run.pull_requests is empty.
  }
  if (eventName === 'pull_request_target') {
    await refresh(event.pull_request.number);
  } else if (eventName === 'issue_comment') {
    if (event.issue.pull_request && event.comment.body.trim().startsWith('/review-policy')) {
      await refresh(event.issue.number);
    }
  } else if (['workflow_run', 'schedule', 'workflow_dispatch'].includes(eventName)) {
    const pulls = await pages(`/pulls?state=open&base=${encodeURIComponent(defaultBranch)}`);
    await checkHeads(pulls);
  } else {
    throw new Error(`Unsupported event: ${eventName}`);
  }
}

module.exports = { classify, evaluate, pathRisk, run, client };
if (require.main === module) {
  const fs = require('node:fs');
  run({ event: JSON.parse(fs.readFileSync(process.env.GITHUB_EVENT_PATH, 'utf8')),
    eventName: process.env.GITHUB_EVENT_NAME, repository: process.env.GITHUB_REPOSITORY,
    token: process.env.GITHUB_TOKEN }).catch((error) => {
    console.error(error.message);
    process.exitCode = 1;
  });
}
