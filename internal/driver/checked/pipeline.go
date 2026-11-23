package checked

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/driver/generate"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// The pipeline text of a run-time program (record 074, Headers) is that of a generation with one
// more header, program, after options: what makes the run-time program other than the options that
// every target shares, which reproduces it from the grammar alone. Its targets are the run-time
// backends, vm and closure, whose target lines set no option, since a run-time target takes none.

// ProgramLine is what the program header of a run-time pipeline text writes: which program of its
// grammar the program is (driver.Role), and the options of the check of the grammar's parser that
// the options header does not write. The parser's options and the role reproduce the program: the
// recognizer and the insertion program are derived from the parser in one place (Source.again).
type ProgramLine struct {
	Role                 driver.Role
	Recognition          check.Recognition
	DisableMemo          bool
	NoProjections        bool
	EagerAttributeCopies bool
}

// ProgramHeader is the scheme of the program header. Its test holds it, with the options header of a
// generation, to the options of the check (check.Options): each is written by exactly one of them.
var ProgramHeader = &pipe.Header[ProgramLine]{
	Words: []pipe.HeaderWord{{Word: "program", Fields: []pipe.HeaderField{
		{Field: "Role", Codec: enumCodec("role", []driver.Role{driver.Parser, driver.Recognizer, driver.Insertion, driver.InsertionRecognizer}, driver.Role.String)},
		{Field: "Recognition", Codec: enumCodec("recognition", []check.Recognition{check.BuildValues, check.Recognize, check.RecognizeWithStreamValues},
			func(r check.Recognition) string { return recognitionNames[r] })},
		{Field: "DisableMemo"}, {Field: "NoProjections"}, {Field: "EagerAttributeCopies"},
	}}},
}

// recognitionNames are the names of the values of check.Recognition in a pipeline text.
var recognitionNames = map[check.Recognition]string{
	check.BuildValues: "build-values", check.Recognize: "recognize", check.RecognizeWithStreamValues: "recognize-with-stream-values",
}

// enumCodec is the text form of an enumeration of the program header, whose values are values and
// the name of each name.
func enumCodec[E ~uint8](what string, values []E, name func(E) string) *pipe.Codec {
	return &pipe.Codec{
		Format: func(v reflect.Value) string { return name(E(v.Uint())) },
		Parse: func(s string, v reflect.Value) error {
			names := make([]string, len(values))
			for i, e := range values {
				if name(e) == s {
					v.SetUint(uint64(e))
					return nil
				}
				names[i] = name(e)
			}
			return fmt.Errorf("%q is no %s (%s)", s, what, strings.Join(names, ", "))
		},
	}
}

// runTimeBackends are the backends of a run-time pipeline text.
var runTimeBackends = []result.Backend{result.VM, result.Closure}

// Describe returns the pipeline text of the build of the program of s for the run-time backends,
// in order, under its settings and the debugging settings d: the headers sxir, options, program,
// settings, debug and one target line per backend, then the pipeline that driver.Compile runs for
// them. The settings are written as the default profile with an override for each optimization
// that they set otherwise, which gives back the same settings. Read reads the text back.
func Describe(s *Source, d common.Debug, backends ...result.Backend) (string, error) {
	if len(backends) == 0 {
		return "", fmt.Errorf("a run-time build takes at least one backend")
	}
	call := s.genOptions(d)
	targets := make([]driver.Target, len(backends))
	for i, b := range backends {
		if !slices.Contains(runTimeBackends, b) {
			return "", fmt.Errorf("the backend %s builds no run-time program (vm, closure)", b)
		}
		targets[i] = driver.Target{Backend: b, Gen: call}
	}
	desc, err := driver.Describe(s.Program(), driver.Options{Settings: s.settings, Debug: d}, targets...)
	if err != nil {
		return "", err
	}
	desc.Header = programHeaderLines(generate.HeaderLines(targets), s.programLine())
	return desc.String(), nil
}

// programHeaderLines returns the header lines of a generation, lines, with the program line l after
// the options line.
func programHeaderLines(lines []pipe.Line, l ProgramLine) []pipe.Line {
	i := slices.IndexFunc(lines, func(l pipe.Line) bool { return l.Word == "options" })
	return slices.Insert(lines, i+1, ProgramHeader.Line("program", "", l))
}

// programLine returns the program line of s: its role and the options of the grammar's parser that
// the options header does not write.
func (s *Source) programLine() ProgramLine {
	p := s.parser
	return ProgramLine{Role: s.role, Recognition: p.Recognition, DisableMemo: p.DisableMemo, NoProjections: p.NoProjections,
		EagerAttributeCopies: p.EagerAttributeCopies}
}

// genOptions returns the options of the whole call of a run-time build of s under the debugging
// settings d, as the headers of a generation write them: the options of the grammar's parser that
// every target shares, the settings of s as the default profile with its overrides, and d.
func (s *Source) genOptions(d common.Debug) common.GenOptions {
	opt := map[string]int{}
	for _, n := range pass.Names() {
		if f, _ := pass.Lookup(n); s.settings.Value(n) != f.Default {
			opt[n] = s.settings.Value(n)
		}
	}
	return common.GenOptions{SourceInfo: s.parser.SourceInfo, DefaultValue: s.parser.DefaultValue,
		LeftRecursionBindings: s.parser.LeftRecursionBindings, Optimize: driver.Profiles()[0], Opt: opt, Debug: d}
}

// Build is a run-time build that a pipeline text describes: the program of the grammar, by the
// options of its parser and its role, its settings and debugging settings, and its targets, whose
// pipeline the text gives.
type Build struct {
	// Parser are the options that the grammar's parser is checked with, and Role which program of
	// the grammar the build compiles.
	Parser check.Options
	Role   driver.Role
	// Settings are the settings of the build, and Debug its debugging settings without an Output.
	Settings pass.Settings
	Debug    common.Debug
	// Backends are the run-time backends of the targets, in the order of their lines.
	Backends []result.Backend
	// Version is the version of sxir that printed the text (generate.Build.Version).
	Version  string
	Pipeline string
}

// Read reads the headers of the pipeline text of a run-time build that Describe wrote: the version
// of sxir, then one line of each of options, program, settings and debug, and one target line or
// more of the backends vm and closure, which set no option. A word that a run-time text does not
// take, a word other than target given twice, a missing word, a key that its word does not have and
// a missing key are errors. The pipeline is checked when the build runs, against the pipeline
// assembled for its options.
func Read(text string) (*Build, error) {
	d, err := pipe.Parse(text)
	if err != nil {
		return nil, err
	}
	b := &Build{Pipeline: text}
	var call common.GenOptions
	var prog ProgramLine
	seen := map[string]bool{}
	for _, l := range d.Header {
		if seen[l.Word] && l.Word != "target" {
			return nil, fmt.Errorf("pipeline text: the header %s is given twice", l.Word)
		}
		seen[l.Word] = true
		switch l.Word {
		case "sxir":
			if l.Label == "" || len(l.Fields) > 0 {
				return nil, fmt.Errorf("pipeline text: want sxir <version>")
			}
			b.Version = l.Label
		case "program":
			if err := ProgramHeader.Read(l, &prog); err != nil {
				return nil, fmt.Errorf("pipeline text: %w", err)
			}
		case "options", "settings", "debug":
			if err := generate.Header.Read(l, &call); err != nil {
				return nil, fmt.Errorf("pipeline text: %w", err)
			}
		case "target":
			i := slices.IndexFunc(runTimeBackends, func(r result.Backend) bool { return r.String() == l.Label })
			if i < 0 {
				return nil, fmt.Errorf("pipeline text: %q is no target of a run-time build (vm, closure)", l.Label)
			}
			var t common.GenOptions
			if err := generate.Header.Read(l, &t); err != nil {
				return nil, fmt.Errorf("pipeline text: %w", err)
			}
			if !reflect.DeepEqual(t, common.GenOptions{}) {
				return nil, fmt.Errorf("pipeline text: the target %s sets an option of a generating backend", l.Label)
			}
			b.Backends = append(b.Backends, runTimeBackends[i])
		default:
			return nil, fmt.Errorf("pipeline text: a run-time build has no header %s", l.Word)
		}
	}
	for _, w := range []string{"sxir", "options", "program", "settings", "debug", "target"} {
		if !seen[w] {
			return nil, fmt.Errorf("pipeline text: the header %s is missing", w)
		}
	}
	if call.Start != "" {
		return nil, fmt.Errorf("pipeline text: a run-time program has no start rule of its own, not %q", call.Start)
	}
	if b.Settings, err = generate.Settings(common.GenOptions{Optimize: call.Optimize, Opt: call.Opt, NoOpt: call.NoOpt}); err != nil {
		return nil, fmt.Errorf("pipeline text: %w", err)
	}
	b.Debug = call.Debug
	b.Role = prog.Role
	b.Parser = check.Options{SourceInfo: call.SourceInfo, DefaultValue: call.DefaultValue, LeftRecursionBindings: call.LeftRecursionBindings,
		Recognition: prog.Recognition, DisableMemo: prog.DisableMemo, NoProjections: prog.NoProjections, EagerAttributeCopies: prog.EagerAttributeCopies}
	return b, nil
}

// Warning returns a warning for a text that another version of sxir printed, whose passes may
// behave differently, or "".
func (b *Build) Warning() string {
	return (&generate.Build{Version: b.Version}).Warning()
}

// Source returns the program of the build for the grammar g: its parser checked with the options of
// the build, under its settings, or the program of its role derived from it, as the compiler side
// of a run-time program derives it.
func (b *Build) Source(g *ast.Grammar) (*Source, error) {
	s, err := Check(g, Options{Compile: b.Parser, Settings: b.Settings}, nil, nil)
	if err != nil {
		return nil, err
	}
	var derived any
	switch b.Role {
	case driver.Parser:
		return s, nil
	case driver.Recognizer:
		derived, err = s.Recognizer()
	case driver.Insertion:
		derived, err = s.Insertion()
	case driver.InsertionRecognizer:
		if derived, err = s.Insertion(); err == nil {
			derived, err = derived.(*Source).Recognizer()
		}
	default:
		return nil, fmt.Errorf("the role %s is no program of a grammar", b.Role)
	}
	if err != nil {
		return nil, err
	}
	return derived.(*Source), nil
}

// Compile builds the targets of the build b for the grammar g, in order, with the pipeline of the
// text, the observers of x and the writer of the debugging output out, which the debugging settings
// of the headers need when they write. The settings, the debugging settings and the pipeline of x
// come from b.
func (b *Build) Compile(g *ast.Grammar, x driver.Options, out io.Writer) ([]driver.Output, error) {
	s, err := b.Source(g)
	if err != nil {
		return nil, err
	}
	x.Settings, x.Debug, x.Pipeline = b.Settings, b.Debug, b.Pipeline
	x.Debug.Output = out
	targets := make([]driver.Target, len(b.Backends))
	for i, backend := range b.Backends {
		targets[i] = driver.Target{Backend: backend}
	}
	return driver.Compile(context.Background(), s.Program(), x, targets...)
}
