# Roadmap

What may come after 0.4.0. Nothing here is a promise or has a date. decsr-tui
stays a read-only watcher next to a git client: it will not start or drive an
agent.

## Next

- **Verify codex on a real run.** The codex watcher is built from the upstream
  rollout format and is only tested on fixtures. Check it against a real codex
  session and fix what differs.
- **An Antigravity (`agy`) watcher.** Read its session files the same way as the
  other watchers: read-only, matched to the repository by working directory.
- **Built-in prices.** Today a cost shows only for a model priced under
  `pricing:` in the config. Ship a default price list that the config can
  override.

## Git

- **Submodules.** List them, show their state and update them.
- **Bisect.** Start, mark good or bad, and follow a bisect from the Commits
  panel.
- **A command palette.** Search every action and custom command by name instead
  of remembering its key.

## Watch

- **Mouse support in Watch.** Click a node or card to select it and use the wheel
  to scroll. The mouse works in the Git screen only today.

## Packaging

- **Homebrew, winget and apt.** Install and upgrade without the one-line
  installer. Releases already ship archives for Linux, Windows and macOS.
