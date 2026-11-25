# TUI reference

The screen of decsr-tui, panel by panel, and every key. For install and the
config file see the [README](../README.md).

## Screens

Three screens, switched from the header tabs or with a key from any screen
(`1`-`5` stay the panels inside Git):

| Key | Screen |
|---|---|
| `H` | **Home**: the starfield logo on a painted dark background and three boxes: Sessions (every runner, newest first, live dot, age, node count), Repo (branch, ahead and behind, staged, unstaged and untracked counts, state) and Menu (Watch, Git, Help). `↑↓` (`j` `k`, `home` `end`) move, `←→` (`h` `l`) or `tab` switch list, `enter` or `space` opens the session in Watch or the menu entry, `w` and `g` jump to Watch and Git, `R` refreshes. The program starts here unless `--session` is given. |
| `G` | **Git**: the five panels and the main pane described below. On this screen `G` stays "bottom of the list". |
| `W` | **Watch**: decsr's Workspace for one session. The session is the root node, every subagent a child node (nested ones deeper). Left the node graph, in the middle one card per node, on the right the live pane of the selected node; below 120 columns the live pane moves under them, below 80 the graph is left out. |

Every screen has the same chrome: the header, a notice line under it, the screen, the status line and the footer of keycap pills.

### Watch keys

| Key | Action |
|---|---|
| `↑↓`, `j k`, `home` `end` | Previous or next node, first or last; the graph and the feed move together |
| `←` `→`, `h` `l` | Parent node, first child node |
| `f` | Follow: keep the newest node and step selected while the session is live. Moving turns it off |
| `space` | Expand the card to its whole timeline |
| `[` `]` | Older or newer step of the node |
| `enter` | Full detail of the selected step in an overlay (`esc` closes) |
| `v` | Hide or show the live pane |
| `n` `p` | Next or previous changed file of the node |
| `pgup` `pgdn`, `ctrl+u` `ctrl+d` | Scroll the diff of the selected file |
| `R`, `?` | Refresh, help |

The live pane shows the running line (runner, model, duration, tokens in and out, the cached part; `~` marks the counts of a running node as estimates), `ctx 18k / 200k` with a meter (only when the window of the model is known), the cost (only when a price is configured under `pricing:`, labelled `est.`), the timeline with live text and an animated `thinking...`, and the changed files with the diff of the selected one. A running node that has been silent for a minute says `no activity for 1m`.

## Layout (Git)

```text
header          ◆ decsr · repo  H Home G Git W Watch  [branch] ↑↓  [REBASING]   agent strip
notice line     ✓ push done   (fades after a few seconds)
left column     five framed panels, the focused one open        main pane (one frame)
status line     ● state   ▸ last command-log line
footer          keycap pills: the keys of the focused panel · ? help · q quit
```

- The left column is 35% of the width (30 to 56 cells), the main pane the rest.
- The focused panel is open and drawn in its group colour: Files teal, Branches
  violet, Commits cyan, Stash amber, Agent pink. The others are one row high
  with a dim border. The main pane takes the colour of the focused panel while
  it has focus.
- From 33 rows the command log (`@`) shows under the panels.
- **Narrow or short terminals.** Below 60 columns, or when the five panels do
  not fit, only the focused panel shows. While the main pane has focus (the
  staging view, the rebase todo, scroll mode) a narrow screen shows it alone.
  `Esc` goes back.
- A panel 6 rows or taller shows its tab bar; a shorter one names the active
  tab in its title (`2 Branches · Local`). A panel 5 rows or taller shows its
  filter line.

## Header

| Part | Meaning |
|---|---|
| `◆ decsr` | The brand glyph and the wordmark in the logo gradient |
| repo | The repository directory name |
| branch chip | The current branch, or `(detached)` |
| `↑n ↓n` | Commits ahead of and behind the upstream |
| `MERGING`, `REBASING`, ... | A red chip while the repository is in that state (`m` continues or aborts) |
| `▘ fetch · esc cancels` | A running network command |
| tabs | `H Home`, `G Git`, `W Watch`; the active one is a filled pill (in brackets without colours) |
| agent strip | Right side. One chip per node: status glyph (a spinner while it runs), short label, tokens and `×calls` (`[▖ Explore 3.1k ×4]`); below 110 columns just glyph, label and tokens. |

Runner glyphs and colours are decsr's: claude `✳` orange, codex `◎` green,
opencode `⬡` yellow.

## Panels

### 1 Files

The working tree as a tree (folders `▾`/`▸`) or a flat list (`` ` ``), two
status letters per file (index, worktree): staged letters green, unstaged and
conflicts red, `??` untracked. The main pane shows the unstaged and the staged
diff of the selected file, or the conflict markers of a conflicted file.

`space` stage/unstage, `a` all, `enter` staging view (folder: collapse), `d`
discard menu, `c` commit, `A` amend, `s` stash, `S` stash options, `e` edit.

**Staging view.** The main pane becomes a diff with a cursor `▌`. The tabs
`Unstaged | Staged` show which side you are on (`tab` switches). `a` toggles
hunk mode (the whole hunk under the cursor is the selection) and line mode
(only the line under the cursor); `v` in line mode starts a range, which then
runs to the cursor. The selection is marked with `▏` and the highlighted line
numbers. `space` stages (or unstages on the Staged tab) the selection, `d`
discards it after a confirm, `esc` leaves.

### 2 Branches

Tabs `Local | Remotes | Tags | Worktrees` (`[` and `]`). Local and Remotes show
ahead and behind per branch; the current branch is `*`. The main pane shows the
log of the selected branch, or for a tag the tagged commit, or for a worktree
its path, branch and head.

Local and Remotes: `space` check out, `n` new branch from it, `d` delete
(remote: delete on the remote), `R` rename, `M` merge into current, `r` rebase
onto it, `u` set upstream, `enter` log. Tags: `n` new (lightweight, or
annotated with a message), `d` delete (local or remote too), `P` push, `space`
check out. Worktrees: `n` add, `d` remove (force asks), `space` switch the whole
screen to that worktree, `p` prune.

### 3 Commits

The log (300 commits) with short hash and subject, or `ctrl+g` the graph. `enter`
shows the files of a commit and its diff per file. The main pane shows the
message, the author, the branches that contain it and the diff.

| Key | Action |
|---|---|
| `r` | Reword (a prefilled message box) |
| `s`, `f` | Squash or fixup into the commit below |
| `d` | Drop |
| `ctrl+j`, `ctrl+k` | Move down or up |
| `e` | Edit: rebase and stop at the commit |
| `i` | Interactive rebase todo from this commit |
| `A` | Amend the commit with the staged changes |
| `F` | Fixup commit for this one |
| `g` | Reset to it |
| `t` | Revert |
| `C` | Copy to the cherry-pick clipboard (`+` marks copied commits) |
| `V` | Paste the clipboard onto the current branch |
| `esc` | Clear the clipboard |

**Rebase todo.** Replaces the main pane. Rows are listed newest first; `p`
pick, `r` reword, `s` squash, `f` fixup, `d` drop, `e` edit, `ctrl+j`/`ctrl+k`
reorder, `enter` starts, `esc` cancels. A reword asks for its message first.

### 4 Stash

`space` apply, `g` pop, `d` drop. The main pane shows the stash diff.

### 5 Agent

The list of the sessions of this repository (runner glyph, title, age, `●`
live or `○` idle); `enter` opens one. Opened, the panel is the activity tree
with the tabs `Activity | Changes`.

Tree rows are decsr's activity lines: a glyph per kind (`▸` command, `✎` edit,
`◦` read, `⌕` search, `⚙` tool, `◈` subagent, `❯` you, `›` message), the text,
and a dim meta at the right (`+3 −1` for edits, `✓ 1.2s` for commands). A
running item shows the spinner. `▸`/`▾` before a `◈` row expands the subagent
nested under it (`space`).

The main pane, by state:

| State | Main pane |
|---|---|
| Session list | The selected session: the running line (runner, model, time, tokens) and, for the watched session, its live view; for others the id, branch, model and times |
| Activity, follow on (`f`) | The live view: the running line, the newest steps and message text, the changed files |
| Activity, follow off | The selected activity: a message as Markdown, a command or tool with its detail, an edit or write as a diff, a subagent with its steps |
| Changes (`c`) | The files the session edited, diffed against `HEAD`: a file list and the diff of the selected one (`[`/`]` change file; `enter` focuses the diff, then `j`/`k` move its cursor) |

Moving up (`k`) switches follow off; `G` or `f` switches it on again.

## Main pane scrolling

`enter` on a row that has nothing to expand puts focus in the main pane
(scroll mode): `j`/`k` scroll, `G`/`home` jump, `esc` returns. `ctrl+d`,
`ctrl+u`, `pgdn`, `pgup` scroll it from anywhere, and the mouse wheel over it.

## Popups

| Kind | Look |
|---|---|
| Confirm | Amber box, `y`/`enter` yes, `n`/`esc` no |
| Input | Accent box with one line (branch name, stash message, filter) |
| Commit message | Multi-line box; the first line is the summary; `enter` commits, `ctrl+j` adds a line |
| Menu | Violet box, rows with a key, `j`/`k` and `enter`, or the row's key |
| Error | Red box, any key closes |
| Help | The keys of the focused panel, the global ones and the custom ones, in a framed list |

## Mouse

A click on a panel row focuses the panel and selects the row; the wheel moves
the selection of a panel or scrolls the main pane. The mouse is handled on the Git screen only; Home and Watch are keyboard-only.

## Global keys

| Key | Action |
|---|---|
| `tab`, `l`, `→` / `shift+tab`, `h`, `←` | Next / previous panel |
| `1`-`5` | Jump to a panel |
| `/` | Filter the focused list; the filter shows in the panel and `esc` clears it |
| `f`, `p`, `P` | Fetch, pull, push; a rejected push offers force with lease; a branch without an upstream offers to set it |
| `z`, `Z` | Undo, redo through the reflog (with a confirm; a dirty tree is refused) |
| `m` | Continue or abort the merge or rebase in progress |
| `R` | Refresh git and the agent sessions |
| `@` | Show or hide the command log |
| `x` | The keys of the focused panel and its custom commands as a menu |
| `H` `W` `G` | Go to Home, Watch, Git |
| `?` | Help |
| `q`, `ctrl+c` | Quit |

Custom commands bind extra keys in a context (`files`, `branches`, `commits`,
`stash`, `tags`, `worktrees`, `agent`, `global`); see the README.

## Theme

The look comes from `internal/ui`: `Text`, `Muted`, `Border`, `Accent`, `OK`,
`Warn`, `Fail` and `Sel` tokens, each with a dark and a light value, plus the
runner and group colours. `theme: auto` asks the terminal for its background
once at startup (falling back to dark inside tmux, over a pipe or when the
terminal does not answer); `dark` and `light` (config or `--theme`) force it.
Colour output follows `NO_COLOR`.
