package print

import (
	"fmt"
	"go/token"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
)

// Validate checks the options of a Go parser that do not depend on the grammar and returns them
// completed: the package name derived from the module path when none is given, Recognize for a
// recognize-only parser, and KeepUnreachable under a bisection limit, so that the numbered runs of
// the optional passes are those of every rule whatever the plan leaves out.
func Validate(opts common.GenOptions) (common.GenOptions, error) {
	if opts.Debug.OptBisectLimit != nil {
		opts.KeepUnreachable = true
	}
	if opts.Module == "" && (opts.Lib == common.LibEmbed || opts.Package == "") {
		return opts, fmt.Errorf("module path is required")
	}
	derived := opts.Package == ""
	if derived {
		opts.Package = defaultPackageName(opts.Module)
	}
	if !token.IsIdentifier(opts.Package) || opts.Package == "_" {
		if derived {
			return opts, fmt.Errorf("the module path %q gives the package name %q, which is not a valid Go identifier; choose the package name with -pkg (PackageName)", opts.Module, opts.Package)
		}
		return opts, fmt.Errorf("invalid package name %q", opts.Package)
	}
	if opts.RecognizeOnly {
		switch {
		case opts.Types || opts.TypedOnly || opts.Document || opts.Stream:
			return opts, fmt.Errorf("recognize-only output generates only Recognize: it cannot have typed output, typed-only output, a Document or a stream")
		}
		opts.Recognize = true
	}
	if opts.TypedOnly {
		switch {
		case !opts.Types:
			return opts, fmt.Errorf("typed-only output needs typed output (-types)")
		case opts.Document || opts.Stream:
			return opts, fmt.Errorf("typed-only output cannot have a Document or a stream, which use the untyped Node (Recognize is allowed)")
		}
	}
	if opts.Types && opts.Module == "" {
		return opts, fmt.Errorf("module path is required for typed output: the parser imports the package module/types")
	}
	if err := opts.ValidateMaxDepth(false); err != nil {
		return opts, err
	}
	if err := opts.ValidateDefaultValue(); err != nil {
		return opts, err
	}
	return opts, nil
}
