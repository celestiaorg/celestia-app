# AI Context Efficiency

Per-developer guidance for keeping AI tool context lean in this repo. Repo-level rules are in [AGENTS.md](../../AGENTS.md).

## What loads into every session

- `AGENTS.md` and its import `docs/ai/invariants.md` load into the main loop and into every spawned sub-agent. Anything added there is paid once per agent. Keep additions short; link to other docs instead of `@`-importing them.
- Periodically audit what loads at session start — connectors, global skills, memory files — and remove what your current work does not need.

## Tool configuration

- With 10 or more connectors enabled, switch tool access from Auto to On demand. Tool definitions are context paid on every turn.
- Disable global skills that are irrelevant to this repo. Always-included skills pollute every session.

## Model choice

- One rule: cheap models explore and gather; frontier models think and orchestrate. Token-heavy research — codebase exploration, log digging, web searches — goes to sub-agents on a cheap model that report conclusions back.
- Keep the frontier model for orchestration, design, hard debugging, final synthesis, and judgment — including review: reviewer agents run on the frontier model, fed by the cheap `context-gatherer` (see `.agents/skills/implement/SKILL.md`).
- Store durable context in files (`docs/plans/` notes, memory) instead of long chat history.
