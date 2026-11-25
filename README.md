<p align="center">
  <img src="doc/assets/banner.svg" alt="decsr-tui: git + live agent watcher" width="720">
</p>

<p align="center"><b>A git client and a live coding-agent watcher in one terminal UI.</b></p>

<p align="center">
  <a href="https://github.com/Decsr/decsr-tui/actions/workflows/ci.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/Decsr/decsr-tui/ci.yml?branch=main&label=CI"></a>
  <a href="https://github.com/Decsr/decsr-tui/releases/latest"><img alt="Latest release" src="https://img.shields.io/github/v/release/Decsr/decsr-tui?label=release"></a>
  <a href="go.mod"><img alt="Go version" src="https://img.shields.io/github/go-mod/go-version/Decsr/decsr-tui"></a>
  <a href="LICENSE"><img alt="Licence" src="https://img.shields.io/github/license/Decsr/decsr-tui"></a>
</p>

<p align="center">
  <a href="#what-it-is">What it is</a> ·
  <a href="#screens">Screens</a> ·
  <a href="#features">Features</a> ·
  <a href="#quick-start">Quick start</a> ·
  <a href="#usage">Usage</a> ·
  <a href="#keys">Keys</a> ·
  <a href="#configuration">Configuration</a> ·
  <a href="#supported-agents">Agents</a> ·
  <a href="#privacy-and-safety">Privacy</a> ·
  <a href="#troubleshooting">Troubleshooting</a>
</p>

## What it is

decsr-tui is one terminal program with three screens. **Home** lists your
repository and the coding-agent sessions found for it. **Git** is a
lazygit-style client: stage files, hunks and single lines, rebase
interactively, cherry-pick, stash and more. **Watch** shows a live coding-agent
session (claude, opencode or codex) the way [decsr](https://github.com/Decsr/Decsr-Code)
shows it: the agent and its subagents as a node graph, with a live pane for
tokens, context, cost estimate and changed files.

It is a fork of the decsr orchestrator with the orchestrator removed. It only
reads the session files that the agent CLIs write; it never starts, drives or
talks to an agent.

**Why**

- Review and stage an agent's edits, then commit, in the same terminal where it
  runs, without switching tools.
- See what a long agent session is doing: which subagent is running, how full the
  context window is, whether a node has stalled.
- No agent is started and no network is used (except the git network commands
  you trigger yourself), so it is safe to leave open next to any agent.
- One static binary for Ubuntu and Windows, plus a plain YAML config file.

## Screens

Switch with `H` Home, `G` Git and `W` Watch from any screen. The program starts
on Home (or on Watch with `--session`). The previews below are text renderings
of the real screens.

**Home**: the logo, the sessions of this repository, the repo state and a menu.

```text
◆ decsr · myrepo  [H Home] G Git W Watch   main                  [▘ ✳ claude 1.2M ×40]

                           ·       ✦        +      ⋆       ✦
                            ·       █▀▀▄ █▀▀▀ ▄▀▀▀ █▀▀▀ █▀▀▄  ·
                           ⋆        █  █ █▀▀  █     ▀▀▄ █▀█     ✦
                              ·     ▀▀▀  ▀▀▀▀  ▀▀▀ ▀▀▀▀ ▀  ▀   ✦
                                         git + live agent watcher

 ┌─ ◆ SESSIONS · 2 ──────────────────────────────┐ ┌─ ◆ REPO ──────────────────────────┐
 │ ▌● ✳ Fix the login bug       4 nodes · now    │ │ BRANCH   main                     │
 │  ○ ⬡ Add retry to fetch      1 node · 3h      │ │ STATE    ✓ clean                  │
 │                                               │ │ FILES    ● 0 staged  ◐ 1 unstaged │
 └───────────────────────────────────────────────┘ └───────────────────────────────────┘
                                                   ┌─ ◆ MENU ──────────────────────────┐
                                                   │ ▌◉ Watch  follow a session        │
                                                   │  ◆ Git    status, branches, ...   │
                                                   │  ? Help   the keys of every screen│
                                                   └───────────────────────────────────┘
  ↑↓  move   ←→  list   enter  open   W  watch   G  git   ?  help   q  quit
```

**Git**: five panels on the left; the main pane follows the selection.

```text
◆ decsr · myrepo  H Home [G Git] W Watch   main ↑2 ↓1          [▘ ✳ claude · Fix the login bug]
╭─ 1 Files · 3 ──────────────╮╭─ Diff a.go ───────────────────────────────────────────────╮
│ ▌ M  a.go                  ││ Unstaged changes                                          │
│    M  b.go                 ││ ── a.go ─────────────────────────────────────── +2 −1 ── │
│   ?? new.go                ││           @@ -10,6 +10,7 @@                               │
╰────────────────────────────╯│   10  10   func login() {                                 │
╭─ 2 Branches · Local ───────╮│   11      -	return nil                                    │
│ ▌* main                    ││       11 +	return check()                                │
╰────────────────────────────╯╰───────────────────────────────────────────────────────────╯
 ● idle    ▸ git add -A
  space stage/unstage · a stage/unstage all · enter staging view · c commit · ? help · q quit
```

Panels 3 Commits, 4 Stash and 5 Agent sit under Branches; the focused panel is
the one that is open.

**Watch**: the session is the root node, every subagent a child node.

```text
◆ decsr · Decsr-Code  H Home G Git [W Watch]   main        [▘ ✳ claude 19.1M ×103]  [✓ Explore 415k ×13]
  Graph                          │  Nodes · 1/16 · follow           │  Live · claude ──────── v hide
 ╭──────────────────────────╮    │▌╭──────────────────────────────╮ │  ▘ claude · claude-opus-5-5 · 2h 10m
 │ claude  Decsr TUI with … │    │▌│ ▘ claude Decsr TUI …  ✳ opus │ │  ctx 276k / 1M ██░░░░ 28%
 │ ✳ claude-opus-5-5 ▘ run  │    │▌│ ▘ Working for 2h 10m · 103 … │ │    ▸ Read CI result        ✓ 0.4s
 ╰──────────────────────────╯    │ ╭──────────────────────────────╮ │    · thinking…
                                 │ │ ✓ Explore Map decsr …  ✳ haiku│ │    ▸ Get failed CI job logs ✓ 4.9s
```

Below 120 columns the live pane moves under the cards; below 80 the graph is
left out.

## Features

**Watch**

- Node graph, one card per node and a live pane for the selected node.
- The session and each subagent (nested ones too) as nodes, with status glyphs.
- Context meter (`ctx 276k / 1M`) when the model's window is known.
- Tokens in, out and cached; estimated cost (labelled `est.`) for models you
  price in the config.
- Stall warning on a running node that has been silent for a minute.
- Live text and an animated `thinking…` line, a timeline per node, and a full
  detail overlay for any step.
- Changed files of the node with their diffs.
- Follow mode keeps the newest running node and step selected.
- Reads claude, opencode and codex sessions side by side.

**Git**

- Files panel with a tree or flat list; stage files, hunks and single lines
  (with ranges); discard; commit, amend; edit in your editor.
- Branches, remotes, tags and worktrees: check out, create, rename, delete,
  merge, rebase, set upstream, push a tag, prune.
- Commits: log and graph, reword, squash, fixup, drop, move, edit, an
  interactive rebase todo, amend, fixup commit, reset, revert, cherry-pick
  (copy and paste).
- Stash: apply, pop, drop. Fetch, pull and push (cancellable, with force with
  lease on a rejected push). Merge and rebase continue or abort.
- Undo and redo through the reflog. Filter any list with `/`. Syntax-coloured
  diffs and a command log.

**General**

- Themes: `auto` (asks the terminal), `dark` or `light`; `NO_COLOR` is honoured.
- Custom commands bound to keys in any panel, with prompts and a confirm, using
  templates such as `{{.SelectedCommit}}`.
- Ubuntu and Windows: one-line installers, `go install`, or build from source.
- Mouse wheel and click support in the Git screen.

## Quick start

decsr-tui is one binary, `decsr-tui`, and works the same on Ubuntu and Windows.
It needs `git` on the `PATH`. Go 1.26 or newer is needed only for the
`go install` and from-source routes.

The one-line installers download a published release and check its SHA-256
against the release `checksums.txt`.

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
go build -o decsr-tui.exe ./cmd/decsr-tui
# or run without building:  go run ./cmd/decsr-tui
```

Release archives for other platforms (macOS, arm64) are also attached to each
GitHub release; they are not a tested target.

### Check it works

```sh
decsr-tui --version
cd your-repo && decsr-tui
```

### Upgrade

Re-run the installer (it installs the latest release over the old binary; close
a running `decsr-tui` first on Windows), or run
`go install github.com/Decsr/decsr-tui/cmd/decsr-tui@latest` again. To pin a
release with the installer, set `DECSR_VERSION` (for example `v0.4.0`).

### Uninstall

| | Ubuntu | Windows |
|---|---|---|
| Binary (installer) | `rm ~/.local/bin/decsr-tui` | delete `%LOCALAPPDATA%\Programs\decsr-tui` and remove it from your user `PATH` |
| Binary (`go install`) | `rm ~/go/bin/decsr-tui` | delete `%USERPROFILE%\go\bin\decsr-tui.exe` |
| Config (optional) | `rm -r ~/.config/decsr-tui` | delete `%AppData%\decsr-tui` |

If you set `DECSR_INSTALL_DIR`, `GOBIN` or `GOPATH`, the binary is there
instead. decsr-tui writes nothing else.

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
case-sensitive; on Windows they compare case-insensitively.

## Usage

```text
decsr-tui [--session <id-prefix>] [--config <file>] [--theme auto|dark|light] [--version] [repo-dir]
```

| Flag | Meaning |
|---|---|
| `repo-dir` | The repository to open. Default: the current directory. |
| `--session <id>` | Open the Watch screen on the session whose id starts with this. |
| `--config <file>` | Config file. Default: `$DECSR_TUI_CONFIG`, else `<user config dir>/decsr-tui/config.yml`. |
| `--theme <mode>` | `auto` (ask the terminal), `dark` or `light`. Overrides the config. |
| `--version` | Print the version and exit. |

```sh
cd your-repo && decsr-tui               # the current repository
decsr-tui /path/to/repo                 # another repository (Windows: C:\src\repo)
decsr-tui --theme light
decsr-tui --session 3f2a                # straight to Watch on the session whose id starts with 3f2a
```

Screen keys, from any screen: `H` Home, `G` Git, `W` Watch, `?` help, `q` quit.
(Once you are on Git, `G` on a list jumps to its bottom.)

## Keys

`?` shows the keys of the focused panel and the global ones; on Git, `x` opens
them as a menu. The full reference is [`doc/tui.md`](doc/tui.md).

### Home

| Key | Action |
|---|---|
| `↑` `↓`, `j` `k`, `home` `end` | Move |
| `tab`, `←` `→`, `h` `l` | Switch between Sessions and Menu |
| `enter`, `space` | Open the session in Watch, or the menu entry |
| `w`, `g` | Go to Watch, Git |
| `R`, `?` | Refresh, help |

### Watch

| Key | Action |
|---|---|
| `↑` `↓`, `j` `k`, `home` `end` | Previous or next node, first, last |
| `←` `→`, `h` `l` | Parent node, first child node |
| `f` | Follow the newest running node and step; moving turns it off |
| `space` | Expand the card to its whole timeline |
| `[` `]` | Older or newer step of the node |
| `enter` | Full detail of the selected step in an overlay (`esc` closes) |
| `v` | Hide or show the live pane |
| `n` `p` | Next or previous changed file of the node |
| `pgup` `pgdn`, `ctrl+u` `ctrl+d` | Scroll the diff |
| `R` | Refresh |

### Git: global

| Key | Action |
|---|---|
| `tab`, `l`, `→` / `shift+tab`, `h`, `←` | Next / previous panel |
| `1`–`5` | Jump to a panel |
| `j`/`k`, `↑`/`↓`, `home`, `G` | Move, top, bottom |
| `ctrl+d`, `ctrl+u`, `pgdn`, `pgup` | Scroll the main pane |
| `/` | Filter the focused list |
| `f`, `p`, `P` | Fetch, pull, push (`esc` cancels a running one) |
| `z`, `Z` | Undo, redo through the reflog |
| `m` | Continue or abort a merge or rebase |
| `R` | Refresh |
| `@` | Show or hide the command log |
| `x`, `?`, `q` | Action menu, help, quit |

### Git: Files

| Key | Action |
|---|---|
| `space` | Stage or unstage the file or folder |
| `a` | Stage or unstage everything |
| `enter` | Open the staging view (folder: collapse) |
| `d` | Discard (menu: unstaged only, or everything) |
| `c`, `A` | Commit, amend |
| `s`, `S` | Stash, stash options |
| `e` | Edit in your editor |
| `` ` `` | Tree or flat list |

**Staging view.** `j`/`k` move the cursor, `space` stages or unstages the
selection, `a` switches between hunk and line mode, `v` starts a line range,
`tab` switches between the unstaged and the staged diff, `d` discards, `esc`
leaves.

### Git: Branches

| Key | Action |
|---|---|
| `[` / `]` | Previous / next tab (Local, Remotes, Tags, Worktrees) |
| `space` | Check out (tag: detached; worktree: switch to it) |
| `n`, `d`, `R` | New, delete, rename |
| `M`, `r`, `u` | Merge into current, rebase onto, set upstream |
| `P` (Tags) | Push the tag |
| `p` (Worktrees) | Prune |
| `enter` | Log of the branch in the main pane |

### Git: Commits

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

### Git: Stash and Agent

Stash: `space` apply, `g` pop, `d` drop.

Agent (the session list and activity tree inside the Git screen): `enter` open,
`space` expand a subagent, `f` follow, `G` newest, `c` Activity or Changes,
`[` `]` file in Changes, `esc` back.

## Configuration

A YAML file at the path below. A missing file means the defaults; an unknown
key or a bad value is an error at startup.

| | Path |
|---|---|
| Ubuntu | `~/.config/decsr-tui/config.yml` |
| Windows | `%AppData%\decsr-tui\config.yml` |
| Any | `--config <file>` or `$DECSR_TUI_CONFIG` |

```yaml
theme: auto            # auto | dark | light
refresh:
  gitSeconds: 2        # git status poll, seconds (> 0)
  agentMillis: 500     # session file poll, milliseconds (> 0)
agents: [claude, codex, opencode]   # which agents to watch

pricing:               # optional: a cost shows (labelled est.) only for a model with a price
  "claude-*":          # a model id or a glob; USD per million tokens
    input_per_mtok: 3
    output_per_mtok: 15
    cache_read_per_mtok: 0.3   # optional; cached input uses input_per_mtok when 0

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

- **`theme`**: `auto` asks the terminal for its background once at startup
  (dark in tmux, over a pipe, or when it does not answer). `--theme` overrides it.
- **`refresh`**: how often git status and the agent session files are polled.
- **`agents`**: the watchers to enable. Leave a name out to stop reading that
  agent's files.
- **`pricing`**: there are no built-in prices. A model without an entry shows no
  cost. The cost is an estimate from token counts.
- **`customCommands`**: a command bound to `key` in a `context`. It shows up in
  `?` and in the `x` menu. `output: log` writes to the command log, `popup`
  shows the output, `terminal` runs it in the terminal. Templates can use
  `.SelectedFile`, `.SelectedCommit`, `.SelectedBranch`, `.SelectedTag`,
  `.SelectedWorktree`, `.CheckedOutBranch`, `.RepoDir` and `.Form`, and the
  `quote` function for shell quoting.

## Supported agents

| Agent | Sessions are read from (Ubuntu / Windows) | Status |
|---|---|---|
| claude | `$CLAUDE_CONFIG_DIR/projects` or `~/.claude/projects` / `%USERPROFILE%\.claude\projects` | Verified on real session files |
| opencode | `$XDG_DATA_HOME/opencode` or `~/.local/share/opencode` / `%USERPROFILE%\.local\share\opencode` (`opencode.db` opened read-only, and the older `storage/` JSON) | Verified on a real run (1.19.x) |
| codex | `$CODEX_HOME/sessions` or `~/.codex/sessions` / `%USERPROFILE%\.codex\sessions` | Built from the upstream rollout format; not yet verified on a real run |

Sessions are matched to the repository by their working directory.

## Privacy and safety

- **Read-only.** decsr-tui never starts, drives or talks to an agent, and
  writes nothing to an agent's directories.
- **Files it reads:** claude's session transcripts under `projects/`, codex's
  rollout files under `sessions/`, and opencode's `opencode.db` (opened
  read-only) or its `storage/` JSON. It also reads your repository through `git`,
  and its own config file.
- **Files it never reads:** login or credential files such as claude's
  `.credentials.json`, codex's `auth.json` and `config.toml`, or opencode's
  `auth.json`.
- **No network** of its own. The only network traffic is what git does when you
  press fetch, pull or push (`f`, `p`, `P`), or what a custom command of yours
  does.
- It runs `git` and, for `e`, your editor, in your repository. Custom commands
  run whatever you put in the config.

See [`doc/provider-policy.md`](doc/provider-policy.md) for the provider policy
checks this project follows.

## Troubleshooting

- **"No sessions found".** Sessions are matched by working directory, so the
  repository path must be the directory the agent ran in. The match ignores case
  on Windows and macOS, but not on Linux. Start decsr-tui from that directory,
  or pass it as `repo-dir`. Check that the agent is enabled under `agents:` and
  that its directory in the table above exists (claude sessions are stored in a
  folder named after the path, with every character that is not a letter, digit
  or `-` turned into `-`).
- **Colours look wrong.** Use a truecolor terminal. If the background guess is
  wrong, force `--theme dark` or `--theme light`. `NO_COLOR` turns colour off.
- **Fonts and glyphs.** The UI uses box drawing, block and a few symbol
  characters. A Nerd Font is optional and not needed. decsr recommends
  [Monaspace](https://monaspace.githubnext.com/). If glyphs show as boxes, switch
  the terminal font.
- **Windows.** Use Windows Terminal. The old console host (a plain `cmd.exe` or
  PowerShell window) has limited colour and glyph support.
- **`decsr-tui: command not found` after `go install`.** Add Go's bin directory
  to your `PATH`: `~/go/bin` on Ubuntu, `%USERPROFILE%\go\bin` on Windows (see
  Quick start), then open a new terminal. `go env GOBIN GOPATH` shows where it
  went.
- **Installer cannot find a release.** Set `DECSR_VERSION` to a tag such as
  `v0.4.0`, or use `go install`.

## Roadmap

See [`doc/roadmap.md`](doc/roadmap.md): verifying codex on a real run, an
Antigravity watcher, submodules, bisect, a command palette, mouse support in
Watch, built-in prices and packaging.

## Contributing

Issues and pull requests are welcome. Please keep a change small and focused.

```sh
gofmt -l .                              # must print nothing
go vet ./...
go test -count=1 ./...
go build -o decsr-tui ./cmd/decsr-tui   # decsr-tui.exe on Windows
```

Tests use fakes and temporary git repositories; none starts an agent or reads
the real agent directories. The UI is built from `internal/ui` (theme tokens and
components) and `internal/tui` (the screens). See [`doc/tui.md`](doc/tui.md).
Add a line to [`CHANGELOG.md`](CHANGELOG.md) for a user-visible change.

## Licence

MIT. See [`LICENSE`](LICENSE).

## Credits

decsr-tui is a fork of [decsr](https://github.com/Decsr/Decsr-Code) with the
orchestrator removed. Its git workflow is inspired by
[lazygit](https://github.com/jesseduffield/lazygit). It is built with
[Bubble Tea](https://github.com/charmbracelet/bubbletea),
[Lip Gloss](https://github.com/charmbracelet/lipgloss) and
[Chroma](https://github.com/alecthomas/chroma).
