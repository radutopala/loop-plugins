package quiz

import (
	_ "embed"
	"strings"
	"text/template"
)

//go:embed generate.md
var generateTemplate string

var promptTmpl = template.Must(template.New("generate").Parse(generateTemplate))

// promptData fills generate.md.
type promptData struct {
	ChannelName    string
	Count          int
	MaxAnchorLines int
	Focus          []FocusItem
	Avoid          []string
}

// renderPrompt renders the generation prompt.
func renderPrompt(d promptData) (string, error) {
	for i, a := range d.Avoid {
		d.Avoid[i] = oneLine(a, 200)
	}
	var b strings.Builder
	if err := promptTmpl.Execute(&b, d); err != nil {
		return "", err
	}
	return b.String(), nil
}

// oneLine collapses s onto one line of at most max runes.
func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}
