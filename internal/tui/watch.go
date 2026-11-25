package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Decsr/decsr-tui/internal/agent"
	"github.com/Decsr/decsr-tui/internal/git"
	"github.com/Decsr/decsr-tui/internal/ui"
)

// The Watch screen is decsr's Workspace for one watched session: the node
// graph, the node feed (one card per node) and the live pane of the selected
// node. The session is the root node, every subagent a child node.

// actOverlay is the full detail of one activity.
type actOverlay struct {
	act    agent.Activity
	node   string
	scroll int
}

type watchState struct {
	curID    string // the session the state belongs to
	nodes    []wnode
	sel      string // selected node id
	follow   bool
	hideLive bool
	expanded map[string]bool
	actSel   map[string]string // node id -> selected activity id; none = the latest
	seen     map[string]int    // node id -> order of first appearance
	nseq     int
	feedTop  int
	fileSel  int
	diffKey  string
	diffs    []git.FileDiff
	viewKey  string
	view     *diffView
	overlay  *actOverlay
	pollErr  string
}

func newWatchState(curID string, follow, hideLive bool) watchState {
	return watchState{curID: curID, follow: follow, hideLive: hideLive, sel: rootID,
		expanded: map[string]bool{}, actSel: map[string]string{}, seen: map[string]int{}}
}

// wnodes are the nodes of the watched session; before the first poll they are
// built from the session alone.
func (m *Model) wnodes() []wnode {
	if len(m.watch.nodes) > 0 {
		return m.watch.nodes
	}
	if m.agent.cur == "" {
		return nil
	}
	return buildNodes(m.agent.snap, m.src.Name())
}

func findNode(nodes []wnode, id string) (wnode, int, bool) {
	for i, n := range nodes {
		if n.id == id {
			return n, i, true
		}
	}
	return wnode{}, -1, false
}

// watchSel is the selected node; the root when the selection is gone.
func (m *Model) watchSel() (wnode, bool) {
	nodes := m.wnodes()
	if n, _, ok := findNode(nodes, m.watch.sel); ok {
		return n, true
	}
	if len(nodes) > 0 {
		return nodes[0], true
	}
	return wnode{}, false
}

// refreshWatch rebuilds the nodes from the snapshot and, in follow mode,
// moves the selection to the newest node and step.
func (m *Model) refreshWatch() {
	a, w := &m.agent, &m.watch
	if w.curID != a.cur {
		*w = newWatchState(a.cur, a.snap.Session.Live, w.hideLive)
	}
	w.nodes = buildNodes(a.snap, m.src.Name())
	for _, n := range w.nodes {
		if _, ok := w.seen[n.id]; !ok {
			w.seen[n.id] = w.nseq
			w.nseq++
		}
	}
	if _, _, ok := findNode(w.nodes, w.sel); !ok {
		w.sel = rootID
	}
	if w.follow {
		w.sel = m.followTarget()
		w.actSel = map[string]string{}
	}
}

// followTarget is the newest node that is running, else the newest node.
func (m *Model) followTarget() string {
	w := &m.watch
	best, bestSeq, running := rootID, -1, false
	for _, n := range w.nodes {
		s := w.seen[n.id]
		switch {
		case n.running() && !running:
			best, bestSeq, running = n.id, s, true
		case n.running() == running && s > bestSeq:
			best, bestSeq = n.id, s
		}
	}
	return best
}

// openWatch opens the session s in the Watch screen.
func (m *Model) openWatch(s agent.Session) tea.Cmd {
	a := &m.agent
	if s.ID != a.cur {
		a.openSession(m.src, s)
	}
	a.pinned = true
	m.screen = scWatch
	m.refreshWatch()
	m.watch.follow = s.Live
	if m.watch.follow {
		m.watch.sel = m.followTarget()
	}
	return m.agentPollCmd()
}

// ---- keys ----

func (m *Model) watchKey(k string) tea.Cmd {
	w := &m.watch
	nodes := m.wnodes()
	_, idx, _ := findNode(nodes, w.sel)
	move := func(i int) {
		if len(nodes) == 0 {
			return
		}
		w.follow = false
		w.sel = nodes[max(0, min(i, len(nodes)-1))].id
	}
	switch k {
	case "up", "k":
		move(idx - 1)
	case "down", "j":
		move(idx + 1)
	case "home":
		move(0)
	case "end":
		move(len(nodes) - 1)
	case "left", "h":
		if n, _, ok := findNode(nodes, w.sel); ok && n.parent != "" {
			w.follow = false
			w.sel = n.parent
		}
	case "right", "l":
		for _, n := range nodes {
			if n.parent == w.sel {
				w.follow = false
				w.sel = n.id
				break
			}
		}
	case "f":
		w.follow = !w.follow
		if w.follow {
			w.sel = m.followTarget()
			w.actSel = map[string]string{}
			m.say(ui.ResultInfo, "follow on: the newest node and step stay selected")
		} else {
			m.say(ui.ResultInfo, "follow off")
		}
	case "v":
		w.hideLive = !w.hideLive
	case " ":
		w.expanded[w.sel] = !w.expanded[w.sel]
	case "[", "]":
		m.stepAct(map[string]int{"[": -1, "]": 1}[k])
	case "n", "p":
		w.fileSel += map[string]int{"n": 1, "p": -1}[k]
	case "pgdown", "ctrl+d", "pgup", "ctrl+u":
		if w.view != nil {
			d := 8
			if k == "pgup" || k == "ctrl+u" {
				d = -8
			}
			w.view.cur = max(min(w.view.cur+d, len(w.view.rows)-1), 0)
		}
	case "enter":
		m.openOverlay()
	case "R":
		return tea.Batch(m.loadCmd(true), m.sessionsCmd())
	case "?":
		m.helpPopup()
	}
	return nil
}

// stepAct moves the selected step of the selected node: -1 older, +1 newer;
// past the newest it goes back to "the latest".
func (m *Model) stepAct(d int) {
	w := &m.watch
	n, ok := m.watchSel()
	if !ok {
		return
	}
	var ids []string
	for _, it := range liveTimeline(n.acts) {
		if it.act != nil {
			ids = append(ids, it.act.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	w.follow = false
	cur := len(ids) - 1
	if id := w.actSel[n.id]; id != "" {
		for i, x := range ids {
			if x == id {
				cur = i
			}
		}
	}
	cur += d
	switch {
	case cur >= len(ids)-1:
		delete(w.actSel, n.id)
	default:
		w.actSel[n.id] = ids[max(cur, 0)]
	}
}

// selAct is the step the selected node shows as selected: the one chosen with
// [ and ], else its latest.
func (m *Model) selAct(n wnode) (agent.Activity, bool) {
	if id := m.watch.actSel[n.id]; id != "" {
		for _, a := range n.acts {
			if a.ID == id {
				return a, true
			}
		}
	}
	for i := len(n.acts) - 1; i >= 0; i-- {
		if n.acts[i].Kind != agent.KindUser {
			return n.acts[i], true
		}
	}
	return agent.Activity{}, false
}

func (m *Model) openOverlay() {
	n, ok := m.watchSel()
	if !ok {
		return
	}
	act, ok := m.selAct(n)
	if !ok {
		m.say(ui.ResultInfo, "no activity yet in this node")
		return
	}
	m.watch.overlay = &actOverlay{act: act, node: n.id}
}

// overlayLines are the lines of the open overlay at w cells.
func (m *Model) overlayLines(w int) []string {
	o := m.watch.overlay
	v := mainView{secs: activitySections(o.act, m.agent.snap)}
	return v.render(max(w, 1), m.spinFrame())
}

func (m *Model) overlayKey(k string) tea.Cmd {
	o := m.watch.overlay
	_, h := ui.FrameSize(m.w, m.h)
	page := max(h-4, 1)
	switch k {
	case "esc", "enter", "q":
		m.watch.overlay = nil
	case "j", "down":
		o.scroll++
	case "k", "up":
		o.scroll--
	case "pgdown", "ctrl+d", " ":
		o.scroll += page
	case "pgup", "ctrl+u":
		o.scroll -= page
	case "home", "g":
		o.scroll = 0
	case "end", "G":
		o.scroll = 1 << 30
	}
	o.scroll = max(o.scroll, 0)
	return nil
}

func (m *Model) overlayBox() string {
	o := m.watch.overlay
	n, _ := m.watchSel()
	head := oneLine(sanitizeText(o.act.Title))
	if o.act.Duration > 0 {
		head += "  (" + actDuration(o.act.Duration) + ")"
	}
	title := truncateOneLine(n.label+" · "+string(o.act.Kind)+" · "+head, 80)
	inner := ui.PanelInner(m.w)
	return ui.Panel(ui.PanelOpts{Title: title, Color: ui.GroupAgents, Body: m.overlayLines(inner), Scroll: o.scroll,
		Keys: []ui.Hint{{Key: "j/k", Label: "scroll"}, {Key: "esc", Label: "close"}}, ScreenW: m.w, ScreenH: m.h})
}

// ---- layout ----

type watchLayout struct {
	graphW, feedW, liveW int  // widths; 0 = not shown
	liveBottom           bool // the live pane sits under the graph and the feed
	topH, liveH          int
}

// watchGeometry splits a w x h body: graph, feed and live pane side by side
// from 120 columns; the live pane under graph and feed below that, the graph
// from 80 columns; a pane too small for what it would show is left out.
func (m *Model) watchGeometry(w, h int) watchLayout {
	l := watchLayout{topH: h}
	live := !m.watch.hideLive
	switch {
	case w >= 120:
		l.graphW = 42 // two canvas columns
		if live {
			l.liveW = 40
		}
		if w >= 150 {
			l.graphW = 44
			if live {
				l.liveW = 56
			}
		}
	case w >= 80:
		l.graphW = 28
	}
	if live && l.liveW == 0 && h >= 18 {
		l.liveBottom = true
		l.liveH = min(max(h*2/5, 9), 14)
		l.topH = h - l.liveH - 1
	}
	l.feedW = w
	if l.graphW > 0 {
		l.feedW -= l.graphW + 1
	}
	if l.liveW > 0 {
		l.feedW -= l.liveW + 1
	}
	return l
}

// ---- drawing ----

// exact cuts or pads lines to h rows of w cells.
func exact(lines []string, w, h int) []string {
	out := make([]string, h)
	for i := range out {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		out[i] = fit(l, w)
	}
	return out
}

// watchBody draws the Watch screen in exactly h rows of w cells.
func (m *Model) watchBody(w, h int) []string {
	a := &m.agent
	if a.cur == "" {
		return m.watchEmpty(w, h, "no agent session to watch",
			"Start "+m.sourceNames()+" in this repo, then press R to look again. H opens Home.")
	}
	nodes := m.wnodes()
	n, _ := m.watchSel()
	if !a.loaded && len(a.snap.Activities) == 0 {
		return m.watchEmpty(w, h, m.spinFrame()+" loading session…", oneLine(sanitizeText(a.snap.Session.Title)))
	}
	l := m.watchGeometry(w, h)
	var top []string
	topH := l.topH
	{
		var parts [][]string
		var widths []int
		if l.graphW > 0 {
			parts = append(parts, m.watchGraph(nodes, l.graphW, topH))
			widths = append(widths, l.graphW)
		}
		parts = append(parts, m.watchFeed(nodes, l.feedW, topH))
		widths = append(widths, l.feedW)
		if l.liveW > 0 {
			parts = append(parts, m.watchLive(n, l.liveW, topH))
			widths = append(widths, l.liveW)
		}
		div := strings.Split(ui.VDivider(topH), "\n")
		for i := 0; i < topH; i++ {
			var sb strings.Builder
			for pi, p := range parts {
				if pi > 0 {
					sb.WriteString(pick(div, i))
				}
				sb.WriteString(fit(pick(p, i), widths[pi]))
			}
			top = append(top, sb.String())
		}
	}
	if !l.liveBottom {
		return exact(top, w, h)
	}
	rule := lipgloss.NewStyle().Foreground(ui.Border).Render(strings.Repeat("─", w))
	out := append(top, rule)
	return exact(append(out, m.watchLive(n, w, l.liveH)...), w, h)
}

func (m *Model) sourceNames() string {
	if n := m.src.Name(); n != "all" {
		return n
	}
	return "claude, codex or opencode"
}

// watchEmpty is the centred message of a Watch screen with nothing to show.
func (m *Model) watchEmpty(w, h int, title, hint string) []string {
	var lines []string
	if w >= ui.LogoWidth+2 && h >= 8 {
		for _, l := range ui.Logo(false) {
			lines = append(lines, centerLine(l, w))
		}
		lines = append(lines, "")
	}
	lines = append(lines, centerLine(sBold.Render(truncateOneLine(title, w)), w))
	for _, l := range hardWrap(hint, max(w-4, 8)) {
		lines = append(lines, centerLine(sDim.Render(l), w))
	}
	if e := m.watch.pollErr; e != "" {
		lines = append(lines, "", centerLine(ui.ResultLine(ui.ResultError, truncateOneLine(e, w-4)), w))
	}
	return exact(append(make([]string, max((h-len(lines))/2, 0)), lines...), w, h)
}

// watchGraph is the node graph pane: a label row and the canvas.
func (m *Model) watchGraph(nodes []wnode, w, h int) []string {
	label := ui.PaneLabel("Graph", false, w)
	body := strings.Split(renderCanvas(nodes, canvasOpts{width: max(w-2, 1), height: max(h-1, 1), selected: m.watch.sel,
		icons: iconsUnicode, spinner: m.spinFrame()}), "\n")
	out := []string{label}
	for _, l := range body {
		out = append(out, " "+l)
	}
	return exact(out, w, h)
}

// watchFeed is the node feed: a label row and one card per node, the window
// kept on the selected card.
func (m *Model) watchFeed(nodes []wnode, w, h int) []string {
	wst := &m.watch
	rows := max(h-1, 1)
	now := m.now()
	var lines []string
	spanOf := map[string][2]int{}
	cw := max(w-1, 12)
	for _, n := range nodes {
		sel := n.id == wst.sel
		card := renderNodeCard(cardOpts{n: n, width: cw, selected: sel, expanded: wst.expanded[n.id], now: now,
			spin: m.spinFrame(), icons: iconsUnicode})
		start := len(lines)
		for _, l := range card {
			mark := " "
			if sel {
				mark = lipgloss.NewStyle().Foreground(ui.Accent).Render("▌")
			}
			lines = append(lines, mark+l)
		}
		spanOf[n.id] = [2]int{start, len(lines)}
	}
	top := wst.feedTop
	if sp, ok := spanOf[wst.sel]; ok {
		if sp[0] < top {
			top = sp[0]
		}
		if sp[1] > top+rows {
			top = min(sp[0], sp[1]-rows)
		}
	}
	top = max(min(top, len(lines)-rows), 0)
	wst.feedTop = top
	idx := 0
	for i, n := range nodes {
		if n.id == wst.sel {
			idx = i + 1
		}
	}
	title := fmt.Sprintf("Nodes · %d/%d", idx, len(nodes))
	if wst.follow {
		title += " · follow"
	}
	if top > 0 {
		title += " · ▴"
	}
	if top+rows < len(lines) {
		title += " · ▾"
	}
	out := []string{ui.PaneLabel(title, true, w)}
	end := min(top+rows, len(lines))
	return exact(append(out, lines[top:end]...), w, h)
}

// ---- live pane ----

// ctxRow is "ctx 18k / 200k ███░░░ 9%  ·  ~$0.42 est.": the context meter
// (only when the window of the model is known) and the estimated cost (only
// when a price is known). "" when there is neither.
func (m *Model) ctxRow(n wnode, w int) string {
	var parts []string
	if ctx := n.usage.Context; ctx > 0 {
		lim := ctxLimit(n.model, ctx)
		s := "ctx " + tokCompact(int(ctx))
		if lim > 0 {
			s += " / " + tokCompact(int(lim))
			mw := min(max(w-ansi.StringWidth(s)-24, 6), 14)
			parts = append(parts, sDim.Render(s)+" "+ui.Meter(float64(ctx)/float64(lim), mw))
		} else {
			parts = append(parts, sDim.Render(s))
		}
	}
	if usd, ok := nodeCost(m.cfg, n); ok && usd > 0 {
		parts = append(parts, sDim.Render("~"+usdLabel(usd)+" est."))
	}
	return strings.Join(parts, sDim.Render("  ·  "))
}

// runLine is the running line of a node cut to w cells.
func (m *Model) runLine(n wnode, w int) string {
	st := agent.StateRunning
	switch n.state {
	case "done", "idle":
		st = agent.StateDone
	case "failed":
		st = agent.StateFailed
	}
	r := liveRun{State: st, Runner: n.runner, Model: n.model, Started: n.started, Usage: n.usage, Estimated: n.running()}
	if !n.running() {
		r.Ended = n.last
	}
	return ansi.Truncate(runningLine(r, m.spinFrame(), m.now()), max(w, 1), "…")
}

// watchLive is the live pane of a node, exactly h rows of w cells: a title
// row, the running line, the context meter and cost, the steps and live text,
// and the changed files with the diff of the selected one.
func (m *Model) watchLive(n wnode, w, h int) []string {
	line := lipgloss.NewStyle().Foreground(ui.Border)
	title := " Live · " + truncateOneLine(n.label, 16) + " "
	hint := " v hide "
	fill := max(w-2-ansi.StringWidth(title)-ansi.StringWidth(hint)-1, 0)
	out := []string{ansi.Truncate("  "+sDim.Render(strings.TrimSpace(title))+" "+line.Render(strings.Repeat("─", fill))+sDim.Render(hint), w, "")}
	out = append(out, "  "+m.runLine(n, w-4))
	if r := m.ctxRow(n, w-4); r != "" {
		out = append(out, "  "+r)
	}
	bodyH := h - len(out)
	if bodyH < 1 {
		return exact(out, w, h)
	}
	m.syncDiffs(n)
	files := m.watch.diffs
	selID := m.watch.actSel[n.id]
	steps := func(sw, rows int) []string {
		return m.watchSteps(n, sw-2, rows, selID)
	}
	indent := func(ls []string) []string {
		for i := range ls {
			ls[i] = "  " + ls[i]
		}
		return ls
	}
	switch {
	case len(files) == 0:
		out = append(out, indent(steps(w, bodyH))...)
	case w >= 90 && bodyH >= 6: // side by side
		rw := min(48, w/2)
		lw := w - rw - 1
		left := indent(steps(lw, bodyH))
		right := m.changesPane(files, rw-1, bodyH, n)
		div := strings.Split(ui.VDivider(bodyH), "\n")
		for i := 0; i < bodyH; i++ {
			out = append(out, fit(pick(left, i), lw)+pick(div, i)+" "+pick(right, i))
		}
	case bodyH >= 12: // stacked
		ch := max(bodyH/2, 6)
		sh := bodyH - ch
		out = append(out, indent(steps(w, sh))...)
		out = append(out, m.changesPane(files, w, ch, n)...)
	default:
		rows := indent(steps(w, bodyH))
		rows[bodyH-1] = "  " + sDim.Render(plural(len(files), "file", "files")+" changed")
		out = append(out, rows...)
	}
	return exact(out, w, h)
}

// syncDiffs keeps the per-file diffs of the node and the diff view of the
// selected file up to date; the view keeps its cursor while the file stays.
func (m *Model) syncDiffs(n wnode) {
	w := &m.watch
	last := ""
	if len(n.acts) > 0 {
		last = n.acts[len(n.acts)-1].ID
	}
	key := fmt.Sprintf("%s|%d|%s|%s", n.id, len(n.acts), last, n.state)
	if key != w.diffKey {
		w.diffKey, w.diffs = key, nodeDiffs(n)
	}
	if len(w.diffs) == 0 {
		w.view, w.viewKey, w.fileSel = nil, "", 0
		return
	}
	w.fileSel = max(min(w.fileSel, len(w.diffs)-1), 0)
	vk := fmt.Sprintf("%s|%d", key, w.fileSel)
	if vk != w.viewKey {
		w.view = mergeOutDiff(w.view, w.diffs[w.fileSel], nil)
		w.viewKey = vk
	}
}

// changesPane is the changed-files box of a node: its files and the diff of
// the selected one.
func (m *Model) changesPane(files []git.FileDiff, w, h int, n wnode) []string {
	return exact(outChangesPane(files, m.watch.fileSel, m.watch.view, n.running(), w, h, false), w, h)
}

// watchSteps is the timeline of a node, rows lines of w cells: text and steps
// in order, the newest at the bottom. A thinking step shows "thinking…", with
// the pixel spinner while it is the last thing a running node does. The step
// chosen with [ and ] is marked and kept in view.
func (m *Model) watchSteps(n wnode, w, rows int, selID string) []string {
	now, spin := m.now(), m.spinFrame()
	dm := lipgloss.NewStyle().Foreground(ui.Muted)
	txt := lipgloss.NewStyle().Foreground(ui.Text)
	items := liveTimeline(n.acts)
	if len(items) == 0 {
		return exact([]string{dm.Render("waiting for the first output…")}, w, rows)
	}
	var lines []string
	selLine := -1
	accent := lipgloss.NewStyle().Foreground(ui.Accent)
	for i, it := range items {
		start := len(lines)
		last := i == len(items)-1
		switch {
		case it.act == nil:
			for _, l := range strings.Split(ansi.Wrap(it.text, max(w-2, 1), ""), "\n") {
				lines = append(lines, "  "+txt.Render(l))
			}
		case it.act.Kind == agent.KindThinking:
			g := actGlyph(it.act.Kind)
			if last && n.running() {
				g = accent.Render(spin)
				lines = append(lines, "  "+g+dm.Render(" thinking…"))
			} else {
				lines = append(lines, "  "+dm.Render(g+" thinking…"))
			}
		default:
			l := activityLine(*it.act, now, max(w-2, 3))
			if it.act.State == agent.StateRunning {
				l = strings.Replace(l, actGlyph(it.act.Kind), accent.Render(spin), 1)
			}
			lines = append(lines, "  "+l)
		}
		if it.act != nil && selID != "" && it.act.ID == selID {
			selLine = start
			lines[start] = accent.Render("▌") + lines[start][1:]
		}
	}
	top := max(len(lines)-rows, 0)
	if selLine >= 0 {
		top = max(min(selLine-rows/2, len(lines)-rows), 0)
	}
	end := min(top+rows, len(lines))
	return exact(lines[top:end], w, rows)
}

// ---- status line ----

// watchStatusLine is the dim bottom line of the Watch screen: the tokens of
// the session per runner and the estimated cost, as decsr's Workspace status
// bar shows them.
func (m *Model) watchStatusLine() string {
	nodes := m.wnodes()
	if len(nodes) == 0 {
		return ansi.Truncate("  "+sDim.Render("no session"), m.w, "")
	}
	var tok int
	for _, n := range nodes {
		tok += promptTokens(n.usage) + int(n.usage.Output)
	}
	root := nodes[0]
	runner := lipgloss.NewStyle().Foreground(runnerColor(root.runner)).Render(runnerGlyph(root.runner, iconsUnicode) + " " + root.runner)
	run := 0
	for _, n := range nodes {
		if n.running() {
			run++
		}
	}
	segs := []string{runner + sDim.Render(" "+tokCompact(tok)+" tok")}
	segs = append(segs, sDim.Render(plural(len(nodes), "node", "nodes")))
	if run > 0 {
		segs = append(segs, lipgloss.NewStyle().Foreground(ui.Accent).Render(m.spinFrame()+" "+plural(run, "running", "running")))
	}
	usd, priced, unknown := sessionCost(m.cfg, nodes)
	switch {
	case priced && unknown:
		segs = append(segs, sDim.Render("~≥ "+usdLabel(usd)+" est."))
	case priced:
		segs = append(segs, sDim.Render("~"+usdLabel(usd)+" est."))
	case tok > 0:
		segs = append(segs, sDim.Render("tokens only"))
	}
	return ansi.Truncate("  "+strings.Join(segs, sDim.Render("  ·  ")), m.w, "…")
}
