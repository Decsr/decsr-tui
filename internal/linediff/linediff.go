// Package linediff is a small line-based text differ for showing Edit activities.
package linediff

import (
	"fmt"
	"strings"
)

// maxLines caps each side after trimming common lines; bigger inputs fall
// back to "remove everything, add everything".
const maxLines = 2000

// Op is one diff line: Kind is ' ', '+' or '-'.
type Op struct {
	Kind byte
	Text string
}

func split(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// Diff returns the line diff turning old into new.
func Diff(old, new string) []Op {
	a, b := split(old), split(new)
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	var ops []Op
	for _, l := range a[:pre] {
		ops = append(ops, Op{' ', l})
	}
	ops = append(ops, middle(a[pre:len(a)-suf], b[pre:len(b)-suf])...)
	for _, l := range a[len(a)-suf:] {
		ops = append(ops, Op{' ', l})
	}
	return ops
}

func middle(a, b []string) []Op {
	var ops []Op
	if len(a) > maxLines || len(b) > maxLines {
		for _, l := range a {
			ops = append(ops, Op{'-', l})
		}
		for _, l := range b {
			ops = append(ops, Op{'+', l})
		}
		return ops
	}
	// lcs[i][j] = LCS length of a[i:] and b[j:]
	n, m := len(a), len(b)
	lcs := make([][]int32, n+1)
	for i := range lcs {
		lcs[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, Op{' ', a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, Op{'-', a[i]})
			i++
		default:
			ops = append(ops, Op{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, Op{'-', a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, Op{'+', b[j]})
	}
	return ops
}

// Unified renders the diff as unified text with ctx lines of context. It
// returns "" when old and new are equal.
func Unified(old, new string, ctx int) string {
	ops := Diff(old, new)
	keep := make([]bool, len(ops))
	changed := false
	for i, o := range ops {
		if o.Kind == ' ' {
			continue
		}
		changed = true
		for k := max(0, i-ctx); k <= min(len(ops)-1, i+ctx); k++ {
			keep[k] = true
		}
	}
	if !changed {
		return ""
	}
	var b strings.Builder
	oldLn, newLn := 1, 1
	for i := 0; i < len(ops); {
		if !keep[i] {
			if ops[i].Kind != '+' {
				oldLn++
			}
			if ops[i].Kind != '-' {
				newLn++
			}
			i++
			continue
		}
		j := i
		on, nn := 0, 0
		for j < len(ops) && keep[j] {
			if ops[j].Kind != '+' {
				on++
			}
			if ops[j].Kind != '-' {
				nn++
			}
			j++
		}
		fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", oldLn, on, newLn, nn)
		for _, o := range ops[i:j] {
			b.WriteString(string(o.Kind) + o.Text + "\n")
		}
		oldLn += on
		newLn += nn
		i = j
	}
	return b.String()
}
