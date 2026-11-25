# Provider policy check

decsr drives other vendors' CLIs and APIs, so each feature must stay inside
their terms. The strictest case today is the claude CLI on a Pro or Max
subscription login. Run every check below on a new feature; mark each one pass,
or fail with the guard that fixes it.

## Sources

Re-read these when a check is unclear or a rule may have changed:

- Claude Code legal and compliance: https://code.claude.com/docs/en/legal-and-compliance
- Anthropic Consumer Terms (section 2 accounts, section 3 automated access): https://www.anthropic.com/legal/consumer-terms
- Claude Code with a Pro or Max plan: https://support.claude.com/en/articles/11145838
- Usage limits: https://support.claude.com/en/articles/11647753
- For other runners (codex, opencode, dsh, API presets), the vendor's own terms.

## Checks

1. **Person present.** Anything that runs an agent with nobody at the session
   (cron automations, `/autonomy 3`, auto-retry after a limit, auto-continue)
   needs an API key for claude, or an explicit opt-in that warns first
   (`allow_subscription`, `--yes`). Use `runner.ClaudeAuthMode()` to tell
   `api-key` from `subscription`. Consumer Terms section 3 bans "automated or
   non-human means" except with an API key.
2. **Official binary only.** Run the installed, unmodified `claude` program
   through its documented flags. Patch nothing, and call no model endpoint as
   the user.
3. **Login tokens untouched.** Never read, copy, store or send
   `~/.claude/.credentials.json`, keychain entries or OAuth tokens, and never
   call private account endpoints such as `/api/oauth/usage`. Usage and limits
   come only from what the CLI prints (stream-json, error text, status-line
   data) or from an API the user gave a key for.
4. **Limits respected.** Hitting a limit waits for the reset or for the user.
   Never rotate accounts, never retry in a tight loop, and on a subscription
   claude keeps auto-retry off by default (`workspace.limit_auto_retry`).
5. **Ordinary, individual usage.** Fan-out stays bounded: `max_parallel`
   (3 by default, 8 at most) and `budget_usd`. Nothing runs around the clock
   like a service, and nothing serves other people through the user's login.
6. **No account sharing.** One user's login runs that user's own work only.
   Never offer decsr as a hosted service on a subscription login.
7. **Visible and documented.** Every automatic action prints an `auto ›` line.
   When a feature touches checks 1 to 6, update `doctor`, the README section
   "Using the claude runner" and `doc/automations.md` to match.

## Known state

decsr-tui is a git TUI that watches agent sessions. It starts no agent CLI,
calls no provider API and sends nothing over the network except what git itself
does when the user presses fetch, pull or push.

- Session watcher: pass. It only reads the session files the agent CLIs write
  for themselves (claude `~/.claude/projects`, codex `~/.codex/sessions`,
  opencode `storage/` JSON and `opencode.db`, the SQLite file opened
  read-only with `query_only`), and starts nothing. Reading files of the user's
  own account on the user's own machine is ordinary individual use.
- Login tokens: pass. The watcher never opens `~/.claude/.credentials.json`,
  codex `auth.json`, opencode `auth.json`, keychain entries or OAuth tokens, and
  calls no account or usage endpoint. Model names, token counts and times shown
  in the header and the live view come from the session files only.
- Limits and automation: pass. Nothing schedules, retries or waits for an agent
  run. The only timers are local polls of git (about 2 s) and of the session
  files (500 ms) while the TUI is open.
- Custom commands (`customCommands` in the config): user-written shell commands
  run by a key press the user configured. decsr-tui never adds one for an
  agent, and a command that starts an agent is the user's own action.
- Tests: pass. `go test` uses fake sources and temporary git repos; no test
  starts an agent CLI or reads the real `~/.claude`, `~/.codex` or opencode
  directories.
