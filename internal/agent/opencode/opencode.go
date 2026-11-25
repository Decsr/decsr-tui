// Package opencode reads opencode (sst/opencode, now anomalyco/opencode) sessions read-only.
//
// Two on-disk layouts are supported, both under $XDG_DATA_HOME/opencode or
// ~/.local/share/opencode:
//
//   - storage/ JSON files (opencode before the SQLite move): session/<projectID>/<id>.json,
//     message/<sessionID>/<id>.json, part/<messageID>/<id>.json, project/<id>.json.
//     Based on packages/opencode/src/session (Session.Info, MessageV2.Info,
//     MessageV2.Part, ToolState) and src/storage.
//   - opencode.db (SQLite, newer): tables project, session, message, part, where
//     message.data and part.data hold the same JSON as the old message/part files.
//     Based on session.sql.ts as described by third-party readers; opened with
//     mode=ro and query_only, never written.
//
// auth.json and every other credential file are never opened.
package opencode

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Decsr/decsr-tui/internal/agent"
)

const liveWindow = 15 * time.Second

type source struct {
	root string

	mu     sync.Mutex
	models map[string]modelAt // session id -> its model as of an update time
}

// modelAt is a model read when the session was last updated at at.
type modelAt struct {
	at    time.Time
	model string
}

// modelOf is the model of session r, read from its newest assistant message
// and kept until the session is updated again.
func (s *source) modelOf(be backend, r rawSession) string {
	s.mu.Lock()
	c, ok := s.models[r.id]
	s.mu.Unlock()
	if ok && c.at.Equal(r.updated) {
		return c.model
	}
	m := be.latestModel(r.id)
	s.mu.Lock()
	if s.models == nil {
		s.models = map[string]modelAt{}
	}
	s.models[r.id] = modelAt{r.updated, m}
	s.mu.Unlock()
	return m
}

// New reads from $XDG_DATA_HOME/opencode or ~/.local/share/opencode.
func New() agent.Source {
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "share")
	}
	return NewWithRoot(filepath.Join(dir, "opencode"))
}

// NewWithRoot reads from an explicit opencode data directory (tests).
func NewWithRoot(dataDir string) agent.Source { return &source{root: dataDir} }

func (s *source) Name() string { return "opencode" }

func (s *source) backends() []backend {
	var out []backend
	if db := filepath.Join(s.root, "opencode.db"); fileExists(db) {
		out = append(out, &sqlBackend{path: db})
	}
	if dir := filepath.Join(s.root, "storage"); dirExists(dir) {
		out = append(out, newJSONBackend(dir))
	}
	return out
}

func (s *source) Sessions(repoDir string) ([]agent.Session, error) {
	byID := map[string]agent.Session{}
	for _, be := range s.backends() {
		raws, err := be.list()
		if err != nil {
			continue
		}
		for _, r := range raws {
			if r.parent != "" || !agent.SameDir(r.directory, repoDir) {
				continue
			}
			ss := r.session()
			ss.Model = s.modelOf(be, r) // the session row has no model
			if old, ok := byID[ss.ID]; !ok || ss.Updated.After(old.Updated) {
				byID[ss.ID] = ss
			}
		}
	}
	out := make([]agent.Session, 0, len(byID))
	for _, ss := range byID {
		out = append(out, ss)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out, nil
}

func (s *source) Open(sess agent.Session) agent.Tracker {
	var be backend
	if strings.HasSuffix(sess.Path, ".db") {
		be = &sqlBackend{path: sess.Path}
	} else {
		be = newJSONBackend(filepath.Join(s.root, "storage"))
	}
	return &tracker{be: be, sess: sess, main: newSessData(sess.ID), subs: map[string]*sessData{}}
}

// rawSession is a session row, whichever store it came from.
type rawSession struct {
	id, parent, directory, title, path string
	created, updated                   time.Time
}

func (r rawSession) session() agent.Session {
	return agent.Session{
		Runner: "opencode", ID: r.id, Path: r.path, Title: r.title, Cwd: r.directory,
		Started: r.created, Updated: r.updated, Live: time.Since(r.updated) < liveWindow,
	}
}

func fileExists(p string) bool { st, err := os.Stat(p); return err == nil && !st.IsDir() }
func dirExists(p string) bool  { st, err := os.Stat(p); return err == nil && st.IsDir() }

func ms(v int64) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(v)
}
