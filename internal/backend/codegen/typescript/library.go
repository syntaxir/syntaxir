package typescript

import (
	"fmt"
	"sync"

	"syntaxir.dev/sxir/internal/backend/codegen"
	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/typescript/print"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/ir/code/exec/compose"
	"syntaxir.dev/sxir/internal/ir/code/exec/library"
	"syntaxir.dev/sxir/internal/middleend/execlower/execdata"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// The TypeScript library of a parser is the library's source with the compositions that it replaces
// specialized for the features of the parser (print.LibraryRequests). The step ts-library! sets the
// library of the tables (execdata.Tables.Library) to the compositions with those requests, and the pass
// specialize, within the lens ts-library, specializes them:
//
//	ts-library!
//	within ts-library {
//	  specialize!
//	}
//	print-ts!
//
// The library alone (common.LibOnly) is specialized for every feature (LibrarySource) and printed by
// print-ts-lib!:
//
//	pipeline library -> output {
//	  specialize!
//	  print-ts-lib!
//	}

// compositions returns the compositions module (compose.Build), built once: no pass changes it, so
// every library unit shares it, and the specializations of a module are memoized by its identity.
var compositions = sync.OnceValue(compose.Build)

// LibraryLens is the lens ts-library: the library of the TypeScript tables (execdata.Tables.Library).
var LibraryLens = pipe.RegisterLens(execdata.TablesIR, execdata.TablesIR, library.IR, library.IR,
	&pipe.Lens[*execdata.Tables, *execdata.Tables, *library.Library, *library.Library]{
		Name: "ts-library",
		Get:  func(t *execdata.Tables) *library.Library { return t.Library },
		Put: func(t *execdata.Tables, l *library.Library) (*execdata.Tables, error) {
			out := *t
			out.Library = l
			return &out, nil
		},
	})

// Library is the step ts-library: it sets the library of the tables to the compositions with the
// requests of the TypeScript library for the features of the parser of the target, as the plan's
// used-features analysis reports them (common.ParserFeatures); with used-features off, for every
// feature. The requests of the templates that the parser's code prints (print.TemplateRequests)
// follow them. The step decides the features of the parser: each feature that the analysis leaves out
// is a decision of the pattern used-features, and the library records the features it decided
// (library.Library.Features), for which print-ts prints the parser.
var Library = pipe.New(pipe.Def[*execdata.Tables, *execdata.Tables]{
	Decl: pipe.Decl{Name: "ts-library", Required: true,
		Obligation: "The library of the tables requests the TypeScript library's compositions for the features of the parser.",
		Features:   []pipe.FeatureID{codegen.TargetFeature.ID()},
		Params:     []string{codegen.UsedFeatures}, Patterns: []string{codegen.UsedFeatures}},
	In: execdata.TablesIR, Out: execdata.TablesIR,
	Run: func(c *pipe.Ctx[*execdata.Tables], t *execdata.Tables) (*execdata.Tables, pass.PreservedSet, error) {
		target, ok := pipe.Feature(c, codegen.TargetFeature).(*Target)
		if !ok {
			return nil, pass.PreserveNone(), fmt.Errorf("internal error: ts-library runs for the target %s", pipe.Feature(c, codegen.TargetFeature).Backend())
		}
		p, err := target.parser()
		if err != nil {
			return nil, pass.PreserveNone(), err
		}
		fs, err := common.ParserFeatures(p.Tables(), target.Opts, common.Params{UsedFeatures: c.Value(codegen.UsedFeatures) != 0, Allow: c.Allow}, t.Plan)
		if err != nil {
			return nil, pass.PreserveNone(), err
		}
		rs, err := print.LibraryRequests(fs)
		if err != nil {
			return nil, pass.PreserveNone(), err
		}
		rs = append(rs, print.TemplateRequests(!target.Opts.NoLimitControls, fs.Has(common.FeatureMissing))...)
		out := *t
		out.Library = &library.Library{Module: compositions(), Requests: rs, Features: uint64(fs)}
		return &out, pass.PreserveNone(), nil
	},
})

// LibrarySource returns the source unit of the TypeScript library alone: the compositions with the
// requests of the library for every feature.
func LibrarySource() (*library.Library, error) {
	rs, err := print.LibraryRequests(common.FeatureSet(common.AllFeatures))
	if err != nil {
		return nil, err
	}
	return &library.Library{Module: compositions(), Requests: rs, Features: common.AllFeatures}, nil
}

// LibraryPrinting is the step print-ts-lib: it prints the file of the TypeScript library alone
// (common.LibOnly) from the library's source in the options of the target of the pipeline
// (codegen.TargetFeature) and the specialized compositions of the library.
var LibraryPrinting = pipe.New(pipe.Def[*library.Library, result.Output]{
	Decl: pipe.Decl{Name: "print-ts-lib", Required: true, Obligation: "The file is the TypeScript library of the specialized compositions.",
		Features: []pipe.FeatureID{codegen.TargetFeature.ID()}},
	In: library.IR, Out: result.IR,
	Run: func(c *pipe.Ctx[*library.Library], l *library.Library) (result.Output, pass.PreservedSet, error) {
		target, ok := pipe.Feature(c, codegen.TargetFeature).(*Target)
		if !ok {
			return nil, pass.PreserveNone(), fmt.Errorf("internal error: print-ts-lib runs for the target %s", pipe.Feature(c, codegen.TargetFeature).Backend())
		}
		files, err := print.LibFiles(target.Opts.TSLib, l)
		if err != nil {
			return nil, pass.PreserveNone(), err
		}
		return &result.Files{Files: files}, pass.PreserveNone(), nil
	},
})
