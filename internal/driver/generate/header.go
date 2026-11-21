package generate

import (
	"fmt"
	"reflect"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/pass/pipe"
)

// Header is how the header lines of a pipeline text carry the generation options (record 074,
// Headers), so that a build can be repeated from its text and the grammar alone. Each option has one
// scope and each scope one header word:
//   - options: the options of the program that every target shares, which the check of the grammar
//     reads (common.CompileOptions) and the start rule;
//   - settings: the optimization profile and its overrides, which the call holds;
//   - debug: the debugging settings (common.Debug), which apply to the whole call: the manager's
//     policies (verify-each, opt-bisect-limit) and the observation flags;
//   - target <backend>: every other option of that target, in declaration order, one line per
//     target.
//
// The writer of the dumps and the library file systems are not written: the reader supplies them.
// The test of the scheme holds it to common.GenOptions and the structs inside it, so a new option
// cannot be forgotten and cannot be given two scopes.
var Header = &pipe.Header[common.GenOptions]{
	Words: []pipe.HeaderWord{
		{Word: "options", Fields: []pipe.HeaderField{
			{Field: "Start"}, {Field: "SourceInfo"}, {Field: "DefaultValue"}, {Field: "LeftRecursionBindings"},
		}},
		{Word: "settings", Fields: []pipe.HeaderField{
			{Field: "Optimize", Key: "profile"}, {Field: "Opt"}, {Field: "NoOpt"},
		}},
		{Word: "debug", Fields: []pipe.HeaderField{
			{Field: "Debug.VerifyEach"}, {Field: "Debug.OptBisectLimit"}, {Field: "Debug.PrintAfter"},
			{Field: "Debug.PrintChanged"}, {Field: "Debug.Stats"}, {Field: "Debug.TimePasses"}, {Field: "Debug.PrintBefore"},
		}},
		{Word: "target", Fields: []pipe.HeaderField{
			{Field: "TypedOnly"}, {Field: "RecognizeOnly"}, {Field: "LimitControls", Key: "limits"},
			{Field: "LogicalUnits"}, {Field: "LogicalVariant"}, {Field: "Expectations"}, {Field: "Observability"}, {Field: "Document"}, {Field: "Stream"},
			{Field: "Module"}, {Field: "Package"}, {Field: "Lib", Codec: libCodec}, {Field: "LibModule"},
			{Field: "MaxDepth"}, {Field: "Types"}, {Field: "Recognize"}, {Field: "OmitPackageDoc"},
			{Field: "ConvertTypes"}, {Field: "KeepNodes"}, {Field: "KeepUnreachable"}, {Field: "KeepComments"},
			{Field: "FeatureWitness"}, {Field: "DisableKeepNodes"},
		}},
	},
	Omit: []string{"LibGo", "TSLib", "Debug.Output"},
}

// libModes are the library modes, whose names (common.LibMode.String) are their text form.
var libModes = []common.LibMode{common.LibEmbed, common.LibImport, common.LibOnly}

var libCodec = &pipe.Codec{
	Format: func(v reflect.Value) string { return common.LibMode(v.Int()).String() },
	Parse: func(s string, v reflect.Value) error {
		for _, m := range libModes {
			if m.String() == s {
				v.SetInt(int64(m))
				return nil
			}
		}
		return fmt.Errorf("%q is no library mode (embed, import, only)", s)
	},
}
