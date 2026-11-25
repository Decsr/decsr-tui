<h1 align="center">decsr-tui</h1>

<p align="center"><b>A git client and a live agent watcher in one terminal screen, in the look of <a href="https://github.com/Decsr/Decsr-Code">decsr</a>.</b></p>

<p align="center">
  <a href="#what-it-is">What it is</a> ·
  <a href="#quick-start">Quick start</a> ·
  <a href="#the-screen">The screen</a> ·
  <a href="#keys">Keys</a> ·
  <a href="#configuration">Configuration</a> ·
  <a href="#agents-it-watches">Agents</a> ·
  <a href="#roadmap">Roadmap</a>
</p>

## What it is

decsr-tui is the git half of a coding session. It shows your working tree,
branches, commits and stashes next to what a coding agent is doing in the same
repository, so you can stage the agent's edits, commit them and check its
progress without leaving the terminal.

- **Git.** Stage files, hunks and single lines; commit, amend, reword, squash,
  fixup, drop, move and revert commits; edit an interactive rebase todo;
  cherry-pick from a clipboard; branches, tags, worktrees, stash, fetch, pull,
  push, reflog undo and redo; custom commands from the config.
- **Agents.** Watches claude, codex and opencode sessions of the repository:
  a live view of the running session, its activity tree with subagents, the
  edits it made as syntax-coloured diffs, and the files it changed against
  `HEAD`.
- **The decsr look.** It uses decsr's design system: the same theme tokens
  (`auto`, `dark` or `light`), framed panels with tab pills, the `▌` list
  cursor, runner colours and glyphs, the activity, live and diff renderers, the
  hint bar and status bar.

**decsr-tui never starts an agent.** It only reads the session files the agent
CLIs write themselves, read-only, and never opens a login or token file. See
[`doc/provider-policy.md`](doc/provider-policy.md).

## Quick start

decsr-tui is one binary, `decsr-tui`, and works the same on Ubuntu and Windows.
It needs `git` on the `PATH`. Go 1.26 or newer is needed only for the
`go install` and from-source routes.

The one-line installers download a published release and check its SHA-256
against the release `checksums.txt`. They work once a `v*` release exists. The
`go install` and from-source routes work right away.

### Ubuntu

```bash
sudo apt update && sudo apt install -y git curl    # prerequisites
```

**(a) One-line installer** (installs to `~/.local/bin`; set `DECSR_INSTALL_DIR` or `DECSR_VERSION` to change the directory or the release):

```bash
curl -fsSL https://raw.githubusercontent.com/Decsr/decsr-tui/main/scripts/install.sh | sh
```

If it says `~/.local/bin` is not on your `PATH`:

```bash
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc && source ~/.bashrc
```

**(b) `go install`** (needs Go 1.26+; get it from <https://go.dev/dl/>, the apt package is usually older):

```bash
go install github.com/Decsr/decsr-tui/cmd/decsr-tui@latest
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.bashrc && source ~/.bashrc   # once
```

**(c) From source:**

```bash
git clone https://github.com/Decsr/decsr-tui.git && cd decsr-tui
go build -o decsr-tui ./cmd/decsr-tui     # then: sudo install decsr-tui /usr/local/bin/
# or run without building:  go run ./cmd/decsr-tui
```

### Windows

Prerequisite: [Git for Windows](https://git-scm.com/download/win), for example
`winget install --id Git.Git -e`. Use Windows Terminal or any terminal with
ANSI colours. The commands below are PowerShell.

**(a) One-line installer** (installs to `%LOCALAPPDATA%\Programs\decsr-tui`, adds it to your user `PATH` if missing, needs no admin rights; set `$env:DECSR_INSTALL_DIR` or `$env:DECSR_VERSION` first to change the directory or the release):

```powershell
irm https://raw.githubusercontent.com/Decsr/decsr-tui/main/scripts/install.ps1 | iex
```

Open a new terminal afterwards so the `PATH` change takes effect.

**(b) `go install`** (needs Go 1.26+, `winget install --id GoLang.Go -e`):

```powershell
go install github.com/Decsr/decsr-tui/cmd/decsr-tui@latest
# once, add %USERPROFILE%\go\bin to your user PATH, then open a new terminal:
[Environment]::SetEnvironmentVariable('Path', [Environment]::GetEnvironmentVariable('Path','User') + ";$env:USERPROFILE\go\bin", 'User')
```

**(c) From source:**

```powershell
git clone https://github.com/Decsr/decsr-tui.git; cd decsr-tui
go build -o decsr-tui ./cmd/decsr-tui   # decsr-tui.exe on Windows
# or run without building:  go run ./cmd/decsr-tui
```

### Run it

```sh
cd your-repo && decsr-tui               # the current repository
decsr-tui /path/to/repo                 # another repository (Windows: C:\src\repo)
decsr-tui --theme dark                  # auto | dark | light
decsr-tui --session 3f2a                # open the agent session whose id starts with 3f2a
decsr-tui --version
```

### Where it reads and writes

| | Ubuntu | Windows |
|---|---|---|
| Config file | `~/.config/decsr-tui/config.yml` | `%AppData%\decsr-tui\config.yml` |
| claude sessions | `$CLAUDE_CONFIG_DIR/projects`, else `~/.claude/projects` | `%CLAUDE_CONFIG_DIR%\projects`, else `%USERPROFILE%\.claude\projects` |
| codex sessions | `$CODEX_HOME/sessions`, else `~/.codex/sessions` | `%CODEX_HOME%\sessions`, else `%USERPROFILE%\.codex\sessions` |
| opencode data | `$XDG_DATA_HOME/opencode`, else `~/.local/share/opencode` | `%XDG_DATA_HOME%\opencode`, else `%USERPROFILE%\.local\share\opencode` |
| Editor (`e` key) | `$EDITOR`, `$VISUAL`, then `nano`, `vi` or `vim` | `%EDITOR%`, `%VISUAL%`, then `notepad` |
| Custom commands | run with `sh -c` | run with `cmd /C` |

`$DECSR_TUI_CONFIG` or `--config` override the config path. Paths on Linux are
case-sensitive; on Windows and macOS they compare case-insensitively.

## Usage

```text
decsr-tui [--session <id-prefix>] [--config <file>] [--theme auto|dark|light] [--version] [repo-dir]
```

| Flag | Meaning |
|---|---|
| `--session <id>` | Open the agent session whose id starts with this, focused. |
| `--config <file>` | Config file. Default: `$DECSR_TUI_CONFIG`, else `<user config dir>/decsr-tui/config.yml`. |
| `--theme <mode>` | `auto` (ask the terminal), `dark` or `light`. Overrides the config. |
| `--version` | Print the version and exit. |

## The screen

```text
◆ decsr · myrepo  main ↑2 ↓1  REBASING                         ▘ ✳ claude · Fix the login bug
╭─ 1 Files · 3 ──────────────╮╭─ Diff a.go ───────────────────────────────────────────────╮
│ ▌ M  a.go                  ││ Unstaged changes                                          │
│    M  b.go                 ││ ── a.go ─────────────────────────────────────── +2 −1 ── │
│                            ││           @@ -10,6 +10,7 @@                               │
╰────────────────────────────╯│   10  10   func login() {                                 │
╭─ 2 Branches · Local ───────╮│   11      -	return nil                                    │
│ ▌* main                    ││       11 +	return check()                                │
╰────────────────────────────╯╰───────────────────────────────────────────────────────────╯
 ● idle    ▸ git add -A
  space stage/unstage · a stage/unstage all · enter staging view · c commit · ? help · q quit
```

(The other panels, Commits, Stash and Agent, sit under Branches.)

- **Header.** The decsr mark, the repository, a branch chip, ahead and behind,
  a `MERGING` or `REBASING` chip and a running `fetch`, `pull` or `push`. On the
  right, the agent chip: the live session in its runner's colour with a spinner
  while it works, or the last watched session dimmed.
- **Panels.** Five framed panels on the left; the focused one is open in its
  group colour and the others shrink to one row. Branches has the tabs
  `Local | Remotes | Tags | Worktrees`; the Agent panel has `Activity |
  Changes` once a session is open.
- **Main pane.** Follows the selection: diffs (syntax-coloured, with a
  line-number gutter), commit details, a branch log, the staging view, the
  rebase todo, or for the Agent panel the live view, the selected activity
  (messages as Markdown, edits as diffs) or the session's changes.
- **Status bar and hint bar.** The state and the last command-log line, then
  the keys of the focused panel.
- **Narrow terminals.** Below 60 columns it is one column: the focused panel,
  or the main pane while it has focus. `Esc` goes back.

## Keys

`?` shows the keys of the focused panel and the global ones; `x` opens them as
a menu.

### Global

| Key | Action |
|---|---|
| `tab`, `l`, `→` / `shift+tab`, `h`, `←` | Next / previous panel |
| `1`–`5` | Jump to a panel |
| `j`/`k`, `↑`/`↓`, `J`/`K`, `home`, `G` | Move, page, top, bottom |
| `ctrl+d`, `ctrl+u`, `pgdn`, `pgup` | Scroll the main pane |
| `/` | Filter the focused list |
| `f`, `p`, `P` | Fetch, pull, push (`esc` cancels a running one) |
| `z`, `Z` | Undo, redo through the reflog |
| `m` | Continue or abort a merge or rebase |
| `R` | Refresh |
| `@` | Show or hide the command log |
| `x`, `?`, `q` | Action menu, help, quit |

### Files

| Key | Action |
|---|---|
| `space` | Stage or unstage the file or folder |
| `a` | Stage or unstage everything |
| `enter` | Open the staging view (folder: collapse) |
| `d` | Discard (menu: unstaged only, or everything) |
| `c`, `A` | Commit, amend |
| `s`, `S` | Stash, stash options |
| `e` | Edit in `$EDITOR` (then `$VISUAL`, `notepad` on Windows, else `nano`, `vi` or `vim`) |
| `` ` `` | Tree or flat list |

**Staging view.** `j`/`k` move the cursor, `space` stages or unstages the
selection, `a` switches between hunk and line mode, `v` starts a line range,
`tab` switches between the unstaged and the staged diff, `d` discards, `esc`
leaves. The selected hunk or lines are marked in the diff.

### Branches

| Key | Action |
|---|---|
| `[` / `]` | Previous / next tab |
| `space` | Check out (tag: detached; worktree: switch to it) |
| `n`, `d`, `R` | New, delete, rename |
| `M`, `r`, `u` | Merge into current, rebase onto, set upstream |
| `P` (Tags) | Push the tag |
| `p` (Worktrees) | Prune |
| `enter` | Log of the branch in the main pane |

### Commits

| Key | Action |
|---|---|
| `enter` | Files of the commit (`esc` back) |
| `r`, `s`, `f`, `d` | Reword, squash into the one below, fixup, drop |
| `ctrl+j`, `ctrl+k` | Move the commit down or up |
| `e`, `i` | Edit (stop at it), interactive rebase todo |
| `A`, `F` | Amend with staged changes, fixup commit |
| `g`, `t` | Reset, revert |
| `C`, `V` | Copy for cherry-pick, paste |
| `ctrl+g` | Graph view |

Rebase todo: `p r s f d e` set the action, `ctrl+j`/`ctrl+k` move the row,
`enter` starts, `esc` cancels.

### Stash

`space` apply, `g` pop, `d` drop.

### Agent

| Key | Action |
|---|---|
| `enter` | Open a session; later expand a subagent or focus the main pane |
| `space` | Expand or collapse a subagent |
| `f` | Follow the newest activity (shows the live view) |
| `G` | Jump to the newest row and follow |
| `c` | Switch between Activity and Changes |
| `[` / `]` | Previous / next file in Changes |
| `esc` | Back to the session list |

Moving up with `k` switches follow off; then the main pane shows the selected
activity instead of the live view.

## Configuration

A YAML file at `<user config dir>/decsr-tui/config.yml` (or `--config`, or
`$DECSR_TUI_CONFIG`). A missing file means the defaults.

```yaml
theme: auto            # auto | dark | light
refresh:
  gitSeconds: 2        # git status poll
  agentMillis: 500     # session file poll
agents: [claude, codex, opencode]   # which agents to watch

customCommands:
  - key: "X"
    context: files          # files | branches | commits | stash | tags | worktrees | agent | global
    description: "Run tests"
    command: "go test ./..."
    output: popup           # log (default) | popup | terminal
  - key: "Y"
    context: commits
    description: "Show a commit"
    command: "git show {{.SelectedCommit}} --stat"
    confirm: true           # show the command and ask first
    prompts:
      - { name: "msg", title: "Message" }   # read as {{index .Form "msg"}}
```

Command templates can use `.SelectedFile`, `.SelectedCommit`,
`.SelectedBranch`, `.SelectedTag`, `.SelectedWorktree`, `.CheckedOutBranch`,
`.RepoDir` and `.Form`, and the `quote` function for shell quoting. Custom keys
show up in `?` and in the `x` menu.

## Agents it watches

| Agent | Read from | Status |
|---|---|---|
| claude | `$CLAUDE_CONFIG_DIR/projects` or `~/.claude/projects` | Verified on real session files |
| opencode | `$XDG_DATA_HOME/opencode` or `~/.local/share/opencode` (`opencode.db`, read-only, and the older `storage/` JSON) | Verified on real data (1.19.x) |
| codex | `$CODEX_HOME/sessions` or `~/.codex/sessions` | Written from the upstream rollout format, not yet checked on real files |

Sessions are matched to the repository by their working directory. decsr-tui
reads them read-only and starts nothing: no agent run, no login, no network
call. Set `agents:` in the config to watch fewer.

## Roadmap

- Check the codex watcher against real rollout files.
- A side-by-side diff mode and word-level highlights inside changed lines.
- Per-panel width settings and a user-set minimum width for the split layout.
- Token usage and cost trend in the live view.
- More watchers (copilot, dsh) once their session formats are verified.

## Development

```sh
go vet ./...
go test -count=1 ./...
go build -o decsr-tui ./cmd/decsr-tui   # decsr-tui.exe on Windows
```

Tests use fakes and temporary git repositories; none starts an agent or reads
the real agent directories. The UI is built from `internal/ui` (decsr's theme
tokens and components) and `internal/tui` (the screen). See
[`doc/tui.md`](doc/tui.md) for the screen reference.
