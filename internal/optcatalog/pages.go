package optcatalog

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/pass"
)

// A mechanism page is hybrid. Its facts region, from the page's title to the end of the region, is
// generated from the registry: the classification, the backends the mechanism acts on and the controls
// with their state. Its prose is written by hand, in the sections that PageSections names, each of
// which must say something.

// FactsRegion is the name of the generated region of a mechanism page.
const FactsRegion = "facts"

// PageSections are the hand-written sections of a mechanism page, in their order. The summary, the
// text between the facts region and the first section, is required too.
var PageSections = []string{
	// What the mechanism does, and the invariants that make a parser with it return what a parser
	// without it returns.
	"Mechanism and Invariants",
	// Where it applies and why, naming the eligible subset of each partial backend.
	"Scope",
	// What its controls compare and what they do not isolate, and what the measurements mean; for a
	// mechanism without a control, why no existing switch isolates it.
	"Controls and Measurements",
	// The records that hold its evidence, the packages that implement it, and related mechanisms.
	"Evidence and Implementation",
}

// mechanismPages returns every mechanism page with its facts region as the registry makes it. A
// mechanism without a page gets a stub: the facts and empty sections, which CheckPageProse rejects
// until they are written.
func mechanismPages(root string, records map[int]Record) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, m := range pass.Mechanisms() {
		p := path.Join(MechanismsDir, m.Page)
		facts, err := factsRegion(m, records)
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(filepath.Join(root, p))
		if errors.Is(err, fs.ErrNotExist) {
			data = []byte(stubPage())
		} else if err != nil {
			return nil, err
		}
		data, err = Replace(data, map[string]string{FactsRegion: facts})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		out[p] = data
	}
	return out, nil
}

// stubPage is the page of a newly registered mechanism before its prose is written.
func stubPage() string {
	var b strings.Builder
	b.WriteString("<!-- generated:" + FactsRegion + " -->\n<!-- /generated:" + FactsRegion + " -->\n\n")
	for _, s := range PageSections {
		b.WriteString("## " + s + "\n\n")
	}
	b.WriteString("[Optimization Catalog](../README.md)\n")
	return b.String()
}

// factsRegion is the generated part of the page of m: its title and what the registry says about it.
func factsRegion(m pass.Mechanism, records map[int]Record) (string, error) {
	flags := pass.Flags()
	var b strings.Builder
	fmt.Fprintf(&b, "# %s: %s\n\n", m.ID, m.Title)
	b.WriteString("- **Classification**: optimization ([design record 080](../../design/records/080-optimization-management.md))\n")
	fmt.Fprintf(&b, "- **Path scope**: %s\n", m.Scope)
	if m.Experiment != "" {
		fmt.Fprintf(&b, "- **State**: experiment (off by default); the build tag `%s` turns it on\n", m.Experiment)
		fmt.Fprintf(&b, "- **With the tag, an eligible subset, named under Scope, of**: %s\n", m.Partial)
		b.WriteString("- **Controls**: none; an experiment gets a control when it is adopted\n")
		return b.String(), nil
	}
	if m.Full != 0 {
		fmt.Fprintf(&b, "- **Every parser of**: %s\n", m.Full)
	}
	if m.Partial != 0 {
		fmt.Fprintf(&b, "- **An eligible subset, named under Scope, of**: %s\n", m.Partial)
	}

	var rows []string
	for _, f := range flags {
		if f.Mechanism != m.ID {
			continue
		}
		st, err := stateText(f)
		if err != nil {
			return "", err
		}
		profiles, err := profileValues(f)
		if err != nil {
			return "", err
		}
		link, err := recordLink(records, f.Record)
		if err != nil {
			return "", fmt.Errorf("flag %s: %w", f.Name, err)
		}
		rows = append(rows, fmt.Sprintf("| `%s` | setting (%s) | %s | %s | %s | %s | %s |", f.Name, f.Kind, f.DefaultText(), profiles, st, f.Backends, link))
	}
	for _, rc := range pass.ReferenceControls() {
		if rc.Mechanism != m.ID {
			continue
		}
		link, err := recordLink(records, rc.Record)
		if err != nil {
			return "", fmt.Errorf("reference control %s: %w", rc.Tag, err)
		}
		rows = append(rows, fmt.Sprintf("| `%s` | reference build tag | — | — | — | %s | %s |", rc.Tag, rc.Backends, link))
	}
	if len(rows) == 0 {
		b.WriteString("- **Controls**: none yet; a release needs a control on every backend the mechanism acts on\n")
	} else {
		b.WriteString("\n| Control | Kind | Default | Profiles | State | Backends | Record |\n|:--|:--|:--|:--|:--|:--|:--|\n")
		b.WriteString(strings.Join(rows, "\n") + "\n")
		if rest := pass.Uncontrolled(m, flags); rest != 0 {
			fmt.Fprintf(&b, "\nNo control reaches it yet on: %s.\n", rest)
		}
	}
	return b.String(), nil
}

// profileValues is the value of the flag in each profile, as "speed 4, size 0".
func profileValues(f pass.Flag) (string, error) {
	var out []string
	for _, name := range driver.Profiles() {
		s, err := driver.Profile(name)
		if err != nil {
			return "", err
		}
		v := s.Value(f.Name)
		text := "off"
		switch {
		case f.Int:
			text = fmt.Sprint(v)
		case v != 0:
			text = "on"
		}
		out = append(out, name+" "+text)
	}
	return strings.Join(out, ", "), nil
}

// recordLink is the link from a mechanism page to the record of the number.
func recordLink(records map[int]Record, n int) (string, error) {
	r, ok := records[n]
	if !ok {
		return "", fmt.Errorf("the record %03d does not exist", n)
	}
	rel, err := filepath.Rel(MechanismsDir, RecordsDir)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("[%03d](%s)", r.Number, path.Join(filepath.ToSlash(rel), r.File)), nil
}

// CheckPageProse reports a mechanism page whose hand-written part is incomplete: a summary or a
// section of PageSections that is missing or empty, a section out of order, or a section that
// PageSections does not name.
func CheckPageProse(name string, data []byte) error {
	s := string(data)
	end := "<!-- /generated:" + FactsRegion + " -->"
	i := strings.Index(s, end)
	if i < 0 {
		return fmt.Errorf("%s has no generated %s region", name, FactsRegion)
	}
	s = s[i+len(end):]
	parts := strings.Split("\n"+s, "\n## ")
	if prose(parts[0]) == "" {
		return fmt.Errorf("%s has no summary after its facts", name)
	}
	var errs []error
	if len(parts)-1 != len(PageSections) {
		errs = append(errs, fmt.Errorf("%s has %d sections, want %d: %s", name, len(parts)-1, len(PageSections), strings.Join(PageSections, ", ")))
	}
	for k, part := range parts[1:] {
		heading, body, _ := strings.Cut(part, "\n")
		if k < len(PageSections) && heading != PageSections[k] {
			errs = append(errs, fmt.Errorf("%s: section %d is %q, want %q", name, k+1, heading, PageSections[k]))
		}
		if prose(body) == "" {
			errs = append(errs, fmt.Errorf("%s: the section %q is empty", name, heading))
		}
	}
	return errors.Join(errs...)
}

// prose is the text with HTML comments, the catalog link and white space removed.
func prose(s string) string {
	for {
		i := strings.Index(s, "<!--")
		if i < 0 {
			break
		}
		j := strings.Index(s[i:], "-->")
		if j < 0 {
			break
		}
		s = s[:i] + s[i+j+3:]
	}
	s = strings.ReplaceAll(s, "[Optimization Catalog](../README.md)", "")
	return strings.TrimSpace(s)
}
