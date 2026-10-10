package quiz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsSource(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"main.go", true},
		{"internal/store/store.go", true},
		{"web/src/App.tsx", true},
		{"lib/x.PY", true},
		{"README.md", false},
		{"go.sum", false},
		{"package-lock.json", false},
		{"yarn.lock", false},
		{"internal/store/store_test.go", false},
		{"src/app.spec.ts", false},
		{"src/app.test.js", false},
		{"tests/test_app.py", false},
		{"vendor/github.com/x/y.go", false},
		{"web/node_modules/react/index.js", false},
		{"api/gen/api.go", false},
		{"api/v1/api.pb.go", false},
		{"web/dist/app.min.js", false},
		{"types/index.d.ts", false},
		{"internal/mocks/mock_store.go", false},
		{".github/scripts/run.sh", false},
		{"pkg/testdata/a.go", false},
		{"pkg/kind_string.go", false},
		{"pkg/.hidden.go", false},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			require.Equal(t, tc.want, IsSource(tc.path))
		})
	}
}

func TestArea(t *testing.T) {
	require.Equal(t, "internal/store", Area("internal/store/store.go"))
	require.Equal(t, ".", Area("main.go"))
}

func TestCleanPath(t *testing.T) {
	cases := map[string]string{
		"a/b.go":     "a/b.go",
		"./a/b.go":   "a/b.go",
		" a.go ":     "a.go",
		"":           "",
		"/abs/a.go":  "",
		"../a.go":    "",
		"a/../b.go":  "",
		"a//b.go":    "",
		".":          "",
		"..":         "",
		`a\b.go`:     "",
		".git/HEAD":  "",
		".git":       "",
		".github/x":  ".github/x",
		"a/./b.go":   "",
		"a/b.go/":    "",
		"./":         "",
		"a/b c.go":   "a/b c.go",
		"a/.git/x":   "a/.git/x",
		"dir/../../": "",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			require.Equal(t, want, cleanPath(in))
		})
	}
}

func TestFingerprint(t *testing.T) {
	require.Equal(t, Fingerprint("What does Open do?"), Fingerprint("  what DOES open do  "))
	require.NotEqual(t, Fingerprint("What does Open do?"), Fingerprint("What does Close do?"))
	require.Len(t, Fingerprint("x"), 32)
}

func TestCatchAll(t *testing.T) {
	cases := map[string]bool{
		"All of the above":             true,
		"none of these":                true,
		"Both of the above":            true,
		"Neither of the other options": true,
		"none of the choices":          true,
		"It retries all requests":      false,
		"None":                         false,
		"above the fold":               false,
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			require.Equal(t, want, catchAllRe.MatchString(in))
		})
	}
}

func TestOneLine(t *testing.T) {
	require.Equal(t, "a b c", oneLine("a\n  b\tc", 10))
	require.Equal(t, "abcd…", oneLine("abcdefgh", 5))
}

func TestRenderPrompt(t *testing.T) {
	out, err := renderPrompt(promptData{
		ChannelName: "proj", Count: 4, MaxAnchorLines: 200,
		Focus: []FocusItem{{Path: "a/b.go", Reason: "changed in 3 recent commits"}},
		Avoid: []string{"What does\nOpen do?"},
	})
	require.NoError(t, err)
	require.Contains(t, out, `channel "proj"`)
	require.Contains(t, out, "- `a/b.go`: changed in 3 recent commits")
	require.Contains(t, out, "## Write 4 questions")
	require.Contains(t, out, "- What does Open do?")
	require.Contains(t, out, "plugin_submit_questions")
	require.Contains(t, out, "at most 200 lines")

	out, err = renderPrompt(promptData{ChannelName: "proj", Count: 1})
	require.NoError(t, err)
	require.NotContains(t, out, "Do not repeat")
}

func TestNextMastery(t *testing.T) {
	now := mustDate("2026-10-10")
	cases := []struct {
		name    string
		box     int
		found   bool
		correct bool
		wantBox int
		wantDue string
	}{
		{"new area right", 0, false, true, 2, "2026-10-12"},
		{"new area wrong", 0, false, false, 1, "2026-10-11"},
		{"up a box", 3, true, true, 4, "2026-10-18"},
		{"top box stays", 5, true, true, 5, "2026-10-26"},
		{"wrong goes back", 4, true, false, 1, "2026-10-11"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := nextMasteryFor(tc.box, tc.found, tc.correct, now)
			require.Equal(t, tc.wantBox, m.Box)
			require.Equal(t, tc.wantDue, m.Due)
			require.Equal(t, now, m.UpdatedAt)
		})
	}
}
