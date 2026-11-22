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

// Describe returns the pipeline text of the generation of the target t of p under x (record 074,
// The Text Door): the headers that give the options of the build, then the pipeline that
// driver.Compile runs for it. t.Gen holds the options of the target, completed by its printer's
// validation; p is nil for the TypeScript library alone. Read reads the text back.
func Describe(p *driver.Program, x driver.Options, t driver.Target) (string, error) {
	d, err := driver.Describe(p, x, t)
	if err != nil {
		return "", err
	}
	d.Header = headerLines(t.Backend, t.Gen)
	return d.String(), nil
}

// headerLines returns the header lines of a generation by the backend b with the options o: the
// version of sxir, then each word of Header in order, the target labeled with its backend.
func headerLines(b result.Backend, o common.GenOptions) []pipe.Line {
	lines := []pipe.Line{{Word: "sxir", Label: common.Version()}}
	for _, w := range Header.Words {
		label := ""
		if w.Word == "target" {
			label = b.String()
		}
		lines = append(lines, Header.Line(w.Word, label, o))
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
func writePipeline(p *driver.Program, x driver.Options, t driver.Target) error {
	if !t.Gen.Debug.PrintPipeline {
		return nil
	}
	text, err := Describe(p, x, t)
	if err != nil {
		return err
	}
	_, err = io.WriteString(t.Gen.Debug.Output, text)
	return err
}

// Build is a generation that a pipeline text describes: its backend, its options and the text,
// whose pipeline replaces the assembled one (driver.Options.Pipeline). A text of the Go library is a
// build of its own (Library), which GoLibrary runs and Generate does not.
type Build struct {
	Backend result.Backend
	// Library is set for a text of the Go library, whose Backend is result.Go and whose options are
	// those of the settings and the debugging settings only.
	Library bool
	// Gen holds the options of the headers. The fields that no header writes (Header.Omit) are
	// zero: the libraries are those of this version, which Generate supplies, and the writer of the
	// debugging output is the caller's.
	Gen common.GenOptions
	// Version is the version of sxir that printed the text. A text of another version is read, but
	// its passes may behave differently (Warning).
	Version  string
	Pipeline string
}

// Read reads the headers of a pipeline text that Describe or DescribeGoLibrary wrote: the version of
// sxir, then one line of each word of Header, and one target, or, for a text of the Go library, whose
// pipeline starts at the library and which gives no target, one line of each of its words
// (libraryWords). A word that the scheme does not know, a word given twice, a word that the text
// does not take, a key that its word does not have and a missing key are errors. The pipeline is
// checked when the build runs, against the pipeline assembled for its options.
func Read(text string) (*Build, error) {
	d, err := pipe.Parse(text)
	if err != nil {
		return nil, err
	}
	b := &Build{Pipeline: text}
	words := headerWords()
	if d.In == library.IR.Name && !slices.ContainsFunc(d.Header, func(l pipe.Line) bool { return l.Word == "target" }) {
		b.Backend, b.Library, words = result.Go, true, libraryWords
	}
	seen := map[string]bool{}
	for _, l := range d.Header {
		if seen[l.Word] {
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
			switch l.Label {
			case result.Go.String():
				b.Backend = result.Go
			case result.TypeScript.String():
				b.Backend = result.TypeScript
			default:
				return nil, fmt.Errorf("pipeline text: %q is no target of a generation (go, ts)", l.Label)
			}
		}
		if err := Header.Read(l, &b.Gen); err != nil {
			return nil, fmt.Errorf("pipeline text: %w", err)
		}
	}
	for _, w := range append([]string{"sxir"}, words...) {
		if !seen[w] {
			return nil, fmt.Errorf("pipeline text: the header %s is missing", w)
		}
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

// Generate generates the files of the build b for g, with the libraries of this version, the
// observers of x and the writer of the debugging output out, which the debugging settings of the
// headers need when they write. The settings, the debugging settings and the pipeline of x come from b.
func (b *Build) Generate(g *ast.Grammar, x driver.Options, out io.Writer) ([]common.GeneratedFile, error) {
	if b.Library {
		return nil, fmt.Errorf("the pipeline text builds the Go library, which takes no grammar")
	}
	o := b.Gen
	o.Debug.Output = out
	x.Pipeline = b.Pipeline
	switch b.Backend {
	case result.Go:
		o.LibGo = libGo
		return GoWith(checkForGenerator, g, o, x, nil)
	case result.TypeScript:
		o.TSLib = tsLib
		return TypeScriptWith(checkForGenerator, g, o, x, nil)
	}
	return nil, fmt.Errorf("internal error: no generation for the backend %s", b.Backend)
}

// GoLibrary generates the files of the Go library of the build b, a text of the Go library, with the
// observers of x and the writer of the debugging output out, which the debugging settings of the
// headers need when they write. The settings, the debugging settings and the pipeline of x come from b.
func (b *Build) GoLibrary(x driver.Options, out io.Writer) ([]common.GeneratedFile, error) {
	if !b.Library {
		return nil, fmt.Errorf("the pipeline text builds a parser for the target %s, not the Go library", b.Backend)
	}
	o := b.Gen
	o.Debug.Output = out
	x.Pipeline = b.Pipeline
	return goLibrary(o, x)
}
