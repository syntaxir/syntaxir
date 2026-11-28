package print

import (
	"syntaxir.dev/sxir/internal/ir/code/exec/stores"
	"syntaxir.dev/sxir/internal/middleend/execlower/execdata"
)

// The tests of package gogen_test compile grammars with the engine, which imports packages that
// the generator must not depend on, so they cannot live in package print. These declarations
// expose to them the unexported internals that they check.

type (
	Dvar         = dvar
	LiveRules    = liveRules
	RuleKey      = ruleKey
	ShakeUnit    = shakeUnit
	TypesPackage = typesPackage
)

const LibImportPath = libImportPath

// ClassReject is the condition that a character is not in a class.
var ClassReject = classReject

// ExecVarsError returns the error that the check of the variables of a printed body gives, for a body
// whose text is text and whose locals dead are not declared.
func ExecVarsError(text string, dead ...string) error {
	g := &Generator{}
	res := &BodyResult{Text: text, Stores: &stores.Result{Dead: map[string]bool{}}}
	for _, v := range dead {
		res.Stores.Dead[v] = true
	}
	(&primPrinter{g: g}).execVars(&execdata.Lowered{}, res)
	return g.PrintErr
}

var (
	ClassBits          = classBits
	DeclaredNames      = declaredNames
	DefaultPackageName = defaultPackageName
	FreeIdents         = freeIdents
	KeptUnits          = keptUnits
	LoadLib            = loadLib
	QualifyLib         = qualifyLib
	RuleTableLimits    = ruleTableLimits
	ShakableVar        = shakableVar
	ShakeVars          = shakeVars
	TrimComments       = trimComments
	VarLinesExcept     = varLinesExcept
)

// ReachFrom calls reachFrom.
func (l *LiveRules) ReachFrom(roots []RuleKey) { l.reachFrom(roots) }

// MissingBodies calls missingBodies.
func (l *LiveRules) MissingBodies(table string, printed map[RuleKey]bool) []RuleKey {
	return l.missingBodies(table, printed)
}

// LoweredActionNames returns the names of the library functions and methods that only lowered action expressions call.
func LoweredActionNames() []string { return loweredActionNames() }

// CheckDiscarded is Generator.checkDiscarded of a generator that discarded the functions named.
func CheckDiscarded(discarded []string, parts ...string) error {
	return (&Generator{discarded: discarded}).checkDiscarded(parts...)
}
