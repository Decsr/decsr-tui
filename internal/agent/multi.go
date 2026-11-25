package agent

import (
	"sort"
)

type multi struct{ sources []Source }

// Multi merges several sources into one named "all". A source that fails is skipped.
func Multi(sources ...Source) Source { return &multi{sources: sources} }

func (m *multi) Name() string { return "all" }

func (m *multi) Sessions(repoDir string) ([]Session, error) {
	var out []Session
	for _, s := range m.sources {
		ss, err := s.Sessions(repoDir)
		if err != nil {
			continue
		}
		out = append(out, ss...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out, nil
}

// Open routes the session to the source named by Session.Runner.
func (m *multi) Open(s Session) Tracker {
	for _, src := range m.sources {
		if src.Name() == s.Runner {
			return src.Open(s)
		}
	}
	return emptyTracker{s}
}

type emptyTracker struct{ s Session }

func (emptyTracker) Poll() (bool, error)  { return false, nil }
func (e emptyTracker) Snapshot() Snapshot { return Snapshot{Session: e.s} }
