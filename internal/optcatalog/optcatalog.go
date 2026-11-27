// Package optcatalog generates the tables of the optimization catalog (docs/optimizations/README.md)
// and the flag table of the code-generation guide from the registry of optimizations (package pass),
// the profiles (package driver), the optimization records and the file of measurement runs, as
// design record 080 says. The generated tables sit between marker comments in hand-written pages; a
// test regenerates them and fails on any difference.
//
// The package links every part of the compiler that registers flags, so its registry is the
// registry of the generator.
package optcatalog

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	// The parts that register flags beside those that driver links.
	_ "syntaxir.dev/sxir/internal/backend/codegen/golang"
	_ "syntaxir.dev/sxir/internal/backend/codegen/typescript"
	_ "syntaxir.dev/sxir/internal/backend/codegen/typescript/pass"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/pass"
)

// The files with generated regions, relative to the root of the repository.
const (
	CatalogPath   = "docs/optimizations/README.md"
	GuidePath     = "docs/guide/code-generation.md"
	MeasuredPath  = "docs/optimizations/measured.json"
	RecordsDir    = "docs/optimizations"
	MechanismsDir = "docs/optimizations/mechanisms"
)

// State is the lifecycle state of a registered flag.
type State string

const (
	Adopted    State = "adopted"
	OptIn      State = "opt-in"
	Experiment State = "experiment"
)

// StateOf returns the state of the flag: an experiment as it is declared, adopted if speed or a
// profile turns it on, an opt-in otherwise; and the profiles that turn it on.
func StateOf(f pass.Flag) (State, []string, error) {
	var on []string
	for _, name := range driver.Profiles() {
		s, err := driver.Profile(name)
		if err != nil {
			return "", nil, err
		}
		if s.On(f.Name) {
			on = append(on, name)
		}
	}
	switch {
	case f.Experiment:
		return Experiment, on, nil
	case len(on) > 0:
		return Adopted, on, nil
	}
	return OptIn, nil, nil
}

// Record is what the catalog reads of an optimization record: its number, file, status, and the
// states it decides for flags (its "Decides" header line).
type Record struct {
	Number  int
	File    string
	Status  string
	Decides map[string]State
}

var (
	recordFile  = regexp.MustCompile(`^(\d{3})-.+\.md$`)
	statusLine  = regexp.MustCompile(`^(?:- )?\*\*Status\*\*:?\s*:?\s*(.*)$`)
	decidesLine = regexp.MustCompile("^- \\*\\*Decides\\*\\*: (.*)$")
	decidesItem = regexp.MustCompile("^`([a-z0-9-]+)` (adopted|opt-in|experiment)$")
)

// ReadRecords reads the header of every numbered optimization record under root.
func ReadRecords(root string) (map[int]Record, error) {
	dir := filepath.Join(root, RecordsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[int]Record{}
	for _, e := range entries {
		m := recordFile.FindStringSubmatch(e.Name())
		if m == nil || m[1] == "000" {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		r, err := readRecord(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		r.Number, r.File = n, e.Name()
		if _, dup := out[n]; dup {
			return nil, fmt.Errorf("two optimization records have the number %03d", n)
		}
		out[n] = r
	}
	return out, nil
}

// readRecord reads the header lines of a record: those before its first section.
func readRecord(path string) (Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Record{}, err
	}
	r := Record{Decides: map[string]State{}}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "## ") {
			break
		}
		if m := statusLine.FindStringSubmatch(line); m != nil && r.Status == "" {
			r.Status = strings.TrimSpace(m[1])
		}
		if m := decidesLine.FindStringSubmatch(line); m != nil {
			for _, item := range strings.Split(m[1], ", ") {
				d := decidesItem.FindStringSubmatch(strings.TrimSuffix(item, "."))
				if d == nil {
					return Record{}, fmt.Errorf("%s: the Decides item %q is not \"`flag` state\"", filepath.Base(path), item)
				}
				r.Decides[d[1]] = State(d[2])
			}
		}
	}
	return r, sc.Err()
}

// ReadMeasured reads the file that maps each flag to the timed measurement run that last covered it.
func ReadMeasured(root string) (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(root, MeasuredPath))
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", MeasuredPath, err)
	}
	return out, nil
}

// Generate returns the files with generated regions under root, each with its regions as the
// registry, the profiles, the records and the measurement runs make them.
func Generate(root string) (map[string][]byte, error) {
	records, err := ReadRecords(root)
	if err != nil {
		return nil, err
	}
	measured, err := ReadMeasured(root)
	if err != nil {
		return nil, err
	}
	catalog, err := catalogRegions(records, measured)
	if err != nil {
		return nil, err
	}
	guide, err := guideRegions()
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for path, regions := range map[string]map[string]string{CatalogPath: catalog, GuidePath: guide} {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			return nil, err
		}
		data, err = Replace(data, regions)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out[path] = data
	}
	return out, nil
}

// Replace replaces the content of each region of data, between the lines
// "<!-- generated:NAME -->" and "<!-- /generated:NAME -->", with the region's text. A region of
// data that regions lacks, or one of regions that data lacks, is an error.
func Replace(data []byte, regions map[string]string) ([]byte, error) {
	s := string(data)
	for name, text := range regions {
		open, close := "<!-- generated:"+name+" -->\n", "<!-- /generated:"+name+" -->"
		i := strings.Index(s, open)
		j := strings.Index(s, close)
		if i < 0 || j < i {
			return nil, fmt.Errorf("no generated region %s", name)
		}
		s = s[:i+len(open)] + text + s[j:]
	}
	for _, m := range regexp.MustCompile(`<!-- generated:([a-z-]+) -->`).FindAllStringSubmatch(s, -1) {
		if _, ok := regions[m[1]]; !ok {
			return nil, fmt.Errorf("the generated region %s has no generator", m[1])
		}
	}
	return []byte(s), nil
}

// cell is the text of a backend cell of the index.
func cell(m pass.Mechanism, b pass.Backend) string {
	switch {
	case m.Full.Has(b):
		return "✓"
	case m.Partial.Has(b):
		return "Partial"
	}
	return "—"
}

// stateText is the state of a flag as the tables write it: "adopted (speed, size)", "opt-in" or
// "experiment".
func stateText(f pass.Flag) (string, error) {
	st, on, err := StateOf(f)
	if err != nil {
		return "", err
	}
	if st == Adopted {
		return string(st) + " (" + strings.Join(on, ", ") + ")", nil
	}
	return string(st), nil
}

// catalogRegions returns the regions of the catalog: the counts of the release criterion, the index of
// the classified mechanisms, the settings, and the reference controls.
func catalogRegions(records map[int]Record, measured map[string]string) (map[string]string, error) {
	flags := pass.Flags()
	var none int
	for _, m := range pass.Mechanisms() {
		if pass.ControlOf(m.ID, flags) == pass.ControlNone {
			none++
		}
	}
	counts := fmt.Sprintf("Unclassified mechanisms: %d. Optimization mechanisms without a control: %d. Both are 0 at a release.\n", len(pass.Unclassified()), none)

	var index strings.Builder
	index.WriteString("| ID | Mechanism | Path scope |")
	for _, b := range pass.AllBackends() {
		index.WriteString(" " + b.String() + " |")
	}
	index.WriteString(" Controls |\n|:--|:--|:--|")
	for range pass.AllBackends() {
		index.WriteString(":-:|")
	}
	index.WriteString(":--|\n")
	for _, m := range pass.Mechanisms() {
		fmt.Fprintf(&index, "| [%s](mechanisms/%s) | %s | %s |", m.ID, m.Page, m.Title, m.Scope)
		for _, b := range pass.AllBackends() {
			index.WriteString(" " + cell(m, b) + " |")
		}
		var controls []string
		for _, f := range flags {
			if f.Mechanism == m.ID {
				controls = append(controls, "`"+f.Name+"`")
			}
		}
		for _, r := range pass.ReferenceControls() {
			if r.Mechanism == m.ID {
				controls = append(controls, "`"+r.Tag+"` (reference)")
			}
		}
		if len(controls) == 0 {
			controls = []string{"none yet"}
		}
		index.WriteString(" " + strings.Join(controls, ", ") + " |\n")
	}

	var settings strings.Builder
	settings.WriteString("| Setting | Mechanism | Kind | State | Backends | Record | Last timed run |\n|:--|:--|:--|:--|:--|:--|:--|\n")
	for _, f := range flags {
		st, err := stateText(f)
		if err != nil {
			return nil, err
		}
		r, ok := records[f.Record]
		if !ok {
			return nil, fmt.Errorf("flag %s names the record %03d, which does not exist", f.Name, f.Record)
		}
		run := measured[f.Name]
		if run == "" {
			run = "none"
		}
		fmt.Fprintf(&settings, "| `%s` | %s | %s | %s | %s | [%03d](%s) | %s |\n", f.Name, f.Mechanism, f.Kind, st, f.Backends, r.Number, r.File, run)
	}

	refs := "None.\n"
	if rcs := pass.ReferenceControls(); len(rcs) > 0 {
		var b strings.Builder
		b.WriteString("| Build tag | Mechanism | Backends | Record |\n|:--|:--|:--|:--|\n")
		for _, rc := range rcs {
			r, ok := records[rc.Record]
			if !ok {
				return nil, fmt.Errorf("reference control %s names the record %03d, which does not exist", rc.Tag, rc.Record)
			}
			fmt.Fprintf(&b, "| `%s` | %s | %s | [%03d](%s) |\n", rc.Tag, rc.Mechanism, rc.Backends, r.Number, r.File)
		}
		refs = b.String()
	}
	return map[string]string{"counts": counts, "index": index.String(), "settings": settings.String(), "reference-controls": refs}, nil
}

// profileList writes the names of profiles as the guide does: "`size` profile" or "`a` and `b` profiles".
func profileList(names []string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = "`" + n + "`"
	}
	if len(q) == 1 {
		return q[0] + " profile"
	}
	return strings.Join(q[:len(q)-1], ", ") + " and " + q[len(q)-1] + " profiles"
}

// guideRegions returns the regions of the code-generation guide: the table of the flags, each row
// with what the flag does, what it trades, and whether it is an opt-in, an experiment, or on only in
// profiles other than speed.
func guideRegions() (map[string]string, error) {
	var b strings.Builder
	b.WriteString("| Name | Kind | Default | What it does and what it costs |\n| --- | --- | --- | --- |\n")
	for _, f := range pass.Flags() {
		st, on, err := StateOf(f)
		if err != nil {
			return nil, err
		}
		text := strings.ToUpper(f.Doc[:1]) + f.Doc[1:] + ". " + f.Tradeoff
		if f.ChangesOutput {
			text += ". It changes output"
		}
		switch {
		case st == Experiment:
			text = "Experimental. " + text
		case st == OptIn && f.Int:
			text += ". It is an opt-in"
		case st == OptIn:
			text += ". It is an opt-in: turn it on with `-opt " + f.Name + "`"
		case st == Adopted && !slices.Contains(on, "speed"):
			text += ". It is on in the " + profileList(on)
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n", f.Name, f.Kind, f.DefaultText(), text)
	}
	return map[string]string{"flags": b.String()}, nil
}
