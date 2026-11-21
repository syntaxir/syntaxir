package ref

import (
	"fmt"

	"zeroparse.dev/zp/internal/ir/code"
)

// The reference model of the traced call path of the engine (the operations of component call): whether
// a rule call takes the traced path (a call that the traced path itself makes does not, so that it is
// not traced twice), the calls of the traced path in progress, and the events that the trace of the
// parse delivered. A traced call reports the begin of the call, runs it, and reports its
// end with its outcome. The model covers a call that runs its body directly, one of a rule that no
// parse memoizes and that leads no cycle (RuleSpec.Memo, DocMemo and Leader false), and does not model
// the examined range, the evaluation counts, the reuse of memo entries or the expectations that the
// events of the engine also carry.
//
// TODO(#310): the model declares no cost of its own for call.track beyond the bound that the operation
// declares (a unit and its body, as rule.invoke does). The engine charges the unit of the call protocol
// twice for a traced call until the charges count logical events, so the cases run without a budget and
// check no charge.

// callState is the state of the component call.
type callState struct {
	// open is the number of traced calls in progress.
	open   int
	events []string
}

// allowsCall reports whether a call of an operation of the traced call path meets its precondition.
func (m *Model) allowsCall(o *code.Op, args []any) (allowed, ok bool) {
	switch o.Name {
	case "call.track":
		s := m.spec(args[0].(RuleRef).Index)
		return m.cfg.Traced && !s.Memo && !s.DocMemo && !s.Leader, true
	}
	return false, false
}

// callCall runs an operation of the traced call path, and reports false for any other.
func (m *Model) callCall(o *code.Op, args []any) ([]any, bool) {
	s := &m.call
	switch o.Name {
	case "call.tracked":
		// Outside a traced call, which is the only place the cases ask: the call that the traced path
		// makes is not itself traced, and the mark of the engine that says so is gone when the call ends.
		return []any{m.cfg.Traced}, true
	case "call.track":
		index, level := args[0].(RuleRef).Index, args[1].(int64)
		s.open++
		depth := s.open
		start := m.regs["pos"].(int64)
		s.events = append(s.events, fmt.Sprintf("enter %s level=%d pos=%d depth=%d lookahead=%v", RuleName(index), level, start, depth, m.silent()))
		recs, tokens := len(m.rec), int64(len(m.missing.path))
		v, ok := m.evaluate(index, level, true)
		end := start
		if ok {
			end = m.regs["pos"].(int64)
		} else {
			// A call that fails leaves the position, the recovered errors and the tokens it inserted.
			m.regs["pos"] = start
			m.rec = m.rec[:recs]
			m.resetMissing(tokens)
		}
		s.events = append(s.events, fmt.Sprintf("exit %s level=%d pos=%d depth=%d lookahead=%v matched=%v end=%d", RuleName(index), level, start, depth, m.silent(), ok, end))
		s.open--
		return []any{v, ok}, true
	}
	return nil, false
}
