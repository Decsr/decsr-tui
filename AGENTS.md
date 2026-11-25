# AGENTS.md

Behavioral guidelines to reduce common LLM coding mistakes. Merge with project-specific instructions as needed.

**Tradeoff:** These guidelines bias toward caution over speed. For trivial tasks, use judgment.

## 0. Git: Never Commit or Push for the User

**The user controls git. Do NOT run `git commit`, `git push`, `git pull`, or any
history-changing git command unless the user explicitly asks in that message.**

Instead:
- Make and verify code changes, run tests/builds/linters as needed.
- When work is ready, **provide the git commit message as text** for the user to
  copy and run themselves (include the `Co-authored-by` trailer).
- Leave staging, committing, and pushing to the user.

Do not auto-push. Summarize what changed and hand the user a ready-to-use
commit message.

## 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:
- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them - don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

## 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

## 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:
- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it - don't delete it.

When your changes create orphans:
- Remove imports/variables/functions that YOUR changes made unused.
- Don't remove pre-existing dead code unless asked.

The test: Every changed line should trace directly to the user's request.

## 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:
- "Add validation" → "Write tests for invalid inputs, then make them pass"
- "Fix the bug" → "Write a test that reproduces it, then make it pass"
- "Refactor X" → "Ensure tests pass before and after"

For multi-step tasks, state a brief plan:
```
1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
```

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.

## 5. Provider Policy Check

**Every new feature passes the provider policy check before it is called done.**

decsr-tui never starts an agent; it reads the session files that claude, codex
and opencode write. A feature that starts, schedules, retries or talks to an
agent CLI or a provider API, or that reads login state, is in scope; so is one
that reads a new agent file. Run every check in
[`doc/provider-policy.md`](doc/provider-policy.md) against the design before
building, and again against the finished diff. The check is done when each item
is marked pass, or fail with the guard that fixes it. Report the result in the
hand-off. When a check fails and no guard fits, stop and ask the user before
building.

`go test` uses fakes: no test may start an agent CLI or read the real
`~/.claude`, `~/.codex` or opencode data directories.

---

**These guidelines are working if:** fewer unnecessary changes in diffs, fewer rewrites due to overcomplication, and clarifying questions come before implementation rather than after mistakes.

## Model Usage Policy

- **Claude Opus** — design and management only: architecture, planning, task breakdown, delegation, supervision, code review and verification. Opus does not write implementation code.
- **Claude Haiku** — searching code and documentation (exploration, lookups, research sub-agents).
- **Claude Sonnet** — all hands-on work: implementation, tests, refactors, docs, config, scripts, builds and fixes for review findings.
- **GPT \*-sol** — UI/UX design only (layouts, user flows, visual and interaction design, design review). GPT models are banned from writing or editing code.

## Graphify Knowledge Graph

Applies when the repository tracks `graphify-out/`.

- A global git hook (`~/.githooks`) runs `graphify update` in the background after every commit, merge and checkout (10-minute debounce), so `graphify-out/` can change minutes after a commit and be left uncommitted and unpushed.
- Before every commit: run `graphify update .` synchronously and stage `graphify-out/` in the same commit as the change.
- Before finishing a task: wait until the background rebuild is done (`graphify-out/.autoupdate.lock` absent), then check that `git status` is clean and `git log origin/<branch>..HEAD` is empty.
- If `graphify-out/` differs only in the `Built from commit` freshness hash, discard it with `git checkout -- graphify-out`; committing a hash-only change just retriggers the hook in an endless loop.
- If it contains real graph changes (nodes, edges, communities), commit them as `chore(graphify): refresh graph` and push to the same branch.
- Never commit `graphify-out/.autoupdate.*` (lock, stamp, log).
