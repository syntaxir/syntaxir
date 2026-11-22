package state_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/runtime/engine"
	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/state"
	"syntaxir.dev/sxir/internal/testing/enginetest"
	"syntaxir.dev/sxir/internal/testing/statetest"
	"syntaxir.dev/sxir/pkg/runtime"
	sxirinput "syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
)

func requireInsertions(t *testing.T, prog *engine.Program, input string, o engine.ParseOptions, count int) (*value.Node, value.SyntaxErrors) {
	t.Helper()
	n, err := prog.ParseWith("main", input, o)
	var recovered value.SyntaxErrors
	if !errors.As(err, &recovered) || len(recovered) != count {
		t.Fatalf("input %q: node=%v, recoveries=%#v, error=%v; want %d insertions", input, n, recovered, err, count)
	}
	if o.Recognize {
		if n != nil {
			t.Fatal("recognition returned a tree")
		}
	} else if n == nil {
		t.Fatal("recovered parse returned no tree")
	}
	return n, recovered
}

func TestMissingTokensLiteralAndUnits(t *testing.T) {
	t.Parallel()
	for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
		for _, controlled := range []bool{false, true} {
			if controlled && !state.BudgetControlsEnabled {
				continue
			}
			o := engine.ParseOptions{Expectations: true, Unit: unit, MissingTokens: 8}
			if controlled {
				o.Context = context.Background()
				o.Limits.Compute = 1 << 20
			}
			prog := enginetest.MustCompile(t, `rule main = "é" ";" #missing $$`)
			n, errs := requireInsertions(t, prog, "é", o, 1)
			insertions := statetest.MissingNodes(t, n)
			pos := int32(1)
			if unit == sxirinput.Bytes {
				pos = 2
			}
			if len(insertions) != 1 {
				t.Fatalf("missing nodes: %v", insertions)
			}
			m := insertions[0]
			if nodes.D(m).Start != pos || nodes.D(m).End != pos || nodes.D(m).Text != "" || errs[0].Pos != int(pos) || errs[0].Col != int(pos)+1 {
				t.Fatal("fabricated source range/text or wrong unit", m, errs)
			}
			if expected, _ := nodes.D(m).Fields.Get("expected"); expected != ";" {
				t.Fatal(expected)
			}
			if message, _ := nodes.D(m).Fields.Get("message"); message != errs[0].Error() {
				t.Fatal(message, errs[0])
			}
			if _, err := prog.ParseWith("main", "é;", o); err != nil {
				t.Fatal("ordinary literal changed", err)
			}
			o.Recognize = true
			_, recognized := requireInsertions(t, prog, "é", o, 1)
			if !reflect.DeepEqual(errs, recognized) {
				t.Fatal("recognition diagnostic differs", errs, recognized)
			}
			// Strict mode and explicit zero retain the original acceptance set.
			o.Recognize, o.MissingTokens = false, 0
			if n, err := prog.ParseWith("main", "é", o); n != nil || err == nil {
				t.Fatal("strict mode inserted", n, err)
			}
		}
	}
	partial := enginetest.MustCompile(t, `rule main = "ab" #missing "a" $$`)
	n, _ := requireInsertions(t, partial, "a", engine.ParseOptions{Expectations: true, MissingTokens: 2}, 1)
	if m := statetest.MissingNodes(t, n)[0]; nodes.D(m).Start != 0 || nodes.D(m).End != 0 {
		t.Fatal("partial prefix was consumed", m)
	}
	invalid := enginetest.MustCompile(t, `rule main = . ";" #missing $$`)
	for _, unit := range []sxirinput.PositionUnit{sxirinput.CodePoints, sxirinput.Bytes} {
		n, errs := requireInsertions(t, invalid, "\xff", engine.ParseOptions{Expectations: true, Unit: unit, MissingTokens: 2}, 1)
		if nodes.D(statetest.MissingNodes(t, n)[0]).Start != 1 || errs[0].Pos != 1 {
			t.Fatal("invalid UTF-8 position")
		}
	}
}

func TestMissingTokensRollbackBudgetAndCut(t *testing.T) {
	t.Parallel()
	prog := enginetest.MustCompile(t, `rule main = (token "x" / token "y") $$
rule token = ";" #missing`)
	for _, controlled := range []bool{false, true} {
		if controlled && !state.BudgetControlsEnabled {
			continue
		}
		o := engine.ParseOptions{Expectations: true, MissingTokens: 2}
		if controlled {
			o.Context = context.Background()
		}
		n, _ := requireInsertions(t, prog, "y", o, 1)
		if len(statetest.MissingNodes(t, n)) != 1 {
			t.Fatal("abandoned insertion leaked")
		}
		o.MissingTokens = 1
		n, err := prog.ParseWith("main", "y", o)
		var limit *runtime.MissingTokenLimitError
		if n != nil || !errors.As(err, &limit) || limit.Limit != 1 || limit.Position != 0 {
			t.Fatal("rollback refunded budget or reused a completed insertion", n, err)
		}
		if _, err := prog.ParseWith("main", ";y", engine.ParseOptions{Expectations: true}); err != nil {
			t.Fatal("retry inherited insertion state", err)
		}
	}
	captures := enginetest.MustCompile(t, `rule main = (x:(";" #missing) "a" / ";" #missing "b") $$`)
	n, _ := requireInsertions(t, captures, "b", engine.ParseOptions{Expectations: true, MissingTokens: 2}, 1)
	if _, ok := nodes.D(n).Fields.Get("x"); ok {
		t.Fatal("abandoned capture survived")
	}
	cut := enginetest.MustCompile(t, `rule main = (";" #missing #cut "x" / "y") $$`)
	if n, err := cut.ParseWith("main", "y", engine.ParseOptions{Expectations: true, MissingTokens: 2}); n != nil || err == nil {
		t.Fatal("insertion changed cut scope", n, err)
	}
	chain := enginetest.MustCompile(t, `rule main = ";" #missing ")" #missing $$`)
	_, errs := requireInsertions(t, chain, "", engine.ParseOptions{Expectations: true, MissingTokens: 2}, 2)
	if errs[0].Expected[0] != `";"` || errs[1].Expected[0] != `")"` {
		t.Fatal("diagnostic encounter order", errs)
	}
	// A whole-input failure exposes only its final ordinary syntax error.
	if n, err := chain.ParseWith("main", "x", engine.ParseOptions{Expectations: true, MissingTokens: 2}); n != nil {
		t.Fatal(n, err)
	} else {
		var ordinary *value.SyntaxError
		if !errors.As(err, &ordinary) {
			t.Fatal("recovery batch escaped a failed parse", err)
		}
	}
}

func TestMissingTokensLookaheadRepetitionsAndTwins(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`rule main = !token token $$
rule token = ";" #missing`,
		`rule main = (&token "x" / token) $$
rule token = ";" #missing`,
		`rule main = (token &token / token) $$
rule token = ";" #missing`,
	} {
		prog := enginetest.MustCompile(t, source)
		requireInsertions(t, prog, "", engine.ParseOptions{Expectations: true, MissingTokens: 8}, 1)
	}
	for _, expression := range []string{`(";" #missing)*`, `(";" #missing)+`, `((";" #missing)*)*`} {
		prog := enginetest.MustCompile(t, `rule main = `+expression+` $$`)
		n, _ := requireInsertions(t, prog, "", engine.ParseOptions{Expectations: true, MissingTokens: 4}, 1)
		if len(statetest.MissingNodes(t, n)) != 1 {
			t.Fatal("zero-progress repetition did not stop once")
		}
	}
	minimum := enginetest.MustCompile(t, `rule main = (";" #missing) #repeat(2) $$`)
	if n, err := minimum.ParseWith("main", "", engine.ParseOptions{Expectations: true, MissingTokens: 4}); n != nil || err == nil {
		t.Fatal("nullable insertion met a two-iteration minimum", n, err)
	}
	// Atomic use invokes token's value-free twin. It must share the same site
	// identity as the later value-producing call at the same position.
	twins := enginetest.MustCompile(t, `rule main = token #atomic token $$
rule token = ";" #missing`)
	if n, err := twins.ParseWith("main", "", engine.ParseOptions{Expectations: true, MissingTokens: 4}); n != nil || err == nil {
		t.Fatal("value-free twin supplied a second insertion site", n, err)
	}
}

func TestMissingTokensLeftRecursion(t *testing.T) {
	t.Parallel()
	for _, controlled := range []bool{false, true} {
		if controlled && !state.BudgetControlsEnabled {
			continue
		}
		o := engine.ParseOptions{Expectations: true, MissingTokens: 64}
		if controlled {
			o.Context = context.Background()
			o.Limits.Compute = 1 << 20
		}
		for _, recognize := range []bool{false, true} {
			o.Recognize = recognize
			for _, source := range []string{
				`rule main = e $$
rule e = e "x" / ";" #missing`,
				`rule main = e $$
rule e = e !e "x" / ";" #missing`,
				`rule main = e $$
rule e = other "x" / ";" #missing
rule other = e`,
			} {
				prog := enginetest.MustCompile(t, source)
				for _, input := range []string{"", "x", "xx"} {
					n, _ := requireInsertions(t, prog, input, o, 1)
					if !recognize && len(statetest.MissingNodes(t, n)) != 1 {
						t.Fatal("LR seed diagnostics/value mismatch")
					}
				}
			}
			// A successful seed can only grow by consuming input. An extra
			// Missing value at the same end position does not win a growth tie.
			stable := enginetest.MustCompile(t, `rule main = e $$
rule e = e ";" #missing / "x"`)
			n, err := stable.ParseWith("main", "x", o)
			if err != nil || len(statetest.MissingNodes(t, n)) != 0 {
				t.Fatal("zero-width LR growth won", n, err)
			}
		}
	}
}

func TestMissingTokensOperatorsCandidates(t *testing.T) {
	t.Parallel()
	infix := enginetest.MustCompile(t, `rule main = e $$
rule e = #operators {
    operand "a"
    level { infix left "+" #missing }
}`)
	n, _ := requireInsertions(t, infix, "aa", engine.ParseOptions{Expectations: true, MissingTokens: 64}, 1)
	if len(statetest.MissingNodes(t, n)) != 1 {
		t.Fatal("discarded operator candidate leaked")
	}
	postfix := enginetest.MustCompile(t, `rule main = e $$
rule e = #operators {
    operand "a"
    level { postfix "!" #missing }
}`)
	if n, err := postfix.ParseWith("main", "a", engine.ParseOptions{Expectations: true, MissingTokens: 8}); err != nil || len(statetest.MissingNodes(t, n)) != 0 {
		t.Fatal("empty postfix was applied", n, err)
	}
	nullable := enginetest.MustCompile(t, `rule main = e $$
rule e = #operators {
    operand ""
    level { infix left "+" #missing }
}`)
	requireInsertions(t, nullable, "", engine.ParseOptions{Expectations: true, MissingTokens: 8}, 1)
}

func TestMissingTokensLabelsAndConsumingRecovery(t *testing.T) {
	t.Parallel()
	for _, expression := range []string{
		`";" @error("semicolon") #missing`,
		`";" #missing @error("semicolon")`,
		`(";" #missing @error("inner")) @error("semicolon")`,
		`(";" #missing) #recover(skip="!") @error("semicolon")`,
	} {
		prog := enginetest.MustCompile(t, `rule main = `+expression+` $$`)
		n, errs := requireInsertions(t, prog, "", engine.ParseOptions{Expectations: true, MissingTokens: 4}, 1)
		if errs[0].Message() != "semicolon" || len(statetest.MissingNodes(t, n)) != 1 {
			t.Fatal("outer label or recovery composition", n, errs)
		}
	}
	ordinary := enginetest.MustCompile(t, `rule main = (";" #recover(skip="!")) @error("outer") $$`)
	_, err := ordinary.ParseWith("main", "!", engine.ParseOptions{Expectations: true, MissingTokens: 4})
	var errs value.SyntaxErrors
	if !errors.As(err, &errs) || errs[0].Message() == "outer" {
		t.Fatal("ordinary consuming recovery label changed", err)
	}
}

func TestMissingTokensLedgerSeedBudget(t *testing.T) {
	t.Parallel()
	p := &state.Parser{Missing: &state.MissingTokenState{Limit: 2}}
	event := state.MissingTokenEvent{Site: state.MissingTokenSite{ID: 1, Pos: 3}, Err: &value.SyntaxError{Pos: 3}}
	if !p.InsertMissing(event) || p.InsertMissing(event) {
		t.Fatal("site guard")
	}
	seed := p.MissingEvents(0)
	p.ResetMissing(0)
	if p.Missing.Attempts != 1 || seed[0].Site != event.Site {
		t.Fatal("rollback refunded budget or cleared owned seed")
	}
	p.ReplayMissing(seed, state.ErrEntry(event.Err))
	p.ReplayMissing(seed, state.ErrEntry(event.Err))
	if p.Missing.Attempts != 2 || len(p.Recovered) != 1 {
		t.Fatal("installed event replay was charged or duplicated")
	}
	p.ResetMissing(0)
	defer func() {
		f, ok := recover().(state.Fatal)
		var limit *runtime.MissingTokenLimitError
		if !ok || !errors.As(f.Err, &limit) || limit.Position != 3 {
			t.Fatal("seed replay did not exhaust budget")
		}
	}()
	p.ReplayMissing(seed, state.ErrEntry(event.Err))
}

func TestMissingTokensPredicateRecognitionAndMemoDependencies(t *testing.T) {
	t.Parallel()
	prog := enginetest.MustCompile(t, `rule main = x:(token #atomic) {len($x) == 0} $$
rule token = ";" #missing`)
	for _, recognize := range []bool{false, true} {
		requireInsertions(t, prog, "", engine.ParseOptions{Expectations: true, MissingTokens: 4, Recognize: recognize}, 1)
	}
	dependent := enginetest.MustCompile(t, `rule main = (outer "x" / outer "y") $$
rule outer = inner
rule inner = ";" #missing`)
	for _, name := range []string{"main", "outer", "inner"} {
		if !dependent.ByName[name].Missing {
			t.Fatal("missing dependency not transitive", name)
		}
	}
	// The budget must still be spent when completed nested rule results would
	// otherwise be reused after an alternative restores the same source position.
	if n, err := dependent.ParseWith("main", "y", engine.ParseOptions{Expectations: true, MissingTokens: 1}); n != nil {
		t.Fatal(n, err)
	} else {
		var limit *runtime.MissingTokenLimitError
		if !errors.As(err, &limit) {
			t.Fatal(err)
		}
	}
	// Explicitly inserting a value does not change how a successful plain
	// parse runs its unrelated action, while recognition may skip that action.
	actions := enginetest.MustCompile(t, `type R = { N int }
rule main = ";" #missing => R{N: 1/0}`)
	if n, err := actions.ParseWith("main", "", engine.ParseOptions{Expectations: true, MissingTokens: 2}); n != nil || err == nil {
		t.Fatal("ordinary action was skipped", n, err)
	}
	requireInsertions(t, actions, "", engine.ParseOptions{Expectations: true, MissingTokens: 2, Recognize: true}, 1)
}

func TestMissingTokensRetainedResultsAndLiteralRecursion(t *testing.T) {
	t.Parallel()
	// With insertion, e is left-recursive through the insertion of ";", and the growth of its seed
	// "x" by an insertion that consumes nothing does not win: no token is inserted.
	recursive := enginetest.MustCompile(t, `rule main = e $$
rule e = ";" #missing e / "x"`)
	if n, err := recursive.ParseWith("main", "x", engine.ParseOptions{Expectations: true, MissingTokens: 8}); err != nil || len(statetest.MissingNodes(t, n)) != 0 {
		t.Fatal("zero-width growth through an insertion won", n, err)
	}
	prog := enginetest.MustCompile(t, `rule main = e $$
rule e = ";" #missing e / "x" ";" #missing`)
	n, errs := requireInsertions(t, prog, "x", engine.ParseOptions{Expectations: true, MissingTokens: 8}, 1)
	before, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	retained := n.Clone()
	for i := 0; i < 4; i++ {
		requireInsertions(t, prog, "x", engine.ParseOptions{Expectations: true, MissingTokens: 8}, 1)
	}
	after, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	cloned, err := json.Marshal(retained)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || string(before) != string(cloned) || len(errs) != 1 {
		t.Fatal("later batch parses changed retained Missing results")
	}
}

func TestMissingTokensMemoSeedContextAndRecycledPointers(t *testing.T) {
	t.Parallel()
	p := &state.Parser{Missing: &state.MissingTokenState{Limit: 8}, Memo: state.NewMemoTable()}
	err := &value.SyntaxError{Pos: 0}
	p.InsertMissing(state.MissingTokenEvent{Site: state.MissingTokenSite{ID: 1, Pos: 0}, Err: err})
	e := &state.MemoEntry{}
	p.RecordMissingSeed(e, 0)
	if len(p.MissingSeedEvents(e)) != 1 {
		t.Fatal("seed did not retain insertion")
	}
	p.ResetMissing(0)
	// A pointer retired by the memo slab can become a seed with no insertions.
	p.RecordMissingSeed(e, 0)
	if len(p.MissingSeedEvents(e)) != 0 {
		t.Fatal("recycled pointer inherited an insertion")
	}
	for _, controlled := range []bool{false, true} {
		p := &state.Parser{Memo: state.NewMemoTable()}
		if controlled && state.BudgetControlsEnabled {
			p.Control = state.NewBudgetControl(context.Background(), runtime.Limits{})
		}
		ordinary := state.MemoKey{Rule: 1}
		lookahead := state.MemoKey{Rule: 1, MissingSilent: true}
		p.MemoPut(ordinary, &state.MemoEntry{Ok: true, End: 7})
		p.MemoPut(lookahead, &state.MemoEntry{Ok: false})
		if e, ok := p.MemoGet(ordinary); !ok || !e.Ok || e.End != 7 {
			t.Fatal("lookahead replaced insertion-enabled seed")
		}
		if e, ok := p.MemoGet(lookahead); !ok || e.Ok {
			t.Fatal("lookahead imported an insertion-enabled seed")
		}
	}
}

// TestMissingTypeChecked checks that a missing at run time takes only a missing token: an error
// value of recovery is an error but not a missing.
func TestMissingTypeChecked(t *testing.T) {
	t.Parallel()
	o := engine.ParseOptions{Expectations: true, MissingTokens: 8}
	inserted := enginetest.MustCompile(t, "type P = { M missing }\nrule main = \"a\" m:(\";\" #missing) $$ => P{M: $m.(_).(missing)}")
	n, err := inserted.ParseWith("main", "a", o)
	if n == nil || err == nil || strings.Contains(err.Error(), "cannot") {
		t.Fatalf("inserted: %v %v", n, err) // the tree, with the insertion's syntax error
	}
	recovered := enginetest.MustCompile(t, "rule main = \"a\" m:inner \"b\" $$ => $m.(_).(missing)\nrule inner = \"c\" #recover(skip=[x-y]*)")
	if _, err := recovered.ParseWith("main", "axyb", o); err == nil || !strings.Contains(err.Error(), "cannot cast error to missing") {
		t.Fatalf("recovered: %v", err)
	}
}
