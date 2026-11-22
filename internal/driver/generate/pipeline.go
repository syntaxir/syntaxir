package generate

import (
	"fmt"
	"io"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/driver/result"
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
// whose pipeline replaces the assembled one (driver.Options.Pipeline).
type Build struct {
	Backend result.Backend
	// Gen holds the options of the headers. The fields that no header writes (Header.Omit) are
	// zero: the libraries are those of this version, which Generate supplies, and the writer of the
	// debugging output is the caller's.
	Gen common.GenOptions
	// Version is the version of sxir that printed the text. A text of another version is read, but
	// its passes may behave differently (Warning).
	Version  string
	Pipeline string
}

// Read reads the headers of a pipeline text that Describe wrote: the version of sxir, one line of
// each word of Header, and one target. A word that the scheme does not know, a word given twice, a
// key that its word does not have and a missing key are errors. The pipeline is checked when the
// build runs, against the pipeline assembled for its options.
func Read(text string) (*Build, error) {
	d, err := pipe.Parse(text)
	if err != nil {
		return nil, err
	}
	b := &Build{Pipeline: text}
	seen := map[string]bool{}
	for _, l := range d.Header {
		if seen[l.Word] {
			return nil, fmt.Errorf("pipeline text: the header %s is given twice", l.Word)
		}
		seen[l.Word] = true
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
	for _, w := range append([]string{"sxir"}, headerWords()...) {
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
