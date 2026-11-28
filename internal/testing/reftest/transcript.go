package reftest

import (
	"fmt"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/pass"
)

// A Transcript is what a build writes: its configuration and the results of the controls, by tag.
type Transcript struct {
	Configuration string
	Results       map[string]string
}

// configurationHeading and resultsHeading begin the parts of a written transcript; the heading of
// the results of a control is followed by its tag.
const (
	configurationHeading = "== configuration\n"
	resultsHeading       = "== results "
)

// Take returns the transcript of this build for the controls.
func Take(t *testing.T, rcs []pass.ReferenceControl) Transcript {
	t.Helper()
	tr := Transcript{Configuration: Configuration(t), Results: map[string]string{}}
	for _, rc := range rcs {
		tr.Results[rc.Tag] = Results(t, rc)
	}
	return tr
}

// String returns the transcript as a build writes it, with the results in the order of the
// registry's controls.
func (tr Transcript) String() string {
	var b strings.Builder
	b.WriteString(configurationHeading)
	b.WriteString(tr.Configuration)
	for _, rc := range pass.ReferenceControls() {
		if r, ok := tr.Results[rc.Tag]; ok {
			b.WriteString(resultsHeading + rc.Tag + "\n")
			b.WriteString(r)
		}
	}
	return b.String()
}

// ParseTranscript reads a transcript that String wrote. No line of a part begins with "== ", which
// begins the headings.
func ParseTranscript(s string) (Transcript, error) {
	if !strings.HasPrefix(s, configurationHeading) {
		return Transcript{}, fmt.Errorf("the transcript does not begin with %q", configurationHeading)
	}
	tr := Transcript{Results: map[string]string{}}
	var part *strings.Builder
	var config strings.Builder
	results := map[string]*strings.Builder{}
	for _, l := range strings.SplitAfter(s, "\n") {
		switch {
		case l == "":
		case l == configurationHeading:
			part = &config
		case strings.HasPrefix(l, resultsHeading):
			tag := strings.TrimSuffix(strings.TrimPrefix(l, resultsHeading), "\n")
			if _, dup := results[tag]; dup {
				return Transcript{}, fmt.Errorf("the transcript has the results of %s twice", tag)
			}
			part = &strings.Builder{}
			results[tag] = part
		case strings.HasPrefix(l, "== "):
			return Transcript{}, fmt.Errorf("the line %q is no heading of a transcript", l)
		default:
			part.WriteString(l)
		}
	}
	tr.Configuration = config.String()
	for tag, b := range results {
		tr.Results[tag] = b.String()
	}
	return tr, nil
}

// Compare returns what is wrong with the transcript got of the build with the tag, against base, the
// transcript of the default build: a configuration that does not differ, which means the tag selected
// no file of the build, and results of the tag's control that differ, with the first line that
// differs.
func Compare(tag string, base, got Transcript) []string {
	var out []string
	if base.Configuration == got.Configuration {
		out = append(out, fmt.Sprintf("%s: the configuration of the build is that of the default build: the tag selects no file", tag))
	}
	want, ok := base.Results[tag]
	switch {
	case !ok:
		out = append(out, fmt.Sprintf("%s: the transcript of the default build has no results of the control", tag))
	case got.Results[tag] == "":
		out = append(out, fmt.Sprintf("%s: the build wrote no results of the control", tag))
	case got.Results[tag] != want:
		out = append(out, fmt.Sprintf("%s: the results differ from those of the default build\n%s", tag, firstDifference(want, got.Results[tag])))
	}
	return out
}

// firstDifference describes the first line in which a and b differ.
func firstDifference(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := range max(len(al), len(bl)) {
		var x, y string
		if i < len(al) {
			x = al[i]
		}
		if i < len(bl) {
			y = bl[i]
		}
		if x != y {
			return fmt.Sprintf("line %d:\n default   %s\n reference %s", i+1, x, y)
		}
	}
	return "no line differs"
}
