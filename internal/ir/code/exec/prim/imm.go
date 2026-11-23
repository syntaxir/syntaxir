package prim

import (
	"fmt"
	"strconv"
	"strings"
)

// Immediate operands of the primitives, which their Flow operations have too.

// Silence is the immediate of silent: the change Delta of the silent depth, and for a
// lookahead (Site is not 0) its site and, at its end, whether its body matched.
type Silence struct {
	Delta int
	Site  int
	Ok    bool
}

// Site is the immediate of the operations of skip-run and scan sites: the site's number.
type Site int

// RunWalk is the immediate of runBegin: the run site, whether its iterations have values
// (skipValue), which skipKids gathers in a list, and whether its records are keyed by the
// values of the variables of the site's run key (program.RunKey), which the printed parser holds; a
// site free of the environment (core.Repeat.RunEnvFree) is keyed by the position alone.
type RunWalk struct {
	Site          int
	Build, KeyEnv bool
}

// Resume is the immediate of resumeStart: the repetition that a Document parse resumes
// (core.Repeat.Resume), its bounds (Max < 0 is unbounded), and whether the values of its elements
// contain no position values, so that reused elements may move with an edit (core.Repeat.Shiftable).
type Resume struct {
	Site, Min, Max int
	Shiftable      bool
}

// Slot is the immediate of setCapture and frameVal: a capture slot of a frame.
type Slot int

// Expectation is the immediate of expect: the expectation, and whether it is the message of a
// failed @error.
type Expectation struct {
	Exp     ExpID
	Message bool
}

// SkippedCalls is the immediate of skippedCalls: the rules that the alternative a guard skips
// calls before it fails at its first terminal, outermost first (core.GuardCalls), and the
// expectation of that terminal, the guard's.
type SkippedCalls struct {
	Rules []int
	Exp   ExpID
}

// Spread is the immediate of append: whether the children of a tuple node are appended
// instead of the node.
type Spread bool

// RuleCall is the immediate of callRule: the rule and the Operators Rule level it is called
// from.
type RuleCall struct{ Rule, Level int }

// Outline is the immediate of callOutline: the outlined call, the slots of the capture scope of the
// current frame, and whether the call builds the node's value. Call numbers the distinct outlined
// calls (a node under a frame with these slots, building or not) that the lowering of the module
// makes, from 0 in the order it makes them; the plan of the module's table finds the function that
// runs it by the number.
type Outline struct {
	Call  int
	Slots []string
	Build bool
}

// ObservedSkip is the immediate of observeSkip: the rule call that the alternative a guard skips
// begins with, as the grammar writes it: the rule's name and the level of the Operators Rule that the
// reference names, or "" if it names none.
type ObservedSkip struct {
	Rule  string
	Level string
}

// Chars is the immediate of literalText, str and setWhere: a string.
type Chars string

// List is the immediate of newNode and newBranchNode: whether the node is a list.
type List bool

// Room is the immediate of newVals: the room of the new list.
type Room int

// Attach is the immediate of attachCaptures and attachFrame: the slot names of the scope,
// and whether the captures are the values of a frame (attachFrame) instead of variables.
type Attach struct {
	Names *Names
	Frame bool
}

// StateMark is the immediate of markState: which parts of the state besides the position the
// code between the mark and its reset may change.
type StateMark struct{ Env, Trail, Rec bool }

// Conversion is the immediate of convertResult: the rule, and whether the fresh node of the
// body is passed to convert in place.
type Conversion struct {
	Rule  int
	Owned bool
}

// Capture names the captures of an action expression: the slot names of the scope whose variables
// a call passes, or Frame when the expression reads the current frame and the call passes none.
type Capture struct {
	Names []string
	Frame bool
}

// Predicate is the immediate of pred: the number of the predicate's action site in the Core module
// of the function's table (core.ActionSites).
type Predicate struct {
	Action int
	Capture
}

// Binding is the immediate of bind: the variable the binding binds and the number of the binding's
// action site in the Core module of the function's table (core.ActionSites).
type Binding struct {
	Var    string
	Action int
	Capture
}

// Action is the immediate of eval: the number of the action site of the function's rule in the Core
// module of the function's table (core.ActionSites).
type Action struct {
	Action int
	Capture
}

// Names are the slot names of a capture scope, which printers declare once: the immediate of
// declareNames and newFrame.
type Names struct {
	List []string
	// Declared reports that a declareNames call declares them; otherwise their first use does.
	Declared bool
}

// ImmString returns the immediate imm of a primitive, or of its Flow operation, as listings print it
// (code.Op.FormatImm): its static data, without the addresses of the Core nodes it points to, so that
// a listing of the same function is the same in every run.
func ImmString(imm any) string {
	switch x := imm.(type) {
	case Silence:
		s := fmt.Sprintf("%+d", x.Delta)
		if x.Site != 0 {
			s += fmt.Sprintf(" site %d", x.Site)
		}
		if x.Ok {
			s += " ok"
		}
		return s
	case Site:
		return fmt.Sprintf("site %d", int(x))
	case Resume:
		s := fmt.Sprintf("resume %d min %d max %d", x.Site, x.Min, x.Max)
		if x.Shiftable {
			s += " shiftable"
		}
		return s
	case RunWalk:
		return fmt.Sprintf("site %d build=%t key-env=%t", x.Site, x.Build, x.KeyEnv)
	case Slot:
		return fmt.Sprintf("slot %d", int(x))
	case Expectation:
		if x.Message {
			return fmt.Sprintf("exp %d message", x.Exp)
		}
		return fmt.Sprintf("exp %d", x.Exp)
	case SkippedCalls:
		return fmt.Sprintf("rules %v exp %d", x.Rules, x.Exp)
	case Spread:
		if x {
			return "spread"
		}
		return "one"
	case RuleCall:
		return fmt.Sprintf("rule %d level %d", x.Rule, x.Level)
	case Outline:
		s := fmt.Sprintf("call %d slots %v", x.Call, x.Slots)
		if x.Build {
			s += " build"
		}
		return s
	case ObservedSkip:
		if x.Level != "" {
			return fmt.Sprintf("%s.%s", x.Rule, x.Level)
		}
		return x.Rule
	case Chars:
		return strconv.Quote(string(x))
	case List:
		if x {
			return "list"
		}
		return "tuple"
	case Room:
		return fmt.Sprintf("room %d", int(x))
	case Attach:
		s := names(x.Names)
		if x.Frame {
			s += " frame"
		}
		return s
	case StateMark:
		var parts []string
		for _, p := range []struct {
			on   bool
			name string
		}{{x.Env, "env"}, {x.Trail, "trail"}, {x.Rec, "rec"}} {
			if p.on {
				parts = append(parts, p.name)
			}
		}
		return "state " + strings.Join(append([]string{"pos"}, parts...), " ")
	case Conversion:
		if x.Owned {
			return fmt.Sprintf("rule %d owned", x.Rule)
		}
		return fmt.Sprintf("rule %d", x.Rule)
	case Predicate:
		return fmt.Sprintf("action %d", x.Action) + capture(x.Capture)
	case Binding:
		return fmt.Sprintf("%s = action %d", x.Var, x.Action) + capture(x.Capture)
	case Action:
		return fmt.Sprintf("action %d", x.Action) + capture(x.Capture)
	case *Names:
		s := names(x)
		if x.Declared {
			s += " declared"
		}
		return s
	case ExpID:
		return fmt.Sprintf("exp %d", x)
	case Lit:
		s := strconv.Quote(x.Text)
		if x.Fold {
			s += "i"
		}
		return s
	case ClassID:
		return fmt.Sprintf("class %d", x)
	case Guard:
		return fmt.Sprintf("guard exp %d depth %d", x.Exp, x.Depth)
	case Anchor:
		return [...]string{"begin input", "end input", "begin line", "end line"}[x]
	case Terminal:
		return "terminal"
	case ClassRun:
		return fmt.Sprintf("%d classes", len(x))
	case Shape:
		return fmt.Sprintf("%+v", x)
	}
	return fmt.Sprintf("%T", imm)
}

func names(n *Names) string {
	if n == nil {
		return "[]"
	}
	return fmt.Sprint(n.List)
}

// capture prints the slots of a capture, and where it reads them from the current frame.
func capture(c Capture) string {
	s := fmt.Sprintf(" slots %v", c.Names)
	if c.Frame {
		s += " frame"
	}
	return s
}
