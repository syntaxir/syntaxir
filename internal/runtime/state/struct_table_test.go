package state

import (
	"fmt"
	"testing"

	"syntaxir.dev/sxir/internal/compile"
	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/testing/compiletest"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/input"
	"syntaxir.dev/sxir/pkg/runtime/value"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// TestEngineValueStructTable runs the struct table (conform.StructCases) on the engine's compositions
// cover and made (actionCover and actionMade, printed from Exec IR), through the method that the backends
// call to make a struct.
func TestEngineValueStructTable(t *testing.T) {
	for _, controlled := range []bool{false, true} {
		prog := &SharedProgram{}
		InitSharedProgram(prog, compiletest.Program(t, "type S = { F0 int }\n@start\nrule main = \"a\"\n", compile.Options{}).Runtime())
		p := &Parser{Prog: prog, Input: NewInput(conform.BuiltinInput, input.Bytes)}
		if controlled {
			p.Control = NewBudgetControl(nil, runtime.Limits{Compute: 1 << 62})
		}
		c := &EvalCtx{P: p, Start: conform.StructRuleStart, End: conform.StructRuleEnd}
		text := p.NewNode(nodes.Data{Kind: valueimpl.MatchKind, Terminal: true, Text: "xyz", Start: 2, End: 4})
		vals := map[string]any{
			conform.MemberNil:     nil,
			conform.MemberInt:     1,
			conform.BuiltinString: "ab",
			conform.BuiltinText:   text,
			conform.BuiltinNode:   p.NewNode(nodes.Data{Kind: valueimpl.SeqKind, Start: 2, End: 5, Children: []*value.Node{text, text}}),
			conform.StructLate:    p.NewNode(nodes.Data{Kind: valueimpl.MatchKind, Terminal: true, Text: "q", Start: 7, End: 9}),
		}
		err := conform.RunStructs("engine", func(fields []string) string {
			var names []string
			var vs []any
			for i, f := range fields {
				names = append(names, fmt.Sprintf("F%d", i))
				vs = append(vs, vals[f])
			}
			before := len(p.Created)
			n, err := c.NewStruct("S", names, vs)
			if err != nil {
				return "error: " + err.Error()
			}
			recorded := len(p.Created) == before+1 && p.Created[before] == n
			s := fmt.Sprintf("%d-%d", nodes.D(n).Start, nodes.D(n).End)
			if recorded {
				s += conform.StructRecorded
			}
			return s
		})
		if err != nil {
			t.Errorf("controlled=%v: %v", controlled, err)
		}
	}
}

// TestEngineValueStructTableActionCode runs the struct table on action code: the code the action expression compiler
// emits for a struct literal, with FIELD for each field, makes the struct the table wants, and FIELD
// charges what STEP, SETFIELD and COVER, which it fuses, charge.
func TestEngineValueStructTableActionCode(t *testing.T) {
	cp := compiletest.Program(t, "type S = { F0 int\n F1 int }\n@start\nrule main = \"a\"\n", compile.Options{})
	for _, controlled := range []bool{false, true} {
		prog := &SharedProgram{}
		InitSharedProgram(prog, cp.Runtime())
		// literal returns the action code of a struct literal of the fields, fused or not, whose values
		// are the locals v<i>.
		literal := func(fields []string, fused bool) *ActionProgram {
			tp := &ActionProgram{NA: int32(len(fields)) + 1, NN: 1, Structs: []ActionStruct{{Kind: cp.TypeKind("S"), Fields: 2}}}
			tp.Code = append(tp.Code, ActionInstr{Op: TStruct}, ActionInstr{Op: TIntK, A: 1})
			for i := range fields {
				tp.Names = append(tp.Names, fmt.Sprintf("v%d", i), fmt.Sprintf("F%d", i))
				v, name := int32(i)+1, int32(2*i+1)
				tp.Code = append(tp.Code, ActionInstr{Op: TLocal, Dst: v, A: name - 1})
				if fused {
					tp.Code = append(tp.Code, ActionInstr{Op: TField, A: 0, B: v, C: name})
				} else {
					tp.Code = append(tp.Code, ActionInstr{Op: TStep}, ActionInstr{Op: TSetField, A: 0, B: v, C: name}, ActionInstr{Op: TCover, A: 0, B: v})
				}
			}
			tp.Code = append(tp.Code, ActionInstr{Op: TMade, B: 0}, ActionInstr{Op: TRet})
			return tp
		}
		text := nodes.Ptr(nodes.Data{Kind: valueimpl.MatchKind, Terminal: true, Text: "xyz", Start: 2, End: 4})
		vals := map[string]any{
			conform.MemberNil:     nil,
			conform.MemberInt:     1,
			conform.BuiltinString: "ab",
			conform.BuiltinText:   text,
			conform.BuiltinNode:   nodes.Ptr(nodes.Data{Kind: valueimpl.SeqKind, Start: 2, End: 5, Children: []*value.Node{text, text}}),
			conform.StructLate:    nodes.Ptr(nodes.Data{Kind: valueimpl.MatchKind, Terminal: true, Text: "q", Start: 7, End: 9}),
		}
		err := conform.RunStructs("engine action code", func(fields []string) string {
			var got [2]string
			var used [2]uint64
			var fieldsOf [2]string // the same values in both, so the same pointers
			for k, fused := range []bool{true, false} {
				p := &Parser{Prog: prog, Input: NewInput(conform.BuiltinInput, input.Bytes)}
				if controlled {
					p.Control = NewBudgetControl(nil, runtime.Limits{Compute: 1 << 62})
				}
				var locals *Local
				for i, f := range fields {
					locals = &Local{Name: fmt.Sprintf("v%d", i), Val: vals[f], Next: locals}
				}
				c := &EvalCtx{P: p, Start: conform.StructRuleStart, End: conform.StructRuleEnd, Locals: locals}
				v, err := literal(fields, fused).Eval(c)
				if err != nil {
					return "error: " + err.Error()
				}
				n := v.(*value.Node)
				got[k] = fmt.Sprintf("%d-%d", nodes.D(n).Start, nodes.D(n).End)
				if len(p.Created) == 1 && p.Created[0] == n {
					got[k] += conform.StructRecorded
				}
				fieldsOf[k] = fmt.Sprint(nodes.D(n).Fields)
				if controlled {
					used[k] = p.Control.Used
				}
			}
			if got[0] != got[1] || used[0] != used[1] || fieldsOf[0] != fieldsOf[1] {
				return fmt.Sprintf("fused %s %s (%d units), unfused %s %s (%d units)", got[0], fieldsOf[0], used[0], got[1], fieldsOf[1], used[1])
			}
			return got[0]
		})
		if err != nil {
			t.Errorf("controlled=%v: %v", controlled, err)
		}
	}
}
