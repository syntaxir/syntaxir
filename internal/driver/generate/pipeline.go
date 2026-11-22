package generate

import (
	"context"
	"fmt"
	"io"
	"slices"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/ir/code/exec/library"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// Describe returns the pipeline text of the generation of the targets of p under x (record 074,
// The Text Door): the headers that give the options of the build, then the pipeline that
// driver.Compile runs for them. The Gen of each target holds its options, completed by its
// printer's validation, and the options of the whole call, which the first target gives; p is nil
// for the TypeScript library alone. Read reads the text back.
func Describe(p *driver.Program, x driver.Options, targets ...driver.Target) (string, error) {
	if len(targets) == 0 {
		return "", fmt.Errorf("a generation takes at least one target")
	}
	d, err := driver.Describe(p, x, targets...)
	if err != nil {
		return "", err
	}
	d.Header = headerLines(targets)
	return d.String(), nil
}

// headerLines returns the header lines of a generation of the targets: the version of sxir, then
// each word of Header in order, the words of the whole call from the options of the first target,
// and one target line per target, in order, labeled with its backend.
func headerLines(targets []driver.Target) []pipe.Line {
	lines := []pipe.Line{{Word: "sxir", Label: common.Version()}}
	for _, w := range Header.Words {
		if w.Word != "target" {
			lines = append(lines, Header.Line(w.Word, "", targets[0].Gen))
			continue
		}
		for _, t := range targets {
			lines = append(lines, Header.Line(w.Word, t.Backend.String(), t.Gen))
		}
	}
	return lines
}

// libraryWords are the header words of the text of the Go library, after sxir: it has no program and
// no target, so its text gives only the settings and the debugging settings.
var libraryWords = []string{"settings", "debug"}

// DescribeGoLibrary returns the pipeline text of the Go library under the settings and the debugging
// settings of o and the edits or the text of x: the headers sxir, settings and debug, then the
// pipeline that driver.GoLibrary runs, which starts at the library. Read reads it back.
func DescribeGoLibrary(o common.GenOptions, x driver.Options) (string, error) {
	d, err := driver.DescribeGoLibrary(x)
	if err != nil {
		return "", err
	}
	d.Header = []pipe.Line{{Word: "sxir", Label: common.Version()}}
	for _, w := range libraryWords {
		d.Header = append(d.Header, Header.Line(w, "", o))
	}
	return d.String(), nil
}

// GoLibrary generates the files of the Go library, the engine and their conformance bindings
// (driver.GoLibrary), with paths relative to the root of the module, running the passes that o
// selects (Settings) with the debugging settings of o; no other option of o applies, since no
// program and no target determine the library.
func GoLibrary(o common.GenOptions) ([]common.GeneratedFile, error) {
	return goLibrary(o, driver.Options{})
}

// goLibrary is GoLibrary with the edits, the text and the observers of x.
func goLibrary(o common.GenOptions, x driver.Options) ([]common.GeneratedFile, error) {
	var err error
	if x.Settings, err = Settings(o); err != nil {
		return nil, err
	}
	x.Debug = o.Debug
	if o.Debug.PrintPipeline {
		text, err := DescribeGoLibrary(o, x)
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(o.Debug.Output, text); err != nil {
			return nil, err
		}
	}
	out, err := driver.GoLibrary(context.Background(), x)
	if err != nil {
		return nil, err
	}
	return out.(*result.Files).Files, nil
}

// writePipeline writes the pipeline text of the generation of the target t of p under x to the
// debugging output when the debugging settings ask for it (common.Debug.PrintPipeline), before the
// build, so that a build that fails still shows what it ran.
func writePipeline(p *driver.Program, x driver.Options, targets []driver.Target) error {
	if !x.Debug.PrintPipeline {
		return nil
	}
	text, err := Describe(p, x, targets...)
	if err != nil {
		return err
	}
	_, err = io.WriteString(x.Debug.Output, text)
	return err
}

// Build is a generation that a pipeline text describes: its targets with their options and the
// text, whose pipeline replaces the assembled one (driver.Options.Pipeline). A text of the Go
// library is a build of its own (Library), which GoLibrary runs and Generate does not.
type Build struct {
	// Library is set for a text of the Go library, whose one target is Go and whose options are
	// those of the settings and the debugging settings only.
	Library bool
	// Targets are the targets of the build, in the order of their lines. The Gen of each holds the
	// options of the headers of the whole call (options, settings and debug) and of its own target
	// line. The fields that no header writes (Header.Omit) are zero: the libraries are those of this
	// version, which Generate supplies, and the writer of the debugging output is the caller's.
	Targets []driver.Target
	// Version is the version of sxir that printed the text. A text of another version is read, but
	// its passes may behave differently (Warning).
	Version  string
	Pipeline string
}

// Read reads the headers of a pipeline text that Describe or DescribeGoLibrary wrote: the version of
// sxir, then one line of each word of Header but target, and one target line or more, or, for a
// text of the Go library, whose pipeline starts at the library and which gives no target, one line
// of each of its words (libraryWords). A word that the scheme does not know, a word other than
// target given twice, a word that the text does not take, a key that its word does not have and a
// missing key are errors. The pipeline is
// checked when the build runs, against the pipeline assembled for its options.
func Read(text string) (*Build, error) {
	d, err := pipe.Parse(text)
	if err != nil {
		return nil, err
	}
	b := &Build{Pipeline: text}
	words := headerWords()
	if d.In == library.IR.Name && !slices.ContainsFunc(d.Header, func(l pipe.Line) bool { return l.Word == "target" }) {
		b.Library, words = true, libraryWords
	}
	// The options of the whole call, which every target takes, and the lines of the targets, which
	// are read over them.
	var call common.GenOptions
	var targets []pipe.Line
	seen := map[string]bool{}
	for _, l := range d.Header {
		if seen[l.Word] && l.Word != "target" {
			return nil, fmt.Errorf("pipeline text: the header %s is given twice", l.Word)
		}
		seen[l.Word] = true
		if l.Word != "sxir" && b.Library && !slices.Contains(words, l.Word) {
			return nil, fmt.Errorf("pipeline text: the text of the Go library has no header %s", l.Word)
		}
		switch l.Word {
		case "sxir":
			if l.Label == "" || len(l.Fields) > 0 {
				return nil, fmt.Errorf("pipeline text: want sxir <version>")
			}
			b.Version = l.Label
			continue
		case "target":
			targets = append(targets, l)
			continue
		}
		if err := Header.Read(l, &call); err != nil {
			return nil, fmt.Errorf("pipeline text: %w", err)
		}
	}
	for _, w := range append([]string{"sxir"}, words...) {
		if !seen[w] {
			return nil, fmt.Errorf("pipeline text: the header %s is missing", w)
		}
	}
	if b.Library {
		b.Targets = []driver.Target{{Backend: result.Go, Gen: call}}
		return b, nil
	}
	for _, l := range targets {
		t := driver.Target{Gen: call}
		switch l.Label {
		case result.Go.String():
			t.Backend = result.Go
		case result.TypeScript.String():
			t.Backend = result.TypeScript
		default:
			return nil, fmt.Errorf("pipeline text: %q is no target of a generation (go, ts)", l.Label)
		}
		if err := Header.Read(l, &t.Gen); err != nil {
			return nil, fmt.Errorf("pipeline text: %w", err)
		}
		b.Targets = append(b.Targets, t)
	}
	return b, nil
}

// headerWords returns the words of Header in order.
func headerWords() []string {
	var ws []string
	for _, w := range Header.Words {
		ws = append(ws, w.Word)
	}
	return ws
}

// Warning returns a warning for a text that another version of sxir printed, whose passes may
// behave differently, or "".
func (b *Build) Warning() string {
	if v := common.Version(); b.Version != v {
		return fmt.Sprintf("the pipeline text was printed by sxir %s, and this is sxir %s: its passes may behave differently", b.Version, v)
	}
	return ""
}

// Generate generates the files of each target of the build b for g, in order, with the libraries
// of this version, the observers of x and the writer of the debugging output out, which the
// debugging settings of the headers need when they write. The settings, the debugging settings and
// the pipeline of x come from b.
func (b *Build) Generate(g *ast.Grammar, x driver.Options, out io.Writer) ([][]common.GeneratedFile, error) {
	if b.Library {
		return nil, fmt.Errorf("the pipeline text builds the Go library, which takes no grammar")
	}
	targets := slices.Clone(b.Targets)
	for i := range targets {
		targets[i].Gen.Debug.Output = out
		targets[i].Gen.LibGo, targets[i].Gen.TSLib = libGo, tsLib
	}
	x.Pipeline = b.Pipeline
	return TargetsWith(checkForGenerator, g, x, targets...)
}

// GoLibrary generates the files of the Go library of the build b, a text of the Go library, with the
// observers of x and the writer of the debugging output out, which the debugging settings of the
// headers need when they write. The settings, the debugging settings and the pipeline of x come from b.
func (b *Build) GoLibrary(x driver.Options, out io.Writer) ([]common.GeneratedFile, error) {
	if !b.Library {
		return nil, fmt.Errorf("the pipeline text builds parsers, not the Go library")
	}
	o := b.Targets[0].Gen
	o.Debug.Output = out
	x.Pipeline = b.Pipeline
	return goLibrary(o, x)
}
