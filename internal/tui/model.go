// Package tui is the terminal UI: one root model that drives the panels.
package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Decsr/decsr-tui/internal/agent"
	"github.com/Decsr/decsr-tui/internal/config"
	"github.com/Decsr/decsr-tui/internal/git"
	"github.com/Decsr/decsr-tui/internal/ui"
)

type panelID int

const (
	pFiles panelID = iota
	pBranches
	pCommits
	pStash
	pAgent
	numPanels
)

var panelNames = [numPanels]string{"Files", "Branches", "Commits", "Stash", "Agent"}

// Options are the command line choices.
type Options struct {
	Session string        // open the session whose id starts with this
	Config  config.Config // zero value means the defaults
}

// logBuf is the command log; git commands append from other goroutines.
type logBuf struct {
	mu    sync.Mutex
	lines []string
}

func (l *logBuf) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, s)
	if len(l.lines) > 200 {
		l.lines = l.lines[len(l.lines)-200:]
	}
}

func (l *logBuf) last(n int) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.lines) > n {
		return append([]string(nil), l.lines[len(l.lines)-n:]...)
	}
	return append([]string(nil), l.lines...)
}

type netOp struct {
	name   string
	cancel context.CancelFunc
}

// Model is the root model.
type Model struct {
	repo *git.Repo
	src  agent.Source
	opts Options
	cfg  config.Config

	w, h      int
	focus     panelID
	mainFocus bool // the main pane has focus (scroll mode)

	head     git.Head
	state    git.RepoState
	headHash string
	gen      int // bumps whenever git data changed

	files    filesPanel
	branches branchesPanel
	commits  commitsPanel
	stashes  stashPanel
	agent    agentPanel
	main     mainView

	clip      []git.Commit // the cherry-pick clipboard
	todo      *todoEditor  // the interactive rebase todo editor, when open
	tags      []git.Tag
	worktrees []git.Worktree
	spinning  bool // the 10fps spinner tick is running

	popup   *popup
	log     *logBuf
	showLog bool
	net     *netOp
	spin    spinner.Model
	frame   int
	polls   int
	quit    bool

	screen   screen
	home     homeState
	watch    watchState
	note     notice
	clock    func() time.Time // nil means time.Now; tests set it
	counts   map[string]nodeCount
	counting bool
}

// New opens the repository at dir.
func New(dir string, src agent.Source, opts Options) (*Model, error) {
	repo, err := git.Open(dir)
	if err != nil {
		return nil, err
	}
	if opts.Config.Refresh.GitSeconds <= 0 {
		opts.Config = config.Default()
	}
	m := &Model{repo: repo, src: src, opts: opts, cfg: opts.Config, log: &logBuf{}, showLog: true, focus: pFiles}
	repo.LogFunc = m.log.add
	m.spin = spinner.New(spinner.WithSpinner(pixelSpinner))
	m.files = filesPanel{tree: true, collapsed: map[string]bool{}}
	m.agent = agentPanel{expanded: map[string]bool{}, follow: true}
	m.watch = newWatchState("", false, false)
	if opts.Session != "" {
		m.screen = scWatch // --session opens Watch directly
	}
	return m, nil
}

// ---- messages ----

type (
	statusTickMsg struct{}
	agentTickMsg  struct{}
	spinTickMsg   struct{}
	dataMsg       struct {
		full      bool
		head      git.Head
		files     []git.File
		state     git.RepoState
		headHash  string
		branches  []git.Branch
		commits   []git.Commit
		stashes   []git.Stash
		tags      []git.Tag
		worktrees []git.Worktree
		err       error
	}
	sessionsMsg struct{ sessions []agent.Session }
	agentMsg    struct {
		id      string
		changed bool
		snap    agent.Snapshot
		err     error
	}
	mainMsg struct {
		seq        int
		ident, ver string
		secs       []section
	}
	actionMsg  struct{ err error }
	netDoneMsg struct {
		name      string
		err       error
		cancelled bool
	}
	editorDoneMsg struct{ err error }
	stgMsg        struct {
		path       string
		staged     bool
		text       string
		fromAction bool
		err        error // reading the diff
		actErr     error // applying the patch
	}
	commitFilesMsg struct {
		hash  string
		files []git.ChangedFile
		err   error
	}
)

func (m *Model) tickStatus() tea.Cmd {
	d := time.Duration(m.cfg.Refresh.GitSeconds) * time.Second
	return tea.Tick(d, func(time.Time) tea.Msg { return statusTickMsg{} })
}

func (m *Model) tickAgent() tea.Cmd {
	d := time.Duration(m.cfg.Refresh.AgentMillis) * time.Millisecond
	return tea.Tick(d, func(time.Time) tea.Msg { return agentTickMsg{} })
}

// tickSpin drives the spinner of running agent items at about 10fps.
func tickSpin() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return spinTickMsg{} })
}

// Init starts loading and the poll ticks.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.startup(), m.tickStatus(), m.tickAgent())
}

func (m *Model) startup() tea.Cmd { return tea.Batch(m.loadCmd(true), m.sessionsCmd()) }

// loadCmd reads git state; full also reads branches, commits and stashes.
func (m *Model) loadCmd(full bool) tea.Cmd {
	repo := m.repo
	return func() tea.Msg {
		var d dataMsg
		d.full = full
		d.head, d.files, d.err = repo.Status()
		if d.err != nil {
			return d
		}
		d.state = repo.State()
		if cs, _ := repo.Log("", 1); len(cs) > 0 {
			d.headHash = cs[0].Hash
		}
		if full {
			d.branches, _ = repo.Branches()
			d.commits, _ = repo.Log("", 300)
			d.stashes, _ = repo.Stashes()
			d.tags, _ = repo.Tags()
			d.worktrees, _ = repo.Worktrees()
		}
		return d
	}
}

func (m *Model) sessionsCmd() tea.Cmd {
	src, dir := m.src, m.repo.Dir
	return func() tea.Msg {
		ss, _ := src.Sessions(dir)
		return sessionsMsg{ss}
	}
}

// do runs a git action off the UI thread, then reloads everything.
func (m *Model) do(f func() error) tea.Cmd {
	return func() tea.Msg { return actionMsg{f()} }
}

func (m *Model) startNet(name string, f func(ctx context.Context) error) tea.Cmd {
	if m.net != nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.net = &netOp{name, cancel}
	m.log.add(name + " ...")
	run := func() tea.Msg {
		err := f(ctx)
		return netDoneMsg{name: name, err: err, cancelled: ctx.Err() != nil}
	}
	return tea.Batch(run, m.spin.Tick)
}

// ---- update ----

// Update handles one message, then keeps the main pane in step.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.update(msg)
	m.syncLists()
	if m.quit {
		return m, tea.Quit
	}
	if m.screen != scGit {
		return m, cmd
	}
	return m, tea.Batch(cmd, m.syncMain())
}

func (m *Model) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.KeyMsg:
		return m.key(msg)
	case tea.MouseMsg:
		return m.mouse(msg)
	case spinner.TickMsg:
		if m.net != nil {
			var c tea.Cmd
			m.spin, c = m.spin.Update(msg)
			return c
		}
	case statusTickMsg:
		return tea.Batch(m.loadCmd(false), m.tickStatus(), m.sessionsEvery())
	case agentTickMsg:
		return tea.Batch(m.agentPollCmd(), m.tickAgent())
	case spinTickMsg:
		m.frame++
		if m.agentRunning() {
			return tickSpin()
		}
		m.spinning = false
	case dataMsg:
		return m.applyData(msg)
	case sessionsMsg:
		m.applySessions(msg.sessions)
		return m.countsCmd(msg.sessions)
	case countsMsg:
		m.applyCounts(msg.counts)
	case agentMsg:
		return m.applyAgent(msg)
	case customDoneMsg:
		return m.customDone(msg)
	case forceWorktreeMsg:
		m.confirm("Remove worktree", "Worktree "+msg.path+" has changes or is locked.\nRemove it anyway?", func() tea.Cmd {
			return m.do(func() error { return m.repo.RemoveWorktree(msg.path, true) })
		})
	case mainMsg:
		if msg.seq == m.main.seq {
			m.main.apply(msg.ident, msg.ver, msg.secs)
			if msg.ident == "changes" {
				m.syncChanges()
			}
		}
	case actionMsg:
		if msg.err != nil {
			m.errorPopup(msg.err)
		}
		return m.loadCmd(true)
	case netDoneMsg:
		return m.netDone(msg)
	case editorDoneMsg:
		if msg.err != nil {
			m.errorPopup(msg.err)
		}
		return m.loadCmd(true)
	case stgMsg:
		return m.applyStg(msg)
	case forceDeleteMsg:
		m.confirm("Delete branch", "Branch "+msg.name+" is not fully merged.\nDelete it anyway?", func() tea.Cmd {
			return m.do(func() error { return m.repo.DeleteBranch(msg.name, true) })
		})
	case commitFilesMsg:
		if msg.err == nil && m.commits.sub != nil && m.commits.sub.hash == msg.hash {
			m.commits.sub.files = msg.files
		}
	}
	return nil
}

func (m *Model) sessionsEvery() tea.Cmd {
	m.polls++
	if m.polls%2 == 0 { // every second status poll, so about every 4s
		return m.sessionsCmd()
	}
	return nil
}

func (m *Model) applyData(d dataMsg) tea.Cmd {
	if d.err != nil {
		m.log.add("status: " + d.err.Error())
		return nil
	}
	var cmd tea.Cmd
	changed := !reflect.DeepEqual(d.files, m.files.files) || d.head != m.head || d.state != m.state
	if !d.full && d.headHash != m.headHash {
		cmd = m.loadCmd(true) // HEAD moved elsewhere: refresh the rest
	}
	m.head, m.state, m.files.files = d.head, d.state, d.files
	if d.full {
		m.headHash = d.headHash
		m.branches.all, m.commits.commits, m.stashes.stashes = d.branches, d.commits, d.stashes
		m.tags, m.worktrees = d.tags, d.worktrees
		m.gen++
	} else if changed {
		m.gen++
	}
	return cmd
}

func (m *Model) netDone(msg netDoneMsg) tea.Cmd {
	m.net = nil
	switch {
	case msg.cancelled:
		m.log.add(msg.name + " cancelled")
		m.say(ui.ResultInfo, msg.name+" cancelled")
	case msg.err != nil:
		if msg.name == "push" && isRejected(msg.err) {
			m.confirm("Push rejected", "The remote has commits you do not have.\nForce push with lease?", func() tea.Cmd {
				return m.startNet("push", func(ctx context.Context) error { return m.repo.Push(ctx, true, false) })
			})
		} else {
			m.errorPopup(msg.err)
			m.say(ui.ResultError, msg.name+" failed: "+firstLine(msg.err.Error()))
		}
	}
	if msg.err == nil && !msg.cancelled {
		m.say(ui.ResultOK, msg.name+" done")
	}
	return m.loadCmd(true)
}

func isRejected(err error) bool {
	s := err.Error()
	return strings.Contains(s, "rejected") || strings.Contains(s, "non-fast-forward") || strings.Contains(s, "fetch first")
}

// ---- agent ----

func (m *Model) applySessions(ss []agent.Session) {
	a := &m.agent
	a.sessions = ss
	if m.opts.Session != "" {
		for _, s := range ss {
			if strings.HasPrefix(s.ID, m.opts.Session) {
				m.opts.Session = ""
				m.focus = pAgent
				a.openSession(m.src, s)
				a.open, a.pinned, a.follow = true, true, s.Live
				return
			}
		}
	}
	if len(ss) == 0 {
		return
	}
	if a.cur == "" || (!a.open && !a.pinned && ss[0].ID != a.cur) {
		a.openSession(m.src, ss[0]) // watch the newest session until one is opened
	}
}

func (m *Model) agentPollCmd() tea.Cmd {
	a := &m.agent
	if a.tracker == nil || a.polling {
		return nil
	}
	a.polling = true
	tr, id := a.tracker, a.cur
	return func() tea.Msg {
		changed, err := tr.Poll()
		return agentMsg{id: id, changed: changed, snap: tr.Snapshot(), err: err}
	}
}

func (m *Model) applyAgent(msg agentMsg) tea.Cmd {
	a := &m.agent
	if msg.id != a.cur {
		return nil
	}
	a.polling = false
	if msg.err != nil {
		if m.watch.pollErr == "" {
			m.say(ui.ResultError, "cannot read the session: "+oneLine(msg.err.Error()))
		}
		m.watch.pollErr = oneLine(msg.err.Error())
	} else {
		m.watch.pollErr = ""
	}
	was := m.agentRunning()
	if a.loaded {
		m.logAgentEvents(a.snap, msg.snap)
	}
	a.loaded = true
	a.snap = msg.snap
	m.refreshWatch()
	if was && !m.agentRunning() {
		m.say(ui.ResultOK, m.sessionWord()+" finished")
	}
	if a.follow && a.open && !a.changes {
		a.toEnd = true
	}
	if !m.spinning && m.agentRunning() {
		m.spinning = true
		return tickSpin()
	}
	return nil
}

// / agentRunning reports whether the watched session has an activity in
// progress: its spinner shows in the header and in the tree and live view.
func (m *Model) agentRunning() bool {
	a := &m.agent
	for _, x := range a.snap.Activities {
		if x.State == agent.StateRunning {
			return true
		}
	}
	for _, s := range a.snap.Subagents {
		for _, x := range s.Activities {
			if x.State == agent.StateRunning {
				return true
			}
		}
	}
	return false
}

// switchRepo re-opens the repository at dir (a worktree) and reloads everything.
func (m *Model) switchRepo(dir string) tea.Cmd {
	repo, err := git.Open(dir)
	if err != nil {
		m.errorPopup(err)
		return nil
	}
	repo.LogFunc = m.log.add
	m.repo = repo
	m.headHash = ""
	m.files.files, m.files.list = nil, list{}
	m.branches = branchesPanel{}
	m.commits = commitsPanel{}
	m.stashes = stashPanel{}
	m.agent = agentPanel{expanded: map[string]bool{}, follow: true}
	m.watch = newWatchState("", false, m.watch.hideLive)
	m.counts = nil
	m.main = mainView{}
	m.focus, m.mainFocus = pFiles, false
	m.log.add("switched to worktree " + repo.Dir)
	return m.startup()
}

func (m *Model) logAgentEvents(old, cur agent.Snapshot) {
	seen := map[string]agent.Subagent{}
	for _, s := range old.Subagents {
		seen[s.ID] = s
	}
	for _, s := range cur.Subagents {
		name := s.Type
		if name == "" {
			name = s.ID
		}
		prev, ok := seen[s.ID]
		switch {
		case !ok:
			m.log.add("subagent " + name + " started")
			m.say(ui.ResultInfo, "subagent "+name+" started")
			if s.Finished {
				m.log.add("subagent " + name + " finished")
			}
		case !prev.Finished && s.Finished:
			m.log.add("subagent " + name + " finished")
			m.say(ui.ResultOK, "subagent "+name+" finished")
		}
	}
}

// ---- lists ----

// syncLists re-finds every list selection after data or filters changed.
func (m *Model) syncLists() {
	for p := pFiles; p < numPanels; p++ {
		l := m.listOf(p)
		items := m.items(p, 40)
		if p == pAgent && m.agent.open && m.agent.toEnd {
			if len(items) > 0 {
				l.cur, l.key = len(items)-1, items[len(items)-1].key
			}
			m.agent.toEnd = false
		}
		l.sync(items)
	}
}

func (m *Model) listOf(p panelID) *list {
	switch p {
	case pFiles:
		return &m.files.list
	case pBranches:
		return &m.branches.list
	case pCommits:
		if m.commits.sub != nil {
			return &m.commits.sub.list
		}
		return &m.commits.list
	case pStash:
		return &m.stashes.list
	case pAgent:
		if m.agent.open {
			return &m.agent.tree
		}
		return &m.agent.sessList
	}
	return &m.agent.sessList
}

// items returns the (filtered) rows of a panel; w is the inner width.
func (m *Model) items(p panelID, w int) []item {
	var its []item
	switch p {
	case pFiles:
		its = m.filesItems(w)
	case pBranches:
		its = m.branchItems(w)
	case pCommits:
		its = m.commitItems(w)
	case pStash:
		its = m.stashItems(w)
	case pAgent:
		its = m.agentItems(w)
	}
	return filterItems(its, m.listOf(p).filter)
}

// ---- helpers ----

func (m *Model) errorPopup(err error) {
	m.popup = newError(err.Error())
	m.say(ui.ResultError, firstLine(err.Error()))
}

func (m *Model) confirm(title, body string, yes func() tea.Cmd) {
	m.popup = &popup{kind: pkConfirm, title: title, body: body, yes: yes}
}

// sessionWord names the watched session's runner for a notice.
func (m *Model) sessionWord() string {
	if r := m.agent.snap.Session.Runner; r != "" {
		return r
	}
	return m.src.Name()
}

func repoName(r *git.Repo) string { return filepath.Base(r.Dir) }

// defaultEditor picks the editor command: $EDITOR, then $VISUAL, then notepad
// on Windows, else the first of nano, vi and vim found on the PATH.
func defaultEditor() []string {
	for _, v := range []string{"EDITOR", "VISUAL"} {
		if ed := strings.Fields(os.Getenv(v)); len(ed) > 0 {
			return ed
		}
	}
	if runtime.GOOS == "windows" {
		return []string{"notepad"}
	}
	for _, name := range []string{"nano", "vi", "vim"} {
		if _, err := exec.LookPath(name); err == nil {
			return []string{name}
		}
	}
	return []string{"vi"}
}

// editorCmd opens path in the editor chosen by defaultEditor.
func (m *Model) editorCmd(path string) tea.Cmd {
	ed := defaultEditor()
	c := exec.Command(ed[0], append(ed[1:], filepath.Join(m.repo.Dir, filepath.FromSlash(path)))...)
	c.Dir = m.repo.Dir
	return tea.ExecProcess(c, func(err error) tea.Msg { return editorDoneMsg{err} })
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
