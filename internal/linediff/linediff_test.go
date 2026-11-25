package linediff

import (
	"strings"
	"testing"
)

func render(ops []Op) string {
	var b strings.Builder
	for _, o := range ops {
		b.WriteString(string(o.Kind) + o.Text + "\n")
	}
	return b.String()
}

func TestDiff(t *testing.T) {
	cases := []struct{ old, new, want string }{
		{"", "a\nb\n", "+a\n+b\n"},
		{"a\nb", "", "-a\n-b\n"},
		{"a\nb\nc", "a\nb\nc", " a\n b\n c\n"},
		{"a\nb\nc", "a\nx\nc", " a\n-b\n+x\n c\n"},
		{"a\nb", "a\nb\nc\n", " a\n b\n+c\n"},
		{"a\nb\nc\nd", "b\nd\ne", "-a\n b\n-c\n d\n+e\n"},
	}
	for _, c := range cases {
		if got := render(Diff(c.old, c.new)); got != c.want {
			t.Errorf("Diff(%q,%q)\n got %q\nwant %q", c.old, c.new, got, c.want)
		}
	}
}

func TestDiffReconstructs(t *testing.T) {
	old := "1\n2\n3\n4\n5\n6\n7\n8"
	new := "1\n3\n4\nx\ny\n6\n8\n9"
	var gotOld, gotNew []string
	for _, o := range Diff(old, new) {
		if o.Kind != '+' {
			gotOld = append(gotOld, o.Text)
		}
		if o.Kind != '-' {
			gotNew = append(gotNew, o.Text)
		}
	}
	if strings.Join(gotOld, "\n") != old || strings.Join(gotNew, "\n") != new {
		t.Fatalf("not reconstructable: %q %q", gotOld, gotNew)
	}
}

func TestCap(t *testing.T) {
	a := strings.Repeat("a\n", 3000) + "x\n"
	b := strings.Repeat("b\n", 3000) + "x\n"
	ops := Diff(a, b)
	if len(ops) != 6001 {
		t.Fatalf("len %d", len(ops))
	}
}

func TestUnified(t *testing.T) {
	if Unified("a", "a", 3) != "" {
		t.Fatal("equal should be empty")
	}
	got := Unified("a\nb\nc\nd\ne\nf\ng\nh", "a\nb\nc\nd\ne\nf\ng\nX", 1)
	want := "@@ -7,2 +7,2 @@\n g\n-h\n+X\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
