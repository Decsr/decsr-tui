package opencode

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, used read-only
)

type backend interface {
	list() ([]rawSession, error)
	children(parentID string) ([]rawSession, error)
	// latestModel is the model of the newest assistant message of a session, "" when unknown.
	latestModel(id string) string
	// load reads what changed since the last call into d and reports whether anything did.
	load(d *sessData) (bool, error)
}

type msgRec struct {
	id      string
	created int64
	data    json.RawMessage
}

type partRec struct {
	id, msgID string
	created   int64
	data      json.RawMessage
}

// sessData holds the messages and parts of one session read so far.
type sessData struct {
	id    string
	msgs  map[string]*msgRec
	parts map[string]*partRec
	last  time.Time // newest modification seen

	files               map[string]fstat // JSON store: per-file mtime and size
	sinceMsg, sincePart int64            // SQLite: newest time_updated seen (ms)
}

type fstat struct {
	mtime time.Time
	size  int64
}

func newSessData(id string) *sessData {
	return &sessData{id: id, msgs: map[string]*msgRec{}, parts: map[string]*partRec{}, files: map[string]fstat{}}
}

func (d *sessData) putMsg(id string, created int64, data []byte) bool {
	if old, ok := d.msgs[id]; ok && bytes.Equal(old.data, data) {
		return false
	}
	d.msgs[id] = &msgRec{id: id, created: created, data: append(json.RawMessage(nil), data...)}
	return true
}

func (d *sessData) putPart(id, msgID string, created int64, data []byte) bool {
	if old, ok := d.parts[id]; ok && bytes.Equal(old.data, data) {
		return false
	}
	d.parts[id] = &partRec{id: id, msgID: msgID, created: created, data: append(json.RawMessage(nil), data...)}
	return true
}

func (d *sessData) touch(t time.Time) {
	if t.After(d.last) {
		d.last = t
	}
}

// ---- JSON storage ----

type jsonBackend struct {
	dir   string // .../opencode/storage
	cache map[string]cachedSession
}

type cachedSession struct {
	st  fstat
	raw rawSession
	ok  bool
}

func newJSONBackend(dir string) *jsonBackend {
	return &jsonBackend{dir: dir, cache: map[string]cachedSession{}}
}

func (b *jsonBackend) worktrees() map[string]string {
	out := map[string]string{}
	files, _ := filepath.Glob(filepath.Join(b.dir, "project", "*.json"))
	for _, f := range files {
		var p struct{ ID, Worktree string }
		if data, err := os.ReadFile(f); err == nil && json.Unmarshal(data, &p) == nil && p.ID != "" {
			out[p.ID] = p.Worktree
		}
	}
	return out
}

func (b *jsonBackend) list() ([]rawSession, error) {
	files, err := filepath.Glob(filepath.Join(b.dir, "session", "*", "*.json"))
	if err != nil {
		return nil, err
	}
	var trees map[string]string
	var out []rawSession
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		st := fstat{info.ModTime(), info.Size()}
		c, hit := b.cache[f]
		if !hit || c.st != st {
			c = cachedSession{st: st}
			var s struct {
				ID        string                           `json:"id"`
				ProjectID string                           `json:"projectID"`
				Directory string                           `json:"directory"`
				ParentID  string                           `json:"parentID"`
				Title     string                           `json:"title"`
				Time      struct{ Created, Updated int64 } `json:"time"`
			}
			if data, err := os.ReadFile(f); err == nil && json.Unmarshal(data, &s) == nil && s.ID != "" {
				if s.Directory == "" {
					if trees == nil {
						trees = b.worktrees()
					}
					s.Directory = trees[s.ProjectID]
				}
				upd := ms(s.Time.Updated)
				if upd.IsZero() {
					upd = info.ModTime()
				}
				c.raw = rawSession{id: s.ID, parent: s.ParentID, directory: s.Directory, title: s.Title,
					path: f, created: ms(s.Time.Created), updated: upd}
				c.ok = true
			}
			b.cache[f] = c
		}
		if c.ok {
			out = append(out, c.raw)
		}
	}
	return out, nil
}

func (b *jsonBackend) children(parentID string) ([]rawSession, error) {
	all, err := b.list()
	var out []rawSession
	for _, r := range all {
		if r.parent == parentID {
			out = append(out, r)
		}
	}
	return out, err
}

// readChanged reads the JSON file at p unless its mtime and size match the last read.
func (d *sessData) readChanged(p string) ([]byte, bool) {
	info, err := os.Stat(p)
	if err != nil {
		return nil, false
	}
	st := fstat{info.ModTime(), info.Size()}
	if d.files[p] == st {
		return nil, false
	}
	data, err := os.ReadFile(p)
	if err != nil || !json.Valid(data) { // may be mid-write: retry next poll
		return nil, false
	}
	d.files[p] = st
	d.touch(st.mtime)
	return data, true
}

func (b *jsonBackend) load(d *sessData) (bool, error) {
	changed := false
	msgFiles, _ := filepath.Glob(filepath.Join(b.dir, "message", d.id, "*.json"))
	for _, f := range msgFiles {
		data, ok := d.readChanged(f)
		if !ok {
			continue
		}
		var m struct {
			ID   string
			Time struct{ Created int64 }
		}
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		if m.ID == "" {
			m.ID = strings.TrimSuffix(filepath.Base(f), ".json")
		}
		if d.putMsg(m.ID, m.Time.Created, data) {
			changed = true
		}
	}
	ids := make([]string, 0, len(d.msgs))
	for id := range d.msgs {
		ids = append(ids, id)
	}
	for _, mid := range ids {
		partFiles, _ := filepath.Glob(filepath.Join(b.dir, "part", mid, "*.json"))
		for _, f := range partFiles {
			data, ok := d.readChanged(f)
			if !ok {
				continue
			}
			var p struct {
				ID   string
				Time struct{ Start, Created int64 }
			}
			if json.Unmarshal(data, &p) != nil {
				continue
			}
			if p.ID == "" {
				p.ID = strings.TrimSuffix(filepath.Base(f), ".json")
			}
			if d.putPart(p.ID, mid, p.Time.Start, data) {
				changed = true
			}
		}
	}
	return changed, nil
}

// ---- SQLite ----

type sqlBackend struct{ path string }

func (b *sqlBackend) open() (*sql.DB, error) {
	u := "file:" + filepath.ToSlash(b.path)
	if !strings.HasPrefix(filepath.ToSlash(b.path), "/") {
		u = "file:/" + filepath.ToSlash(b.path) // Windows drive path
	}
	q := url.Values{}
	q.Add("mode", "ro")
	q.Add("_pragma", "query_only(1)")
	q.Add("_pragma", "busy_timeout(2000)")
	return sql.Open("sqlite", u+"?"+q.Encode())
}

func (b *sqlBackend) query(where string, args ...any) ([]rawSession, error) {
	db, err := b.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	const sel = `SELECT s.id, COALESCE(s.parent_id,''), COALESCE(s.directory,''), COALESCE(p.worktree,''),
		COALESCE(s.title,''), COALESCE(s.time_created,0), COALESCE(s.time_updated,0)
		FROM session s LEFT JOIN project p ON p.id = s.project_id `
	rows, err := db.Query(sel+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []rawSession
	for rows.Next() {
		var r rawSession
		var tree string
		var c, u int64
		if rows.Scan(&r.id, &r.parent, &r.directory, &tree, &r.title, &c, &u) != nil {
			continue
		}
		if r.directory == "" {
			r.directory = tree
		}
		r.path, r.created, r.updated = b.path, ms(c), ms(u)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (b *sqlBackend) list() ([]rawSession, error) { return b.query("") }

func (b *sqlBackend) children(parentID string) ([]rawSession, error) {
	return b.query("WHERE s.parent_id = ? ORDER BY s.time_created", parentID)
}

func (b *sqlBackend) load(d *sessData) (bool, error) {
	db, err := b.open()
	if err != nil {
		return false, err
	}
	defer db.Close()
	changed := false
	rows, err := db.Query(`SELECT id, COALESCE(time_created,0), COALESCE(time_updated,0), data
		FROM message WHERE session_id = ? AND time_updated >= ?`, d.id, d.sinceMsg)
	if err != nil {
		return false, err
	}
	for rows.Next() {
		var id, data string
		var c, u int64
		if rows.Scan(&id, &c, &u, &data) != nil {
			continue
		}
		if d.putMsg(id, c, []byte(data)) {
			changed = true
		}
		d.sinceMsg = max(d.sinceMsg, u)
		d.touch(ms(u))
	}
	rows.Close()
	rows, err = db.Query(`SELECT id, message_id, COALESCE(time_created,0), COALESCE(time_updated,0), data
		FROM part WHERE session_id = ? AND time_updated >= ?`, d.id, d.sincePart)
	if err != nil {
		return changed, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, mid, data string
		var c, u int64
		if rows.Scan(&id, &mid, &c, &u, &data) != nil {
			continue
		}
		if d.putPart(id, mid, c, []byte(data)) {
			changed = true
		}
		d.sincePart = max(d.sincePart, u)
		d.touch(ms(u))
	}
	return changed, rows.Err()
}

// sortedMsgs returns messages oldest first.
func (d *sessData) sortedMsgs() []*msgRec {
	out := make([]*msgRec, 0, len(d.msgs))
	for _, m := range d.msgs {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].created != out[j].created {
			return out[i].created < out[j].created
		}
		return out[i].id < out[j].id
	})
	return out
}

// latestModel is the model named by the newest assistant message of a session,
// read straight from the store (the session row has none). It looks at the
// newest 30 messages and returns "" when none names a model.
func (b *sqlBackend) latestModel(id string) string {
	db, err := b.open()
	if err != nil {
		return ""
	}
	defer db.Close()
	rows, err := db.Query(`SELECT data FROM message WHERE session_id = ? ORDER BY COALESCE(time_created,0) DESC LIMIT 30`, id)
	if err != nil {
		return ""
	}
	defer rows.Close()
	for rows.Next() {
		var data string
		if rows.Scan(&data) == nil {
			if m := assistantModel([]byte(data)); m != "" {
				return m
			}
		}
	}
	return ""
}

func (b *jsonBackend) latestModel(id string) string {
	files, _ := filepath.Glob(filepath.Join(b.dir, "message", id, "*.json"))
	type fm struct {
		path  string
		mtime time.Time
	}
	var fs []fm
	for _, f := range files {
		if info, err := os.Stat(f); err == nil {
			fs = append(fs, fm{f, info.ModTime()})
		}
	}
	sort.Slice(fs, func(i, j int) bool { return fs[i].mtime.After(fs[j].mtime) })
	for i, f := range fs {
		if i == 30 {
			break
		}
		if data, err := os.ReadFile(f.path); err == nil {
			if m := assistantModel(data); m != "" {
				return m
			}
		}
	}
	return ""
}
