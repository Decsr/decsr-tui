package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/Decsr/decsr-tui/internal/agent"
	"github.com/Decsr/decsr-tui/internal/config"
	"github.com/Decsr/decsr-tui/internal/git"
	"github.com/Decsr/decsr-tui/internal/linediff"
	"github.com/Decsr/decsr-tui/internal/pricing"
)

// rootID is the id of the root node: the watched session itself.
const rootID = "root"

// stallAfter is how long a running node may stay silent before the card says so.
const stallAfter = time.Minute

// wnode is one node of the watched session: the session itself (the root) or
// one of its subagents. Every node has its own activities, usage and status.
type wnode struct {
	id, parent string
	depth      int
	title      string // short, one line
	label      string // short chip label: the runner of the root, the type of a subagent
	runner     string
	model      string
	acts       []agent.Activity
	usage      agent.Usage
	state      string // a ui.Status kind: running, done, failed, or "idle" for one that waits
	started    time.Time
	last       time.Time // the newest sign of life
}

// buildNodes turns a snapshot into nodes in tree order: the root, then every
// subagent right after the activity that started it, nested ones deeper.
// Subagents no activity links to yet hang under the root.
func buildNodes(snap agent.Snapshot, defRunner string) []wnode {
	s := snap.Session
	runner := s.Runner
	if runner == "" {
		runner = defRunner
	}
	title := oneLine(sanitizeText(s.Title))
	if title == "" {
		title = s.ID
	}
	root := wnode{id: rootID, depth: 0, title: title, label: runner, runner: runner, model: s.Model,
		acts: snap.Activities, usage: snap.Usage, started: s.Started}
	root.last = lastActivity(root.acts)
	if s.Updated.After(root.last) {
		root.last = s.Updated
	}
	switch {
	case s.Live || anyRunning(root.acts):
		root.state = "running"
	case failedLast(root.acts):
		root.state = "failed"
	default:
		root.state = "done"
	}
	out := []wnode{root}

	subs := map[string]agent.Subagent{}
	for _, sb := range snap.Subagents {
		subs[sb.ID] = sb
	}
	done := map[string]bool{}
	var walk func(parent string, depth int, acts []agent.Activity)
	walk = func(parent string, depth int, acts []agent.Activity) {
		for i := range acts {
			a := acts[i]
			if a.Kind != agent.KindSubagent || a.SubagentID == "" || done[a.SubagentID] {
				continue
			}
			sb, ok := subs[a.SubagentID]
			if !ok {
				continue
			}
			done[sb.ID] = true
			out = append(out, subNode(sb, &a, parent, depth, root))
			walk(sb.ID, depth+1, sb.Activities)
		}
	}
	walk(rootID, 1, root.acts)
	for _, sb := range snap.Subagents {
		if !done[sb.ID] {
			done[sb.ID] = true
			out = append(out, subNode(sb, nil, rootID, 1, root))
			walk(sb.ID, 2, sb.Activities)
		}
	}
	return out
}

func subNode(sb agent.Subagent, link *agent.Activity, parent string, depth int, root wnode) wnode {
	label := sb.Type
	if label == "" {
		label = "agent"
	}
	title := oneLine(sanitizeText(sb.Description))
	if title == "" && link != nil {
		title = oneLine(sanitizeText(link.Title))
	}
	if title == "" {
		title = label
	}
	n := wnode{id: sb.ID, parent: parent, depth: depth, title: title, label: label, runner: root.runner,
		model: sb.Model, acts: sb.Activities, usage: sb.Usage}
	if n.model == "" {
		n.model = root.model
	}
	switch {
	case sb.Finished:
		n.state = "done"
		if link != nil && link.State == agent.StateFailed {
			n.state = "failed"
		}
	case sb.Live || anyRunning(sb.Activities) || (link != nil && link.State == agent.StateRunning):
		n.state = "running"
	default:
		n.state = "idle"
	}
	switch {
	case len(sb.Activities) > 0 && !sb.Activities[0].At.IsZero():
		n.started = sb.Activities[0].At
	case link != nil:
		n.started = link.At
	}
	n.last = lastActivity(n.acts)
	if n.last.IsZero() && link != nil {
		n.last = link.At
	}
	return n
}

func anyRunning(acts []agent.Activity) bool {
	for _, a := range acts {
		if a.State == agent.StateRunning {
			return true
		}
	}
	return false
}

func failedLast(acts []agent.Activity) bool {
	return len(acts) > 0 && acts[len(acts)-1].Kind == agent.KindError
}

// lastActivity is the newest time any activity of acts was seen.
func lastActivity(acts []agent.Activity) time.Time {
	var t time.Time
	for _, a := range acts {
		if a.At.After(t) {
			t = a.At
		}
		if e := a.At.Add(a.Duration); a.Duration > 0 && e.After(t) {
			t = e
		}
	}
	return t
}

func (n wnode) running() bool { return n.state == "running" }

// stalledFor is how long a running node has been silent once that reaches
// stallAfter; ok is false otherwise.
func (n wnode) stalledFor(now time.Time) (time.Duration, bool) {
	if !n.running() {
		return 0, false
	}
	base := n.last
	if base.IsZero() {
		base = n.started
	}
	if base.IsZero() {
		return 0, false
	}
	d := now.Sub(base)
	return d, d >= stallAfter
}

// stallText is "no activity for 1m" (minutes rounded down); "" when the node
// is not running or has been silent for less than a minute.
func stallText(n wnode, now time.Time) string {
	d, ok := n.stalledFor(now)
	if !ok {
		return ""
	}
	return "no activity for " + feedDuration(d.Truncate(time.Minute))
}

// currentAction is the activity a node is busy with: the latest running one,
// else the latest step. ok is false for a node without activity.
func currentAction(n wnode) (agent.Activity, bool) {
	for i := len(n.acts) - 1; i >= 0; i-- {
		if n.acts[i].State == agent.StateRunning {
			return n.acts[i], true
		}
	}
	for i := len(n.acts) - 1; i >= 0; i-- {
		if n.acts[i].Kind != agent.KindUser {
			return n.acts[i], true
		}
	}
	return agent.Activity{}, false
}

// activityCounts is the number of steps (tool calls and subagents) and how
// many of them failed.
func activityCounts(n wnode) (total, failed int) {
	for _, a := range n.acts {
		switch a.Kind {
		case agent.KindUser, agent.KindMessage, agent.KindThinking:
			continue
		}
		total++
		if a.State == agent.StateFailed {
			failed++
		}
	}
	return total, failed
}

// promptTokens is everything the node sent to the model: fresh input and the
// cached part.
func promptTokens(u agent.Usage) int { return int(u.Input + u.CacheRead + u.CacheWrite) }

// tokTotalLabel is "184k tok", the tokens in and out; "" before any are known.
// A running node's count is an estimate ("~"): its last turn is still open.
func tokTotalLabel(n wnode) string {
	total := promptTokens(n.usage) + int(n.usage.Output)
	if total <= 0 {
		return ""
	}
	est := ""
	if n.running() {
		est = "~"
	}
	return est + tokCompact(total) + " tok"
}

// ctxLimit is the context window of the model of a node whose latest prompt
// had used tokens. A claude model that has already gone past 200k runs with
// the 1M window. 0 when the model is unknown.
func ctxLimit(model string, used int64) int64 {
	limit := agent.ContextMax(model)
	if limit > 0 && used > limit && limit == 200_000 {
		return 1_000_000
	}
	return limit
}

// ctxLabel is "ctx 18k" or "ctx 18k / 200k"; "" when the size is unknown.
func ctxLabel(n wnode) string {
	if n.usage.Context <= 0 {
		return ""
	}
	s := "ctx " + tokCompact(int(n.usage.Context))
	if lim := ctxLimit(n.model, n.usage.Context); lim > 0 {
		s += " / " + tokCompact(int(lim))
	}
	return s
}

// cardMeta is the dim text after the duration: " · ctx 18k · 184k tok".
func cardMeta(n wnode) string {
	meta := ""
	if c := ctxLabel(n); c != "" {
		meta += " · " + c
	}
	if t := tokTotalLabel(n); t != "" {
		meta += " · " + t
	}
	return meta
}

// nodeCost is the estimated USD cost of a node from the configured prices;
// known is false when the model has none.
func nodeCost(cfg config.Config, n wnode) (usd float64, known bool) {
	u := n.usage
	if u.Input+u.Output+u.CacheRead+u.CacheWrite == 0 {
		return 0, false
	}
	return pricing.Cost(cfg, n.runner, n.model, u.Input+u.CacheWrite, u.CacheRead, u.Output)
}

// sessionCost sums the known cost of every node; unknown says some node's
// tokens have no price.
func sessionCost(cfg config.Config, nodes []wnode) (usd float64, priced, unknown bool) {
	for _, n := range nodes {
		if n.usage.Input+n.usage.Output+n.usage.CacheRead+n.usage.CacheWrite == 0 {
			continue
		}
		c, ok := nodeCost(cfg, n)
		if !ok {
			unknown = true
			continue
		}
		priced = true
		usd += c
	}
	return usd, priced, unknown
}

func usdLabel(v float64) string {
	if v > 0 && v < 0.01 {
		return "<$0.01"
	}
	return fmt.Sprintf("$%.2f", v)
}

// nodeDuration is how long the node ran so far, or ran in all; -1 when unknown.
func nodeDuration(n wnode, now time.Time) time.Duration {
	if n.started.IsZero() {
		return -1
	}
	end := n.last
	if n.running() || end.IsZero() {
		end = now
	}
	if end.Before(n.started) {
		return -1
	}
	return end.Sub(n.started)
}

// nodeDiffs is the edits of a node as one diff per file, the newest file
// first, drawn from what the agent sent (no git call).
func nodeDiffs(n wnode) []git.FileDiff {
	var out []git.FileDiff
	for _, f := range liveFiles(n.acts) {
		var body strings.Builder
		for _, a := range n.acts {
			if (a.Kind != agent.KindEdit && a.Kind != agent.KindWrite) || a.State == agent.StateFailed {
				continue
			}
			p := a.File
			if p == "" {
				p = a.Title
			}
			if oneLine(sanitizeText(p)) != f.path {
				continue
			}
			body.WriteString(linediff.Unified(a.OldText, a.NewText, 3))
		}
		if body.Len() == 0 {
			continue
		}
		slash := strings.ReplaceAll(f.path, "\\", "/")
		text := "diff --git a/" + slash + " b/" + slash + "\n" + body.String()
		if fds := git.ParseDiff(text); len(fds) > 0 {
			fd := fds[0]
			fd.Path = slash
			out = append(out, fd)
		}
	}
	return out
}
