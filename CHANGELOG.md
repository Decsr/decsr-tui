# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.4.0] - 2026-11-26

First public release.

### Added
- Three screens: Home (starfield logo, sessions, repo state, menu), Git and
  Watch, switched with `H`, `G` and `W`.
- Git screen: files with hunk and line staging, branches, remotes, tags and
  worktrees, commits with interactive rebase, reword, squash, fixup, drop,
  move, amend, reset, revert and cherry-pick, stash, fetch, pull and push,
  undo and redo through the reflog, filters, a command log and custom commands.
- Watch screen: the coding-agent session and its subagents as a node graph, node
  cards and a live pane with context meter, tokens, estimated cost, stall
  warning, live text and the changed files with their diffs. Follow mode keeps
  the newest running node selected.
- Read-only watchers for claude (verified on real session files), opencode 1.19.x
  (verified on a real run) and codex (built from the upstream format, not yet
  verified on a real run).
- Config file with `theme`, `refresh`, `agents`, `pricing` and `customCommands`;
  flags `--session`, `--theme`, `--config` and `--version`.
- Light and dark themes (`auto` asks the terminal), honouring `NO_COLOR`.
- One-line installers for Ubuntu and Windows that verify the release checksum,
  and release archives built by GoReleaser.

### Changed
- The program is a standalone terminal UI. It starts on Home, or on Watch when
  `--session` is given.

### Removed
- The decsr orchestrator: no agent is started, driven or messaged, and no
  provider API is called. decsr-tui only reads the session files agents write.

## Before 0.4.0

decsr-tui forked from [decsr](https://github.com/Decsr/Decsr-Code) with the
orchestrator removed. The older history lives in that repository.
