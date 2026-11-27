package libgo

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
	"syntaxir.dev/sxir/internal/ir/code/exec/ref"
	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
	sxirvalue "syntaxir.dev/sxir/pkg/runtime/value"
)

// The generated Go library is the first implementation of the parser VM's operations under
// conformance (design 065). The binding calls the library's own functions; where an operation is
// inline code of a composition, it does what that code does: memo.store allocates an entry and
// stores it with memoPut as the call does. A stream parse also provides the pruning of its memo.

type rtMachine struct {
	p    *Parser
	vals map[int64][]any
	// metered reports a parse with budget controls, whose charges the suite checks.
	metered bool
	env     rtEnv
	lists   recHandles
}

// rtEnv binds the variable environment of a parser, which the node and the typed library share.
type rtEnv struct {
	rules   []*Rule
	nextKey int64
}

// setUp gives p the variables of cfg, through the setup of a grammar.
func (e *rtEnv) setUp(p *Parser, cfg ref.Config) {
	if len(cfg.Vars) == 0 {
		p.g = &grammar{}
		return
	}
	g, names, vals := &grammar{}, make([]string, len(cfg.Vars)), make([]any, len(cfg.Vars))
	for i, id := range cfg.Vars {
		names[i], vals[i] = fmt.Sprint("v", i), ref.Value(id)
	}
	for _, slots := range cfg.RuleVars {
		r := &Rule{}
		for _, slot := range slots {
			r.Vars = append(r.Vars, names[slot])
		}
		g.rules = append(g.rules, r)
		e.rules = append(e.rules, r)
	}
	g.SetVars(names, vals)
	p.g, p.Env, p.envDepth = g, g.initEnv, g.envDepth
}

// call runs an operation of the environment, and reports false for any other.
func (e *rtEnv) call(p *Parser, vals map[int64][]any, o *code.Op, args []any) ([]any, bool) {
	switch o.Name {
	case "env.lookup":
		return []any{p.variable(int(args[0].(int64)))}, true
	case "env.bind":
		p.bind(int(args[0].(int64)), args[1])
		return nil, true
	case "env.scope":
		return []any{p.Env}, true
	case "env.restore":
		p.Env = args[0].(*Env)
		return nil, true
	case "env.key":
		e.nextKey--
		vals[e.nextKey] = p.memoEnv(args[0].(*Rule))
		return []any{e.nextKey}, true
	}
	return nil, false
}

// errorList is an Errs handle: the entry that rec.since returned.
type errorList struct{ e recEntry }

// recHandles are the Errs handles of the entries that rec.since returned, one for each entry, so
// that a list that two calls share is one handle.
type recHandles map[recEntry]*errorList

// handle returns the Errs handle of the entry e: the one of the entry for an error or a list, and a
// new one for no errors, which has no identity of its own.
func (h recHandles) handle(e recEntry) *errorList {
	if e.none() {
		return &errorList{}
	}
	l := h[e]
	if l == nil {
		l = &errorList{e}
		h[e] = l
	}
	return l
}

// unrolled returns the number of lists that listing the errors of e visits, counted as the model
// counts them: one for an error, and one for a list with those of its entries.
func unrolled(e recEntry) int {
	switch {
	case e.list != nil:
		n := 1
		for _, x := range e.list.entries {
			n += unrolled(x)
		}
		return n
	case e.err != nil:
		return 1
	}
	return 0
}

// unrolledRecord returns the lists that listing the record of p visits.
func unrolledRecord(p *Parser) int {
	n := 0
	for _, e := range p.Recovered {
		n += unrolled(e)
	}
	return n
}

// listOf returns the lists that listing the Errs handle l visits, and its errors.
func listOf(l any) (visits, errs int) {
	e := l.(*errorList).e
	return unrolled(e), e.len()
}

// recoveredIDs returns the identifiers of the recovered errors of p.
func recoveredIDs(p *Parser) []int64 {
	var ids []int64
	for _, e := range p.recoveredErrors() {
		ids = append(ids, int64(e.Pos))
	}
	return ids
}

// recovery runs an operation of the recovered errors, and reports false for any other.
func recovery(p *Parser, lists recHandles, o *code.Op, args []any) ([]any, bool) {
	switch o.Name {
	case "rec.mark":
		return []any{int64(len(p.Recovered))}, true
	case "rec.push":
		p.PushRecovered(args[0].(*sxirvalue.SyntaxError))
		return nil, true
	case "rec.truncate":
		p.Recovered = p.Recovered[:args[0].(int64)]
		return nil, true
	case "rec.since":
		return []any{lists.handle(p.recSince(int(args[0].(int64))))}, true
	case "rec.add":
		// The null handle is a list of no errors (ctx.errs): there is nothing to record.
		if l, _ := args[0].(*errorList); l != nil {
			p.recAdd(l.e)
		}
		return nil, true
	case "rec.none":
		// The null handle is a list of no errors (ctx.errs).
		l, _ := args[0].(*errorList)
		return []any{l == nil || l.e.none()}, true
	case "stream.settleErrors":
		if p.Stream == nil {
			p.Stream = &streamInput{}
		}
		p.streamSettleErrors()
		return nil, true
	}
	return nil, false
}

func newRTMachine(cfg ref.Config, controlled, sparse, stream bool) conform.Impl {
	p := &Parser{MaxDepth: int(cfg.MaxDepth)}
	if stream {
		p.Stream = &streamInput{}
	}
	p.memo.stride, p.memo.sparse = cfg.Rules, sparse
	m := &rtMachine{p: p, vals: map[int64][]any{}, metered: controlled, lists: recHandles{}}
	m.env.setUp(p, cfg)
	if controlled {
		limit := cfg.Limit
		if limit == 0 {
			limit = sxirruntime.Unlimited
		}
		conformUnits(p, limit)
	}
	if m.metered {
		return meteredRT{m}
	}
	return m
}

type meteredRT struct{ *rtMachine }

// conformUnits starts the logical compute units of p for a controlled suite, with the compute limit
// (none for 0 or Unlimited): the count is the units that Used reports.
func conformUnits(p *Parser, limit uint64) {
	if limit == sxirruntime.Unlimited {
		limit = 0
	}
	p.startUnits(parseOptions{controls: true, unitsLimit: limit})
}

// conformMemory gives p a memory limit that no case reaches, before conformUnits starts its units, so
// that the operations take the paths that count the memory of what they make.
func conformMemory(p *Parser) {
	p.setControl(newBudgetControl(sxirruntime.Limits{Memory: math.MaxInt64}))
}

func (m meteredRT) Used() uint64 { return m.p.Units }

func (m *rtMachine) Datum(id int64) any { return ref.Value(id) }

func (m *rtMachine) Rule(index int64) any { return m.env.rules[index] }

func (m *rtMachine) Err(id int64) any { return &sxirvalue.SyntaxError{Pos: int(id)} }

func (m *rtMachine) Recovered() []int64 { return recoveredIDs(m.p) }

func (m *rtMachine) Unrolled() int { return unrolledRecord(m.p) }

func (m *rtMachine) List(l any) (visits, errs int) { return listOf(l) }

func (m *rtMachine) Values(id int64) any {
	if m.vals[id] == nil {
		m.vals[id] = []any{id}
	}
	return id
}

// interrupted turns the panic that ends a parse into the interruption of Exec IR: the nesting limit is
// "nesting", the other limits "limit", and an error that is no limit, such as a value that does not
// convert, "error".
func interrupted() {
	if x := recover(); x != nil {
		f, ok := x.(fatal)
		if !ok {
			panic(x)
		}
		le, limit := f.err.(*sxirruntime.LimitError)
		switch {
		case !limit:
			panic(code.Interrupt{Reason: "error"})
		case le.Kind == sxirruntime.LimitDepth:
			panic(code.Interrupt{Reason: "nesting"})
		}
		panic(code.Interrupt{Reason: "limit"})
	}
}

func (m *rtMachine) Call(o *code.Op, args []any) (res []any) {
	defer interrupted()
	p := m.p
	if res, ok := m.env.call(p, m.vals, o, args); ok {
		return res
	}
	if res, ok := recovery(p, m.lists, o, args); ok {
		return res
	}
	i := func(k int) int { return int(args[k].(int64)) }
	key := func() (int, int, int, []any) { return i(0), i(1), i(2), m.vals[args[3].(int64)] }
	switch o.Name {
	case "memo.lookup":
		rule, pos, level, env := key()
		e := p.memoGet(rule, pos, level, env, false)
		found := e != nil
		if found && e.vgen != p.gen && !p.advanceEntry(e, p.edits) {
			found = false
		}
		return []any{e, found}
	case "memo.store":
		rule, pos, level, env := key()
		e := p.memo.alloc()
		// Every entry the call protocol stores has a record of the input it examined. An entry here
		// stands for one without a record until memo.examine: it examined everything (unrecorded), so
		// every edit drops it.
		*e = memoEntry{ok: args[4].(bool), end: int32(i(5)), silent: args[6].(bool), vgen: p.gen}
		e.setRecs(recoveredErrors(args[8]))
		e.from, e.examined = unrecorded()
		return []any{p.memoPut(rule, pos, level, env, false, e)}
	case "memo.ok":
		return []any{args[0].(*memoEntry).ok}
	case "memo.end":
		return []any{int64(args[0].(*memoEntry).end)}
	case "memo.silent":
		return []any{args[0].(*memoEntry).silent}
	case "memo.examine":
		// The call records the low end of its examined input (memo.examine in the library): its position,
		// or the unit before it.
		e := args[0].(*memoEntry)
		e.from, e.examined, e.pinned = int32(m.posOf(e)), uint32(i(2)), false
		if args[1].(bool) {
			e.from--
		}
		return nil
	case "memo.examined":
		e := args[0].(*memoEntry)
		if isUnrecorded(e) {
			return []any{int64(0)}
		}
		return []any{int64(e.examined)}
	case "memo.before":
		e := args[0].(*memoEntry)
		return []any{!isUnrecorded(e) && int(e.from) < m.posOf(e)}
	case "memo.pin":
		args[0].(*memoEntry).pinned = true
		return nil
	case "memo.splice":
		// A Document edit: the Document's memo is the table of its next parse.
		confEdit(p, documentEdit{i(0), i(1), i(2)})
		return nil
	case "memo.prune":
		p.pruneStreamMemo(i(0))
		return nil
	case "memo.floor":
		return []any{int64(p.Stream.memoBase)}
	case "seen.first":
		return []any{p.confSeenFirst(i(0), i(1))}
	case "budget.charge":
		p.unitsSized(int(args[0].(uint64)))
		return nil
	case "budget.used":
		return []any{p.Units}
	}
	panic("library binding: no operation " + o.Name)
}

// unrecorded returns the examined input of an entry without a record of memo.examine: all of it, so
// that every edit drops the entry, as the reference does.
func unrecorded() (from int32, examined uint32) { return -1, math.MaxUint32 }

// isUnrecorded reports whether e has no record of memo.examine (unrecorded).
func isUnrecorded(e *memoEntry) bool { return e.examined == math.MaxUint32 }

// posOf returns the position of the entry e in the memo: the position of the slot that holds it.
func (m *rtMachine) posOf(e *memoEntry) int {
	t := &m.p.memo
	for i, head := range t.slots {
		for x := head; x != nil; x = x.next {
			if x == e {
				if i >= t.gap {
					i -= t.gapLen
				}
				return i
			}
		}
	}
	panic("library binding: entry not in the memo")
}

// confEdit applies a Document edit to the memo of p as an edit of a Document does (documentState.editReuse):
// it logs the edit and splices it in, and the parse that follows reads the log.
func confEdit(p *Parser, ed documentEdit) {
	p.edits = append(p.edits, ed)
	p.memo.splice(p.edits)
	p.gen = uint32(len(p.edits))
}

var (
	rtMemoOps   = []string{"memo.lookup", "memo.store", "memo.ok", "memo.end", "memo.silent", "seen.first"}
	rtBudgetOps = []string{"budget.charge", "budget.used"}
	rtStreamOps = []string{"memo.prune", "memo.floor"}
	rtDocOps    = []string{"memo.examine", "memo.examined", "memo.before", "memo.pin", "memo.splice"}
	// rtDocEditOps are the operations of the randomized cases that edit a Document's memo, with
	// nothing between a store and the edit that follows it that would remove the entry: the cases
	// then check which entries an edit keeps, moves and removes.
	rtDocEditOps = []string{"memo.lookup", "memo.store", "memo.end", "memo.examine", "memo.examined", "memo.before", "memo.pin", "memo.splice"}
	// rtEnvOps are the operations of the variable environment, with the memo operations that keys of
	// the environment reach.
	rtEnvOps = []string{"env.lookup", "env.bind", "env.scope", "env.restore", "env.key", "memo.lookup", "memo.store"}
	// rtEnvFree are the environment operations that count no compute unit: the key of a call is counted
	// where the call counts it (KeyUnits).
	rtEnvFree = []string{"env.lookup", "env.bind", "env.key"}
	// rtRecoveryOps are the operations of the recovered errors that the library implements as specified,
	// and rtRecoveryFree those that count no compute unit: the entries a stored result takes are counted
	// where it is stored.
	rtRecoveryOps  = []string{"rec.mark", "rec.push", "rec.truncate", "rec.since", "rec.add", "rec.none", "stream.settleErrors"}
	rtRecoveryFree = []string{"rec.mark", "rec.push", "rec.truncate", "rec.since", "rec.add", "rec.none", "stream.settleErrors"}
)

func rtSuites() []conform.Suite {
	var suites []conform.Suite
	for _, controlled := range []bool{false, true} {
		for _, sparse := range []bool{false, true} {
			// A parse reads a string, a stream or a Document. Streams and Documents memoize on the
			// first call: seen.first is not part of their parse.
			for _, mode := range []string{"string", "stream", "document"} {
				ops := append([]string(nil), rtMemoOps...)
				switch mode {
				case "stream":
					ops = append(slices.DeleteFunc(ops, func(o string) bool { return o == "seen.first" }), rtStreamOps...)
				case "document":
					ops = append(slices.DeleteFunc(ops, func(o string) bool { return o == "seen.first" }), rtDocOps...)
				}
				// The operations of the memo are no events: a call counts its key where it is made.
				free := slices.Clone(ops)
				if controlled {
					ops = append(ops, rtBudgetOps...)
				}
				suites = append(suites, conform.Suite{
					Name: fmt.Sprintf("generated Go library (controlled=%v, sparse seen=%v, %s)", controlled, sparse, mode),
					Ops:  ops,
					Free: free,
					New:  func(c ref.Config) conform.Impl { return newRTMachine(c, controlled, sparse, mode == "stream") },
					// The library applies an edit to most entries when a lookup finds them (advanceEntry).
					LazyEdits: mode == "document",
				})
			}
		}
	}
	suites = append(suites,
		conform.Suite{Name: "generated Go library (document edits)", Ops: rtDocEditOps, LazyEdits: true,
			New: func(c ref.Config) conform.Impl { return newRTMachine(c, false, false, false) }},
		conform.Suite{Name: "generated Go typed library (document edits)", Ops: rtDocEditOps, LazyEdits: true,
			New: func(c ref.Config) conform.Impl { return newTypedMachine(c, false) }})
	for _, controlled := range []bool{false, true} {
		ops := []string{"memo.lookup", "memo.store", "memo.ok", "memo.end", "memo.silent"}
		ops = append(ops, rtDocOps...)
		free := slices.Clone(ops)
		if controlled {
			ops = append(ops, rtBudgetOps...)
		}
		suites = append(suites, conform.Suite{
			Name:      fmt.Sprintf("generated Go typed library (controlled=%v)", controlled),
			Ops:       ops,
			Free:      free,
			LazyEdits: true,
			New:       func(c ref.Config) conform.Impl { return newTypedMachine(c, controlled) },
		})
		ops = slices.Clone(rtRecoveryOps)
		if controlled {
			ops = append(ops, rtBudgetOps...)
		}
		suites = append(suites, conform.Suite{
			Name: fmt.Sprintf("generated Go library recovered errors (controlled=%v)", controlled),
			Ops:  ops,
			Free: rtRecoveryFree,
			New:  func(c ref.Config) conform.Impl { return newRTMachine(c, controlled, false, false) },
		}, conform.Suite{
			Name: fmt.Sprintf("generated Go typed library recovered errors (controlled=%v)", controlled),
			Ops:  ops,
			Free: rtRecoveryFree,
			New:  func(c ref.Config) conform.Impl { return newTypedMachine(c, controlled) },
		})
		ops = slices.Clone(rtEnvOps)
		if controlled {
			ops = append(ops, rtBudgetOps...)
		}
		suites = append(suites, conform.Suite{
			Name: fmt.Sprintf("generated Go library environment (controlled=%v)", controlled),
			Ops:  ops,
			Free: rtEnvFree,
			New:  func(c ref.Config) conform.Impl { return newRTMachine(c, controlled, false, false) },
		}, conform.Suite{
			Name: fmt.Sprintf("generated Go typed library environment (controlled=%v)", controlled),
			Ops:  ops,
			Free: rtEnvFree,
			New:  func(c ref.Config) conform.Impl { return newTypedMachine(c, controlled) },
		})
	}
	return suites
}

// recoveredErrors returns the errors that an entry of the library holds for the recovered errors of
// an argument of memo.store: none for nil, one error for the errors that conform.ErrsOp makes, and
// the entry of a handle that rec.since returned.
func recoveredErrors(arg any) recEntry {
	switch a := arg.(type) {
	case nil:
		return recEntry{}
	case *errorList:
		return a.e
	}
	return recEntry{err: &sxirvalue.SyntaxError{}}
}

// typedMachine binds the memo operations of the typed library, whose edits a typed Document's table
// (typedDocumentState) applies between parses.
type typedMachine struct {
	p     *TParser
	doc   *typedDocumentState
	vals  map[int64][]any
	env   rtEnv
	lists recHandles
}

func newTypedMachine(cfg ref.Config, controlled bool) conform.Impl {
	p := &TParser{Parser: &Parser{MaxDepth: int(cfg.MaxDepth)}}
	m := &typedMachine{p: p, doc: &typedDocumentState{}, vals: map[int64][]any{}, lists: recHandles{}}
	m.env.setUp(p.Parser, cfg)
	if controlled {
		limit := cfg.Limit
		if limit == 0 {
			limit = sxirruntime.Unlimited
		}
		conformUnits(p.Parser, limit)
		return meteredTyped{m}
	}
	return m
}

type meteredTyped struct{ *typedMachine }

func (m meteredTyped) Used() uint64 { return m.p.Units }

func (m *typedMachine) Datum(id int64) any { return ref.Value(id) }

func (m *typedMachine) Rule(index int64) any { return m.env.rules[index] }

func (m *typedMachine) Err(id int64) any { return &sxirvalue.SyntaxError{Pos: int(id)} }

func (m *typedMachine) Recovered() []int64 { return recoveredIDs(m.p.Parser) }

func (m *typedMachine) Unrolled() int { return unrolledRecord(m.p.Parser) }

func (m *typedMachine) List(l any) (visits, errs int) { return listOf(l) }

func (m *typedMachine) Values(id int64) any {
	if m.vals[id] == nil {
		m.vals[id] = []any{id}
	}
	return id
}

func (m *typedMachine) Call(o *code.Op, args []any) (res []any) {
	defer interrupted()
	p := m.p
	if res, ok := m.env.call(p.Parser, m.vals, o, args); ok {
		return res
	}
	if res, ok := recovery(p.Parser, m.lists, o, args); ok {
		return res
	}
	i := func(k int) int { return int(args[k].(int64)) }
	switch o.Name {
	case "memo.lookup":
		e := p.memoGet(i(0), i(1), i(2), m.vals[args[3].(int64)], false)
		found := e != nil
		if found && e.vgen != p.gen && !p.advanceEntry(e, p.edits) {
			found = false
		}
		return []any{e, found}
	case "memo.store":
		e := p.memoSlab.one()
		// As in the node binding, an entry stands for one without a record until memo.examine.
		*e = tmemoEntry{ok: args[4].(bool), end: int32(i(5)), silent: args[6].(bool), vgen: p.gen}
		e.setRecs(recoveredErrors(args[8]))
		e.from, e.examined = unrecorded()
		return []any{p.memoPut(i(0), i(1), i(2), m.vals[args[3].(int64)], false, e)}
	case "memo.ok":
		return []any{args[0].(*tmemoEntry).ok}
	case "memo.end":
		return []any{int64(args[0].(*tmemoEntry).end)}
	case "memo.silent":
		return []any{args[0].(*tmemoEntry).silent}
	case "memo.examine":
		e := args[0].(*tmemoEntry)
		e.from, e.examined, e.pinned = int32(m.posOf(e)), uint32(i(2)), false
		if args[1].(bool) {
			e.from--
		}
		return nil
	case "memo.examined":
		e := args[0].(*tmemoEntry)
		if e.examined == math.MaxUint32 {
			return []any{int64(0)}
		}
		return []any{int64(e.examined)}
	case "memo.before":
		e := args[0].(*tmemoEntry)
		return []any{e.examined != math.MaxUint32 && int(e.from) < m.posOf(e)}
	case "memo.pin":
		args[0].(*tmemoEntry).pinned = true
		return nil
	case "memo.splice":
		m.edit(documentEdit{i(0), i(1), i(2)})
		return nil
	case "budget.charge":
		p.unitsSized(int(args[0].(uint64)))
		return nil
	case "budget.used":
		return []any{p.Units}
	}
	panic("typed library binding: no operation " + o.Name)
}

// posOf is rtMachine.posOf for the typed memo.
func (m *typedMachine) posOf(e *tmemoEntry) int {
	for i, head := range m.p.memoSlots {
		for x := head; x != nil; x = x.next {
			if x == e {
				if i >= m.p.memoGap.gap {
					i -= m.p.memoGap.gapLen
				}
				return i
			}
		}
	}
	panic("typed library binding: entry not in the memo")
}

// edit is confEdit for a typed Document: the edit between parses logs the edit and splices it into the
// table, and the next parse starts from the kept chains, without the hash indexes of the last.
func (m *typedMachine) edit(ed documentEdit) {
	p := m.p
	p.edits = append(p.edits, ed)
	m.doc.slots, m.doc.memoGap = p.memoSlots, p.memoGap
	m.doc.splice(p.edits)
	p.memoSlots, p.memoGap, p.memoIndexes, p.memoIndexBytes = m.doc.slots, m.doc.memoGap, nil, 0
	p.gen = uint32(len(p.edits))
}

// conformanceFailures runs the handwritten cases and seeds randomized cases of n calls on s, and
// returns the failures and the number of cases that ran.
func conformanceFailures(s conform.Suite, seeds, n int) (fails []error, ran int) {
	run := func(c conform.Case) {
		skipped, err := conform.Run(s, c)
		if !skipped {
			ran++
		}
		if err != nil {
			fails = append(fails, err)
		}
	}
	for _, c := range conform.Cases() {
		run(c)
	}
	for seed := range seeds {
		// One case in four has a limit that the case may reach; the others run to their end.
		var limit uint64
		if seed%4 == 0 {
			limit = uint64(200 + seed)
		}
		rng := rand.New(rand.NewPCG(uint64(seed), 65))
		run(conform.Random(fmt.Sprint("random_", seed), rng, s.Ops, n, ref.Config{Rules: 3 + seed%2*130, Limit: limit, MaxDepth: 4, Vars: conform.RandomVars(seed), RuleVars: conform.RandomRuleVars}))
	}
	return fails, ran
}

func TestExecConformance(t *testing.T) {
	seeds := 300
	if testing.Short() {
		seeds = 30
	}
	for _, s := range rtSuites() {
		t.Run(s.Name, func(t *testing.T) {
			fails, ran := conformanceFailures(s, seeds, 150)
			if ran < seeds {
				t.Fatalf("only %d cases ran", ran)
			}
			for _, err := range fails {
				t.Error(err)
			}
		})
	}
}

// TestSettlingKeepsASmallScratch measures stream.settleErrors, whose declared cost is the bound of a
// representation: it keeps a scratch of at most streamRecoveryScratchMax slots, cleared so that it
// holds no error, and releases a larger list. Clearing the scratch and releasing a list are no
// events, so it counts nothing either way.
func TestSettlingKeepsASmallScratch(t *testing.T) {
	for _, slots := range []int{0, streamRecoveryScratchMax, streamRecoveryScratchMax + 1} {
		m := newRTMachine(ref.Config{}, true, false, true).(meteredRT)
		m.p.Recovered = make([]recEntry, slots)
		for i := range m.p.Recovered {
			m.p.Recovered[i] = recEntry{err: &sxirvalue.SyntaxError{Pos: i}}
		}
		before := m.Used()
		m.p.streamSettleErrors()
		if got := m.Used() - before; got != 0 {
			t.Errorf("settling a list of %d slots counted %d units", slots, got)
		}
		kept := slots <= streamRecoveryScratchMax
		if len(m.p.Recovered) != 0 || kept != (m.p.Recovered != nil) || kept && cap(m.p.Recovered) != slots {
			t.Errorf("settling a list of %d slots left len %d cap %d nil=%v, want the scratch kept=%v", slots,
				len(m.p.Recovered), cap(m.p.Recovered), m.p.Recovered == nil, kept)
			continue
		}
		for i, e := range m.p.Recovered[:cap(m.p.Recovered)] {
			if e != (recEntry{}) {
				t.Errorf("settling a list of %d slots left slot %d holding an error", slots, i)
			}
		}
	}
}

// TestExecConformanceDetectsBrokenLib is the negative control: deliberately broken variants of
// the library's operations must fail conformance.
func TestExecConformanceDetectsBrokenLib(t *testing.T) {
	type broken struct {
		op         string
		call       func(m *rtMachine, args []any) []any
		controlled bool
		stream     bool
		// document runs the cases of the document suite, which store entries without editing them
		// back into reach, instead of the cases that edit a Document's memo.
		document bool
		// want is a part of the failure message that names the reason, when the variant has one.
		want string
		// ops and free replace the operations of the suite, and name those that count no unit.
		ops, free []string
	}
	binding := func(m *rtMachine, args []any, units int) {
		slot := int(args[0].(int64))
		for range units {
			m.p.unitsSized(1)
		}
		m.p.Env = m.p.Env.set(slot, m.p.envDepth, args[1])
	}
	envValues := func(m *rtMachine, vals []any) []any {
		m.env.nextKey--
		m.vals[m.env.nextKey] = vals
		return []any{m.env.nextKey}
	}
	variants := map[string]broken{
		// The variable environment.
		"an equal binding makes a new environment": {op: "env.bind", ops: rtEnvOps, call: func(m *rtMachine, args []any) []any {
			binding(m, args, 0)
			return nil
		}},
		"a binding binds nothing": {op: "env.bind", ops: rtEnvOps, call: func(m *rtMachine, args []any) []any { return nil }},
		"a binding counts a unit": {op: "env.bind", ops: rtEnvOps, free: rtEnvFree, controlled: true, want: "no event", call: func(m *rtMachine, args []any) []any {
			m.p.unitsSized(1)
			m.p.bind(int(args[0].(int64)), args[1])
			return nil
		}},
		"a lookup counts a unit": {op: "env.lookup", ops: rtEnvOps, free: rtEnvFree, controlled: true, want: "no event", call: func(m *rtMachine, args []any) []any {
			m.p.unitsSized(1)
			return []any{m.p.Env.get(int(args[0].(int64)), m.p.envDepth)}
		}},
		"a lookup reads another slot": {op: "env.lookup", ops: rtEnvOps, call: func(m *rtMachine, args []any) []any {
			return []any{m.p.variable(0)}
		}},
		"a key counts a unit for the call": {op: "env.key", ops: rtEnvOps, free: rtEnvFree, controlled: true, want: "no event", call: func(m *rtMachine, args []any) []any {
			m.p.unitsSized(1)
			return envValues(m, m.p.memoEnv(args[0].(*Rule)))
		}},
		"a key leaves out its first variable": {op: "env.key", ops: rtEnvOps, call: func(m *rtMachine, args []any) []any {
			vals := m.p.memoEnv(args[0].(*Rule))
			return envValues(m, vals[min(1, len(vals)):])
		}},
		"restoring keeps the bindings": {op: "env.restore", ops: rtEnvOps, call: func(m *rtMachine, args []any) []any { return nil }},

		// The recovered errors.
		"a truncate keeps the last error": {op: "rec.truncate", ops: rtRecoveryOps, call: func(m *rtMachine, args []any) []any {
			m.p.Recovered = m.p.Recovered[:min(int(args[0].(int64))+1, len(m.p.Recovered))]
			return nil
		}},
		"since leaves the entries in place": {op: "rec.since", ops: rtRecoveryOps, call: func(m *rtMachine, args []any) []any {
			mark := int(args[0].(int64))
			if len(m.p.Recovered)-mark < 2 {
				return []any{m.lists.handle(m.p.recSince(mark))}
			}
			after := m.p.Recovered[mark:]
			l := &errList{entries: appendSpend(m.p, make([]recEntry, 0, len(after)), after)}
			for _, e := range after {
				l.n += e.len()
			}
			return []any{m.lists.handle(recEntry{list: l})}
		}},
		// A since that copies the errors of the lists and counts a unit for each, as the budget charged the
		// copy: within the bound, but it is no event.
		"since copies the errors of the lists": {op: "rec.since", ops: rtRecoveryOps, free: rtRecoveryFree, controlled: true, want: "no event", call: func(m *rtMachine, args []any) []any {
			mark := int(args[0].(int64))
			if len(m.p.Recovered)-mark < 2 {
				return []any{m.lists.handle(m.p.recSince(mark))}
			}
			l := &errList{}
			for _, e := range appendRecErrors(nil, m.p.Recovered[mark:]) {
				m.p.unitsSized(1)
				l.entries = append(l.entries, recEntry{err: e})
				l.n++
			}
			m.p.Recovered = append(m.p.Recovered[:mark], recEntry{list: l})
			return []any{m.lists.handle(recEntry{list: l})}
		}},
		// The control of the bound on unrolling: wrapping the one entry after a mark changes no error
		// and no charge, and only the bound fails.
		"since wraps the one entry after its mark": {op: "rec.since", ops: rtRecoveryOps, call: func(m *rtMachine, args []any) []any {
			mark := int(args[0].(int64))
			if len(m.p.Recovered)-mark != 1 {
				return []any{m.lists.handle(m.p.recSince(mark))}
			}
			l := &errList{entries: []recEntry{m.p.Recovered[mark]}, n: m.p.Recovered[mark].len()}
			m.p.Recovered[mark] = recEntry{list: l}
			return []any{m.lists.handle(recEntry{list: l})}
		}},
		"an add copies the errors of a list": {op: "rec.add", ops: rtRecoveryOps, call: func(m *rtMachine, args []any) []any {
			for _, e := range appendRecErrors(nil, []recEntry{args[0].(*errorList).e}) {
				m.p.PushRecovered(e)
			}
			return nil
		}},
		"settling keeps the errors": {op: "stream.settleErrors", ops: rtRecoveryOps, call: func(m *rtMachine, args []any) []any { return nil }},
		// Settling that clears a list beyond the scratch and counts a unit for each slot, as the budget
		// charged the clear: within the bound, but it is no event.
		"settling clears a list beyond the scratch": {op: "stream.settleErrors", ops: rtRecoveryOps, free: rtRecoveryFree, controlled: true, want: "no event", call: func(m *rtMachine, args []any) []any {
			m.p.unitsSized(cap(m.p.Recovered))
			clear(m.p.Recovered[:cap(m.p.Recovered)])
			m.p.Recovered = m.p.Recovered[:0]
			return nil
		}},

		// memoTable.put that links a new entry behind the chain instead of replacing the entry under
		// the same key: lookups keep finding the old entry.
		"store appends without replacing": {op: "memo.store", call: func(m *rtMachine, args []any) []any {
			t := &m.p.memo
			pos := int(args[1].(int64))
			e := t.alloc()
			*e = memoEntry{ok: args[4].(bool), end: int32(args[5].(int64)), rule: int32(args[0].(int64)), min: int32(args[2].(int64))}
			e.setKey(m.vals[args[3].(int64)])
			for pos >= len(t.slots) {
				t.slots = append(t.slots, nil)
			}
			link := &t.slots[pos]
			for *link != nil {
				link = &(*link).next
			}
			*link = e
			return []any{e}
		}},
		// A first-call record that drops the rule from the bit index (every rule shares one bit).
		"seen ignores the rule": {op: "seen.first", call: func(m *rtMachine, args []any) []any {
			return []any{m.p.confSeenFirst(0, int(args[1].(int64)))}
		}},
		// A lookup that forgets the level, as a key without Min would.
		"lookup ignores the level": {op: "memo.lookup", call: func(m *rtMachine, args []any) []any {
			e := m.p.memoGet(int(args[0].(int64)), int(args[1].(int64)), 0, m.vals[args[3].(int64)], false)
			return []any{e, e != nil}
		}},
		// A prune that shifts the slots by one position too many: the entries at the floor are lost.
		"prune drops the floor's entries": {op: "memo.prune", stream: true, call: func(m *rtMachine, args []any) []any {
			pos := int(args[0].(int64))
			m.p.pruneStreamMemo(pos + 1)
			m.p.Stream.memoBase = pos
			return nil
		}},
		// A prune that keeps the memo and moves only the floor: entries shift to the wrong positions.
		"prune moves only the floor": {op: "memo.prune", stream: true, call: func(m *rtMachine, args []any) []any {
			m.p.Stream.memoBase = int(args[0].(int64))
			return nil
		}},
		// A prune that frees nothing: the floor stays and every entry is kept.
		"prune frees nothing": {op: "memo.prune", stream: true, want: "result 0: model 4, implementation 0", call: func(m *rtMachine, args []any) []any {
			return nil
		}},
		// A prune that counts one unit more than the entries it removes: beyond the bound.
		"prune charges beyond its bound": {op: "memo.prune", stream: true, controlled: true, want: "more than the bound", call: func(m *rtMachine, args []any) []any {
			m.p.pruneStreamMemo(int(args[0].(int64)))
			m.p.unitsSized(1)
			return nil
		}},
		// A store before the floor that lands on the floor's slot instead of being dropped: a lookup
		// at the floor finds it.
		"store below the floor is kept": {op: "memo.store", stream: true, want: "model false, implementation true", call: func(m *rtMachine, args []any) []any {
			rule, pos, level, env := int(args[0].(int64)), int(args[1].(int64)), int(args[2].(int64)), m.vals[args[3].(int64)]
			e := m.p.memo.alloc()
			*e = memoEntry{ok: args[4].(bool), end: int32(args[5].(int64)), silent: args[6].(bool)}
			return []any{m.p.memoPut(rule, max(pos, m.p.Stream.memoBase), level, env, false, e)}
		}},
		// A splice that reads an empty examined input as no record: it drops an entry that examined no
		// input.
		"splice drops an entry that examined nothing": {op: "memo.splice", document: true, call: func(m *rtMachine, args []any) []any {
			for _, head := range m.p.memo.slots {
				for e := head; e != nil; e = e.next {
					if pos := m.posOf(e); e.vgen == m.p.gen && int(e.from) == pos && int(e.examined) == pos {
						e.from, e.examined = unrecorded()
					}
				}
			}
			confEdit(m.p, documentEdit{int(args[0].(int64)), int(args[1].(int64)), int(args[2].(int64))})
			return nil
		}},
		// A splice that keeps the entries whose examined end is the edit's start plus one.
		"splice keeps an entry that examined the edited unit": {op: "memo.splice", call: func(m *rtMachine, args []any) []any {
			start, end := int(args[0].(int64))+1, int(args[1].(int64))
			confEdit(m.p, documentEdit{start, max(start, end), int(args[2].(int64))})
			return nil
		}},
		// A splice that moves the entries that examined the unit before them at the edit's end.
		"splice moves an entry that examined the unit before the edit's end": {op: "memo.splice", call: func(m *rtMachine, args []any) []any {
			for _, head := range m.p.memo.slots {
				for e := head; e != nil; e = e.next {
					if e.vgen == m.p.gen && !isUnrecorded(e) {
						e.from = max(e.from, int32(m.posOf(e)))
					}
				}
			}
			confEdit(m.p, documentEdit{int(args[0].(int64)), int(args[1].(int64)), int(args[2].(int64))})
			return nil
		}},
		// A splice that moves the entries with recovered errors.
		"splice moves an entry with recovered errors": {op: "memo.splice", call: func(m *rtMachine, args []any) []any {
			for _, head := range m.p.memo.slots {
				for e := head; e != nil; e = e.next {
					e.setRecs(recEntry{})
				}
			}
			confEdit(m.p, documentEdit{int(args[0].(int64)), int(args[1].(int64)), int(args[2].(int64))})
			return nil
		}},
		// A splice that moves pinned entries.
		"splice moves a pinned entry": {op: "memo.splice", call: func(m *rtMachine, args []any) []any {
			for _, head := range m.p.memo.slots {
				for e := head; e != nil; e = e.next {
					e.pinned = false
				}
			}
			confEdit(m.p, documentEdit{int(args[0].(int64)), int(args[1].(int64)), int(args[2].(int64))})
			return nil
		}},
		// A lookup that does not apply the pending edits finds an entry where the edit moved it or
		// after the edit invalidated it.
		"lookup skips the edits": {op: "memo.lookup", document: true, call: func(m *rtMachine, args []any) []any {
			e := m.p.memoGet(int(args[0].(int64)), int(args[1].(int64)), int(args[2].(int64)), m.vals[args[3].(int64)], false)
			return []any{e, e != nil}
		}},
		// An edit that forgets its log moves the slots, and no entry learns how far.
		"splice keeps no log": {op: "memo.splice", document: true, call: func(m *rtMachine, args []any) []any {
			m.p.memo.splice([]documentEdit{{int(args[0].(int64)), int(args[1].(int64)), int(args[2].(int64))}})
			return nil
		}},
		// A lookup that counts two units for each entry it passes: within the results, beyond the bound.
		"lookup charges twice": {op: "memo.lookup", controlled: true, want: "more than the bound", call: func(m *rtMachine, args []any) []any {
			if i := m.p.memo.slot(int(args[1].(int64))); i < len(m.p.memo.slots) {
				for e := m.p.memo.slots[i]; e != nil; e = e.next {
					m.p.unitsSized(1)
					m.p.unitsSized(1)
				}
			}
			e := m.p.memoGet(int(args[0].(int64)), int(args[1].(int64)), int(args[2].(int64)), m.vals[args[3].(int64)], false)
			found := e != nil
			if found && e.vgen != m.p.gen && !m.p.advanceEntry(e, m.p.edits) {
				found = false
			}
			return []any{e, found}
		}},
	}
	for name, b := range variants {
		t.Run(name, func(t *testing.T) {
			ops := rtMemoOps
			if strings.HasPrefix(b.op, "memo.splice") {
				ops = rtDocEditOps
			}
			if b.document {
				ops = append(slices.DeleteFunc(append([]string(nil), rtMemoOps...), func(o string) bool { return o == "seen.first" }), rtDocOps...)
			}
			if b.stream {
				ops = append(slices.DeleteFunc(append([]string(nil), rtMemoOps...), func(o string) bool { return o == "seen.first" }), rtStreamOps...)
			}
			if b.ops != nil {
				ops = b.ops
				if b.controlled {
					ops = append(slices.Clone(ops), rtBudgetOps...)
				}
			}
			s := conform.Suite{Name: "broken: " + name, Ops: ops, Free: b.free, LazyEdits: slices.Contains(ops, "memo.splice"), New: func(c ref.Config) conform.Impl {
				if b.controlled {
					return meteredBrokenRT{brokenRT{newRTMachine(c, true, false, b.stream).(meteredRT).rtMachine, b.op, b.call}}
				}
				return brokenRT{newRTMachine(c, false, false, b.stream).(*rtMachine), b.op, b.call}
			}}
			fails, _ := conformanceFailures(s, 200, 150)
			if len(fails) == 0 {
				t.Fatal("conformance did not detect the broken operation")
			}
			var f *conform.Failure
			if !errors.As(fails[0], &f) {
				t.Fatalf("not a conformance failure: %v", fails[0])
			}
			if !strings.Contains(f.Msg, b.want) {
				t.Fatalf("failed for another reason than %q:\n%v", b.want, fails[0])
			}
			t.Logf("detected:\n%v", fails[0])
		})
	}
}

type brokenRT struct {
	*rtMachine
	op   string
	call func(m *rtMachine, args []any) []any
}

func (b brokenRT) Call(o *code.Op, args []any) []any {
	defer interrupted()
	if o.Name == b.op {
		return b.call(b.rtMachine, args)
	}
	return b.rtMachine.Call(o, args)
}

type meteredBrokenRT struct{ brokenRT }

func (m meteredBrokenRT) Used() uint64 { return m.p.Units }

// TestExecConformanceDetectsBrokenTypedLib is the negative control of the typed library's
// Document operations: a splice that moves the entries with recovered errors, and one that moves
// the pinned entries, must fail conformance.
func TestExecConformanceDetectsBrokenTypedLib(t *testing.T) {
	variants := map[string]func(m *typedMachine){
		"splice moves an entry with recovered errors": func(m *typedMachine) {
			for _, head := range m.p.memoSlots {
				for e := head; e != nil; e = e.next {
					e.setRecs(recEntry{})
				}
			}
		},
		"splice moves a pinned entry": func(m *typedMachine) {
			for _, head := range m.p.memoSlots {
				for e := head; e != nil; e = e.next {
					e.pinned = false
				}
			}
		},
	}
	for name, unpin := range variants {
		t.Run(name, func(t *testing.T) {
			s := conform.Suite{Name: "broken: " + name, Ops: rtDocEditOps, LazyEdits: true, New: func(c ref.Config) conform.Impl {
				return brokenTyped{newTypedMachine(c, false).(*typedMachine), unpin}
			}}
			fails, _ := conformanceFailures(s, 200, 150)
			if len(fails) == 0 {
				t.Fatal("conformance did not detect the broken operation")
			}
		})
	}
}

// brokenTyped runs prepare before each memo.splice of a typed machine.
type brokenTyped struct {
	*typedMachine
	prepare func(m *typedMachine)
}

func (b brokenTyped) Call(o *code.Op, args []any) []any {
	if o.Name == "memo.splice" {
		b.doc.slots = b.p.memoSlots
		b.prepare(b.typedMachine)
	}
	return b.typedMachine.Call(o, args)
}
