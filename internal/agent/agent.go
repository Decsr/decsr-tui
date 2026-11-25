// Package agent is the runner-neutral model of what a coding-agent CLI session did.
package agent

import "time"

type Kind string

const (
	KindUser     Kind = "user"
	KindMessage  Kind = "message"
	KindThinking Kind = "thinking"
	KindCommand  Kind = "command"
	KindRead     Kind = "read"
	KindEdit     Kind = "edit"
	KindWrite    Kind = "write"
	KindSearch   Kind = "search"
	KindTool     Kind = "tool"
	KindSubagent Kind = "subagent"
	KindError    Kind = "error"
)

type State string

const (
	StateRunning State = "running"
	StateDone    State = "done"
	StateFailed  State = "failed"
)

type Activity struct {
	ID         string // tool_use id, or record uuid+index for text/thinking
	Kind       Kind
	State      State
	Tool       string // raw tool name, e.g. "Bash"
	Title      string // one line
	Detail     string // full input summary + result text (capped)
	File       string // for read/edit/write
	OldText    string // Edit old_string (MultiEdit: concatenated)
	NewText    string // Edit new_string, Write content
	Added      int    // line counts for edit/write
	Removed    int
	SubagentID string // KindSubagent: agentId once linked via meta.json
	At         time.Time
	Duration   time.Duration // tool_use -> tool_result
}

// Usage is the token count of a session or a subagent. Context is the size of
// the latest prompt (input + cache read + cache write of the last assistant
// turn), not a sum over turns.
type Usage struct{ Input, Output, CacheRead, CacheWrite, Context int64 }

type Session struct {
	Runner                              string // "claude"
	ID, Path, Title, Cwd, Branch, Model string
	Started, Updated                    time.Time
	Live                                bool // transcript modified within the last 15s
}

type Subagent struct {
	ID, ToolUseID, Type, Description, Model, Path string
	Finished                                      bool
	Live                                          bool
	Activities                                    []Activity
	Usage                                         Usage
}

type Snapshot struct {
	Session    Session
	Activities []Activity
	Subagents  []Subagent // ordered by first appearance
	Usage      Usage
}

// Source is one CLI's transcript store.
type Source interface {
	Name() string
	Sessions(repoDir string) ([]Session, error) // newest Updated first
	Open(s Session) Tracker
}

// Tracker incrementally follows one session. The TUI calls Poll on a ~500ms tick.
type Tracker interface {
	Poll() (changed bool, err error)
	Snapshot() Snapshot
}
