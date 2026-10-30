#!/usr/bin/env python3
"""File a Linear issue for each fuzz target listed in FUZZ_LOG_DIR/failed.txt.

Fuzz output and crashing inputs may describe an exploitable bug, so the nightly
job keeps them out of public logs and this script sends them only to Linear.
An open issue with the same title gets a comment instead of a duplicate.

Environment: LINEAR_API_KEY, LINEAR_TEAM_KEY, LINEAR_ASSIGNEE_ID, FUZZ_LOG_DIR,
RUN_URL, COMMIT_SHA.
"""
import json
import os
import pathlib
import re
import sys
import urllib.request

API = "https://api.linear.app/graphql"
# Registry priority (scripts/fuzz_targets.txt) to Linear priority.
PRIORITIES = {"1": 1, "2": 2, "3": 3}
MAX_LOG_CHARS = 20000
MAX_INPUT_CHARS = 8000


def graphql(query, variables):
    request = urllib.request.Request(
        API,
        data=json.dumps({"query": query, "variables": variables}).encode(),
        headers={
            "Authorization": os.environ["LINEAR_API_KEY"],
            "Content-Type": "application/json",
        },
    )
    with urllib.request.urlopen(request, timeout=30) as response:
        body = json.load(response)
    if body.get("errors"):
        sys.exit("Linear API error: " + json.dumps(body["errors"]))
    return body["data"]


def fence(text):
    return "```\n" + text.replace("```", "`​``") + "\n```"


def crashing_input(pkg, log):
    match = re.search(r"Failing input written to (\S+)", log)
    if not match:
        return None
    path = pathlib.Path(pkg) / match.group(1)
    try:
        return path.read_text(errors="replace")[:MAX_INPUT_CHARS]
    except OSError:
        return None


def describe(pkg, target, log):
    lines = [
        f"Nightly fuzz target `{target}` in `{pkg}` failed.",
        "",
        f"- Run: {os.environ['RUN_URL']}",
        f"- Commit: `{os.environ['COMMIT_SHA']}`",
        "",
        "Reproduce by saving the crashing input below as "
        f"`{pkg}/testdata/fuzz/{target}/<name>` and running "
        f"`go test -run='^{target}$' {pkg}`.",
        "",
        "Keep crash details out of public PRs and issues; see docs/fuzzing.md.",
    ]
    crash = crashing_input(pkg, log)
    if crash:
        lines += ["", "## Crashing input", "", fence(crash)]
    lines += ["", "## Output (tail)", "", fence(log[-MAX_LOG_CHARS:])]
    return "\n".join(lines)


def main():
    log_dir = pathlib.Path(os.environ["FUZZ_LOG_DIR"])
    failed_file = log_dir / "failed.txt"
    # A step other than a fuzz target failed, e.g. setup or the registry check.
    if not failed_file.exists():
        print("No failing fuzz targets recorded.")
        return
    failed = failed_file.read_text().split("\n")
    team = graphql(
        "query($key: String!) { teams(filter: {key: {eq: $key}}) { nodes { id } } }",
        {"key": os.environ["LINEAR_TEAM_KEY"]},
    )["teams"]["nodes"][0]["id"]

    for line in filter(None, failed):
        priority, pkg, target = line.split()
        log = (log_dir / f"{target}.log").read_text(errors="replace")
        title = f"Nightly fuzz failure: {target}"
        description = describe(pkg, target, log)
        existing = graphql(
            """query($team: ID!, $title: String!) {
              issues(filter: {
                team: {id: {eq: $team}},
                title: {eq: $title},
                state: {type: {nin: ["completed", "canceled"]}}
              }) { nodes { id identifier } }
            }""",
            {"team": team, "title": title},
        )["issues"]["nodes"]
        if existing:
            graphql(
                "mutation($input: CommentCreateInput!) { commentCreate(input: $input) { success } }",
                {"input": {"issueId": existing[0]["id"], "body": description}},
            )
            print(f"{target}: commented on {existing[0]['identifier']}")
            continue
        issue = graphql(
            """mutation($input: IssueCreateInput!) {
              issueCreate(input: $input) { issue { identifier } }
            }""",
            {"input": {
                "teamId": team,
                "title": title,
                "description": description,
                "priority": PRIORITIES.get(priority, 2),
                "assigneeId": os.environ["LINEAR_ASSIGNEE_ID"],
            }},
        )["issueCreate"]["issue"]
        print(f"{target}: created {issue['identifier']}")


if __name__ == "__main__":
    main()
