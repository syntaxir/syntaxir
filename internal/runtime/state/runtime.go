package state

import (
	"fmt"
	"sync"

	"syntaxir.dev/sxir/internal/runtime/nodes"
	"syntaxir.dev/sxir/internal/runtime/program"
	"syntaxir.dev/sxir/pkg/runtime"
	"syntaxir.dev/sxir/pkg/runtime/value"
	"syntaxir.dev/sxir/pkg/runtime/value/impl"
)

// SharedProgram is the part of a program that the parses of every backend read: the static
// program, the environment the declared variables start from and the buffers of finished parses.
// Program embeds it, and a parser points to it.
type SharedProgram struct {
	*staticProgram
	// scratch holds the buffers of finished parses for later ones (*scratch).
	scratch sync.Pool
	// initEnv is the environment of the declared variables that every parse starts from
	// (Parser.SeedVars). Environments are persistent, so every parse shares it.
	initEnv *Env
	// envLevels is the depth of the environment trie (EnvLevels of the variable count).
	envLevels int
	// runKeys holds, by site, the slots of the variables that key the records of a run site that is
	// not free of the environment (program.Program.RunKeys), nil for the other sites.
	runKeys []*runKeySlots
}

// runKeySlots returns the slots of the key of the run site, which is not free of the environment.
func (s *SharedProgram) runKeySlots(site int) *runKeySlots {
	if site < len(s.runKeys) && s.runKeys[site] != nil {
		return s.runKeys[site]
	}
	panic(fmt.Sprintf("run site %d is keyed by the variables but the program has no key for it", site))
}

// InitSharedProgram makes s the shared part of a program that runs the static program sp, with
// the declared variables of sp at their initial values.
func InitSharedProgram(s *SharedProgram, sp *staticProgram) {
	s.staticProgram = sp
	vars := sp.Vars.All()
	vals := make([]any, len(vars))
	for i, d := range vars {
		vals[i] = d.Init
	}
	s.envLevels = EnvLevels(len(vals))
	s.initEnv = NewEnv(vals, s.envLevels)
	s.runKeys = nil
	for site, k := range sp.RunKeys {
		for len(s.runKeys) <= site {
			s.runKeys = append(s.runKeys, nil)
		}
		s.runKeys[site] = &runKeySlots{vars: sp.Vars.Slots(k.Vars), bind: sp.Vars.Slots(k.Bind)}
	}
}

// StaticProgramOf returns the static program that s runs.
func StaticProgramOf(s *SharedProgram) *staticProgram { return s.staticProgram }

// Parser is the execution state of a single parse.
type Parser struct {
	Prog *SharedProgram
	// tune holds the settings of the parse (Tuning); the zero value selects the defaults.
	tune Tuning
	Input
	Pos int
	// Hw and lw are the input range [lw, hw) examined by the current rule call. They are used by
	// incremental parsing.
	Hw int
	lw int
	// Stats counts evaluations and memo uses.
	Stats runtime.ParseStats
	// Aborted reports an interrupted execution, whose partial caches cannot be
	// reused by a Document. Normal syntax errors are completed executions.
	Aborted        bool
	EmitCall       bool // a caller-owned stream callback is currently executing
	Diagnostic     func(*value.SyntaxError) error
	DiagnosticTail StreamDiagnosticTail

	Env       *Env   // predicate variables
	envLevels int    // the depth of Env (Prog.envLevels)
	initEnv   *Env   // the initial environment of the variables (Parser.SeedVars), or nil
	Frame     *Frame // current capture frame
	Trail     []Undo // capture writes to undo on backtracking
	Cut       bool   // whether a cut has been passed in the current alternative
	Memo      *MemoTable

	// While silent is positive (inside a lookahead), expectations are not recorded.
	Silent   int
	Farthest int
	// Expectations makes Expect record expectations. Without it only Farthest advances, and a
	// record holds at most the placeholder ExpFar, which stands for a failure at its position.
	Expectations bool
	// Exp is the stack of expectation records. The current record is Exp[ExpBase:], the
	// expectations at Farthest. An expectation is an index into the expectation descriptions; an
	// @error message is an index with MsgBit set (ExpID).
	Exp     []ExpID
	ExpBase int
	// ExpBits cheaply excludes IDs absent from the current record. Collisions still scan.
	ExpBits uint64
	// Arena is an append-only area for expectations kept in memo entries and recovery (referenced by
	// slices).
	Arena    []ExpID
	LastKept []ExpID
	// Descs maps expectation indices to strings (it differs per backend).
	Descs []string
	// core is the state of the mechanisms the engine shares with the library of generated parsers
	// (core.go): the record of recovered errors and the stream's progress in delivering them.
	core
	Missing *MissingTokenState // invocation-local opt-in insertion accounting
	// MissingLen is len(missing.path), or 0 without missing. It lives here so that mark and reset
	// need no pointer chase or call on strict parses.
	MissingLen int

	// Emit receives the elements of the #stream repetition when parsing a stream.
	Emit func(*value.Node) error
	// StreamID is the ID of the start rule of a stream parse, the one rule whose #stream repetition
	// streams.
	StreamID int
	// Depth is the depth of nested recursive rule calls (0 outside them, so a start rule outside every
	// call cycle has depth 0). The parse is aborted when it exceeds MaxDepth.
	Depth    int
	MaxDepth int
	// Created holds the struct nodes built by the action being evaluated (from EvalCtx.Cbase).
	Created []*value.Node
	// MemoAll requests memoizing transient rules (rule.transient) too (used by Document).
	MemoAll bool
	// NoPlain keeps call sites from calling InvokePlain directly for plain rules (Rule.Plain), so
	// that every rule call goes through Call. It is set when MemoAll is (plain rules are then
	// memoized) and when tracing (Call reports the calls), before the parse runs. Every backend
	// reads it once, to run its call-preserving family (FamilyOf), whose call sites all go through
	// Call.
	NoPlain bool
	// Tr is the tracing state, or nil when the parse is not traced.
	Tr *Tracer
	// DeferMemo defers memoizing a rule at a position to its second call there (FirstCall). It is
	// set for whole-input parses; Document and streams memoize on the first call.
	DeferMemo bool

	// Backend is the state that the backend running the parse keeps beside the parser, or nil
	// when it keeps none.
	Backend BackendState

	// Chunks of nodes and child slices (alloc.go), and the stack that collects repetition children
	NodeSlab  []value.Node
	PtrSlab   []*value.Node
	KidStack  []*value.Node
	FrameSlab []Frame
	fieldSlab []valueimpl.NodeField
	// Document edits: gen is the number of edits made before this parse, and edits records them
	// (see moveResult). moved holds the copies made by MoveEmpty.
	Gen    uint32
	Edits  []DocEdit
	Moved  map[*value.Node]*value.Node
	Shifts map[uint32]int // ShiftOf of each generation in the current moveResult
	// moveBase and baseShift are the generation and the shift of the memo entry being moved.
	moveBase  uint32
	baseShift int
	// Runs and LastRuns are the repetition runs recorded in this Document parse and the previous
	// one (resume.go). Runs is nil outside Document parses.
	Runs, LastRuns map[RunKey]*RunRecord
	// SeedReads counts the reads of growing seeds and the reuses of results that depend on them
	// (context_memo.go). It only increases, so a repetition run that it does not change read
	// none (runClean).
	SeedReads int
	// seedReadLog lists, under RunOpenGrowthsOnly, the index in gamma of the outermost growth that
	// each read may concern (seedRead).
	seedReadLog []int
	// gamma holds the growths in progress, innermost last; those at one position are consecutive.
	gamma []growRec
	// frames and deps record the dependencies of evaluations in a context, looks the sites of the
	// lookaheads in progress (-1 for one that records nothing), obsGuard the observations being
	// checked, ctxs the context tables of the positions where growths are in progress, seedSeq
	// numbers the seeds, and grown passes the result of GrowEnd to CallEnd (context_memo.go).
	frames   []depFrame
	deps     []dep
	looks    []lookRec
	obsGuard []int32
	ctxs     []*ctxTable
	seedSeq  uint64
	// interned and internSeq give identities to keys too large for a word (intern).
	interned  map[string]uint64
	internSeq uint64
	grown     *MemoEntry
	// ContextMemoOff evaluates again every call of a rule of a left-recursion cycle in a context
	// and every step of a growth there, instead of reusing a result (ParseOptions.ContextMemoOff).
	ContextMemoOff bool
	// ContextMemoExamined puts the examined range into the identities of growth steps and the
	// outcomes of lookaheads in a context of a parse that reads no Document, as a Document parse
	// does (ParseOptions.ContextMemoExamined, rangeKept).
	ContextMemoExamined bool
	// keptSeedResults are the results that read a seed, for CtxKeepSeedResults.
	keptSeedResults []keptSeedResult
	Resumed         int // elements reused by ResumeRepeat (for tests)
	// skipTables holds the skip-run records of each site, at 2*site outside lookaheads and
	// 2*site+1 inside them, and skip is the walk in progress (skip_walks.go).
	skipTables []*skipSlots
	skips      []skipWalk
	skipN      int
	// runCold holds what the walks of each run site did before the site turned hot (skip_walks.go).
	runCold []runCold
	// scan is the walk of a scan site of a Flow body that has reached ScanHotSpan characters
	// (scan_sites.go).
	scan flowScan
	// skipBytes counts the bytes of the element buffers and error lists of the skip-run records.
	skipBytes uint64
	// Scratch holds the pooled buffers the parser uses (AcquirePooledParser), or nil.
	Scratch *scratch
	// MemUsed counts the bytes of the chunks the parse has taken (budget_memory.go), apart from
	// node chunks, which NodeChunks counts. A stream returns those of the elements it has
	// delivered when it splits its chunks. MemKept counts what a stream keeps across its commits:
	// the node chunks of its prefix and the iterative VM's frames.
	MemUsed, MemKept uint64
	// NodeChunks counts the node chunks allocated since the last splitChunks.
	// A negative count tracks the exact-size prefix budget before #stream.
	NodeChunks  int
	ectx        EvalCtx        // area for UseCtx
	actionExprs actionRegs     // registers of action programs (ActionProgram.Eval)
	item        [1]*value.Node // the element list of an action whose body is not a sequence (one)
	oplocals    [3]Local       // $lhs, $rhs and $op of a OperatorsRule operator action
	Saved       []*value.Node  // captures saved across a reset in longest
	cutFailed   bool           // a candidate of the last longest match failed after a cut
	afterEmpty  bool           // the next expression is the right operand of an infix operator that consumed no input
	nudBound    int            // the binding level above which no operator can follow the last start of an expression
}

// BackendState is the state that a backend keeps beside the parser for the parse it runs, such
// as the stacks of a VM. The parser holds it so that a rule body, which receives only the parser,
// can find it. The shared code calls it where it lets go of parse state; a Document keeps it from
// one parse to the next.
type BackendState interface {
	// SplitChunks lets go of the chunks the backend allocates values from, when the parser
	// splits its own chunks (splitChunks, StartStreamChunks).
	SplitChunks()
	// ClearInactive clears the unused capacity of the backend's stacks when a stream commits.
	ClearInactive()
	// Attach makes the state serve the parser p, for a Document that kept it from a parse with
	// another parser.
	Attach(p *Parser)
	// Keep prepares the state to be kept for a later parse of an input of n units. It lets go
	// of the parser, which Attach replaces, and drops buffers much larger than such a parse
	// needs; clear also clears the used capacity.
	Keep(n int, clear bool)
	// Release lets go of the parser and clears and drops the buffers of the state.
	Release()
	// RetainedBytes estimates the memory of the buffers the state keeps.
	RetainedBytes() uint64
	// StackBytes returns the bytes of the stacks the state holds, by their capacity, for the
	// memory limit.
	StackBytes() uint64
}

type MemoKey struct {
	Rule, Pos, Min int
	MissingSilent  bool // insertion-disabled lookahead has its own LR seeds
	// DocOnly is set for the key of a rule that only a Document parse memoizes (rule.docOnlyMemo): a
	// controlled lookup of it finds an entry only if the values of the key are the entry's, a string
	// bound by the same assignment (identEqual), and none beyond the first docOnlyScan entries of a chain.
	// It shares a word with MissingSilent, so it adds nothing to the memory a key holds.
	DocOnly bool
	// Env holds the values of the variables the rule reads (rule.vars) when it is called, or nil.
	// Results of such rules are memoized per combination of these values.
	Env []any
}

type MemoEntry struct {
	Node     *value.Node
	End      int
	Examined int
	// From is the start of the examined input (detecting a line start may examine the preceding
	// character).
	From int
	// Gen is the number of document edits when the node was stored or last brought up to date
	// through this entry, and shift is how far edits have shifted the entry since; shifted reports
	// that some edit shifted it (the shifts may cancel out, and nodes may have been moved through
	// another result that shares them, so shift alone does not tell whether to move the node).
	Gen   uint32
	shift int32
	// Errs holds the syntax errors recovered in this call, as the list that rec.since returned. They
	// are recorded again, as one entry, when the memo entry is used.
	Errs RecEntry
	// Far and expected are the farthest failure position and the expectations recorded within this
	// call. They are recorded again when the memo entry is used.
	Far      int
	Expected []ExpID

	// MemoTable key, and the next entry at the same position
	Env       []any
	Pos       int
	Next      *MemoEntry
	Rule, Min int32
	// Vgen is the number of document edits that the positions (pos, end, examined, from, far) and
	// shift account for. A Document applies an edit to most entries only when they are looked up
	// (AdvanceEntry).
	Vgen uint32

	Ok bool
	// silent reports that the result was evaluated inside a lookahead. No expectations were
	// recorded, so it is re-evaluated when referenced outside a lookahead.
	silent        bool
	missingSilent bool // insertion-disabled lookahead seed key
	// Growing reports that this is a seed of a left recursion that grows (Parser.gamma).
	Growing bool
	// Present is the set of leaders (growRec.leaderBit) that grew at the position when a result of a
	// rule of a left-recursion cycle was evaluated without reading their seeds: the result is
	// reused where the leaders that grow are among them (CallBegin).
	Present uint64
	// Positional reports that the result depends on position values (.Start, .End). It cannot
	// be shifted past an edit.
	Positional bool
	shifted    bool

	// Seen is the number of document edits when the entry was last stored, or answered a lookup, in a
	// parse. The entry is current in the parse at Gen edits if Seen is Gen; an entry of an earlier text
	// that the parse has not touched is not, and comparisons with it are Document work (keyMatches).
	Seen uint32
}

// ReportedStats derives the public reuse breakdown from counters already
// maintained by the parser, avoiding another increment for every reused item.
func (p *Parser) ReportedStats() runtime.ParseStats {
	s := p.Stats
	s.ResumedElements = p.Resumed
	s.MemoHits = s.Reused - p.Resumed
	return s
}

// Frame holds the capture values for each rule body or repetition element.
type Frame struct {
	Vals []*value.Node
}

// EmptyFrame is the frame for scopes without captures (shared because it is never written to).
var EmptyFrame = &Frame{}

type Undo struct {
	F    *Frame
	Slot int
	Old  *value.Node
}

// Bind rebinds the variable in slot i. Binding the value it has already is a no-op. Saved
// environments are persistent, so a rebind copies the path to the slot (see Env).
func (p *Parser) Bind(slot int, val any) {
	if BudgetControlsEnabled && p.Control != nil {
		p.bindControlled(slot, val)
		return
	}
	// Variables are checked scalar values; an equal binding has no effect.
	if p.Env.Get(slot, p.envLevels) == val {
		return
	}
	p.Env = p.Env.Set(slot, p.envLevels, p.binding(val))
}

// binding returns what a slot holds once the assignment of val binds it: in a Document parse, a string
// with the stamp of the assignment (assigned), which the key of a rule that only a Document memoizes
// holds in its place; otherwise val.
func (p *Parser) binding(val any) any {
	if s, ok := val.(string); ok && p.MemoAll {
		return assigned{s: s, stamp: p.Memo.assign()}
	}
	return val
}

// Mark is the state restored on backtracking.
type Mark struct {
	Pos       int
	Env       *Env
	Trail     int
	Recovered int
	missing   int
}

func (p *Parser) Mark() Mark {
	return Mark{Pos: p.Pos, Env: p.Env, Trail: len(p.Trail), Recovered: len(p.Recovered), missing: p.MissingLen}
}

// DropRecovered drops the errors recovered and the tokens inserted since m, and keeps the rest of the
// state.
func (p *Parser) DropRecovered(m Mark) {
	if p.MissingLen > m.missing {
		p.ResetMissing(m.missing)
	}
	p.Recovered = p.Recovered[:m.Recovered]
}

func (p *Parser) SetCapture(slot int, v *value.Node) {
	p.Trail = append(p.Trail, Undo{F: p.Frame, Slot: slot, Old: p.Frame.Vals[slot]})
	p.Frame.Vals[slot] = v
}

// touch records that the input up to just before position n has been examined.
func (p *Parser) touch(n int) {
	if n > p.Hw {
		p.Hw = n
	}
}

// ExpMark is the expectation record before isolate.
type ExpMark struct {
	Far, Base int
	Bits      uint64
}

// Isolate starts a separate expectation record whose farthest position is pos.
func (p *Parser) Isolate(pos int) ExpMark {
	m := ExpMark{p.Farthest, p.ExpBase, p.ExpBits}
	p.ExpBase, p.Farthest, p.ExpBits = len(p.Exp), pos, 0
	return m
}

// Unisolate ends the separate record and returns its farthest position and expectations. The
// expectations alias the stack and are valid only until the next record (passing them to
// MergeExpected is fine; keep copies them).
func (p *Parser) Unisolate(m ExpMark) (far int, inner []ExpID) {
	far, inner = p.Farthest, p.Exp[p.ExpBase:]
	p.Exp = p.Exp[:p.ExpBase]
	p.ExpBase, p.Farthest, p.ExpBits = m.Base, m.Far, m.Bits
	return far, inner
}

// Rule is a rule as the parser runs it: the facts of the compiled rule and the executable parts that
// the backend built for it.
type Rule struct {
	staticRule
	// Kinds are the node kinds the rule makes (valueimpl.NewRuleKinds).
	Kinds valueimpl.RuleKinds
	// Body matches the body of the rule at the current position and returns its node; min is the
	// binding level of the call (see Call).
	Body func(p *Parser, min int) (*value.Node, bool)
	// NodeKinds and OpKinds are the kinds of nodeType and opNodeType.
	NodeKinds, OpKinds *nodes.NodeTypeKinds
	OperatorsRule      *OperatorsRule
	// Act evaluates the action (nil if there is no action).
	Act Evaluator
	// Value makes the rule's value from the value of its body, for a rule without an action.
	Value func(p *Parser, v *value.Node, start int) *value.Node
	// InitVals are the initial values of vars, in the same order (nil when vars is empty).
	InitVals []any
	// VarSlots are the slots of vars, in the same order (-1 for a name that no variable declares).
	VarSlots []int
}

// StaticRuleOf returns the compiled rule that r runs.
func StaticRuleOf(r *Rule) *staticRule { return &r.staticRule }

// noVals are the values of variables where no variable is defined (all nil): the memo key of
// most calls of a rule that reads variables, shared instead of allocated for each call. Memo
// entries only compare their keys, so nothing writes to it.
var noVals [16]any

// EnvValues returns the current values of the variables in slots (nil for a slot below zero, an
// undeclared variable).
func (p *Parser) EnvValues(slots []int) []any {
	if p.Env == nil && len(slots) <= len(noVals) {
		return noVals[:len(slots):len(slots)]
	}
	vals := make([]any, len(slots))
	for i, slot := range slots {
		if slot >= 0 {
			vals[i] = p.VarValue(slot)
		}
	}
	return vals
}

// memoEnv returns the values of the variables the memoized rule r reads, for its memo key. While
// nothing has been bound since the parse began they are the initial values, computed once: the
// key only compares them, so the slice is shared instead of allocated for each call. The key of a
// rule that only a Document memoizes holds the bindings of the variables (memoBindings).
func (p *Parser) memoEnv(r *Rule) []any {
	if p.Env == p.initEnv && r.InitVals != nil && (!BudgetControlsEnabled || p.Control == nil) {
		return r.InitVals
	}
	p.chargeMemoHeld(uint64(len(r.VarSlots)) * anyBytes)
	if p.docOnlyMemo(r) {
		return p.memoBindings(r.VarSlots)
	}
	return p.keyValues(r.VarSlots)
}

// memoBindings is keyValues for the key of a rule that only a Document memoizes: a string that an
// assignment of the parse bound is held with the stamp of the assignment (assigned), so that a lookup
// compares the assignments instead of the bytes (identEqual).
func (p *Parser) memoBindings(slots []int) []any {
	if p.Env == nil && len(slots) <= len(noVals) {
		return noVals[:len(slots):len(slots)]
	}
	vals := make([]any, len(slots))
	for i, slot := range slots {
		if slot >= 0 {
			vals[i] = p.Env.binding(slot, p.envLevels)
		}
	}
	return vals
}

// keyValues is EnvValues for a memo key, without charges: the call paid for its key (ChargeKey).
func (p *Parser) keyValues(slots []int) []any {
	if p.Env == nil && len(slots) <= len(noVals) {
		return noVals[:len(slots):len(slots)]
	}
	vals := make([]any, len(slots))
	for i, slot := range slots {
		if slot >= 0 {
			vals[i] = p.Env.Get(slot, p.envLevels)
		}
	}
	return vals
}

// Memoizes reports whether the results of rule r are memoized.
func (p *Parser) Memoizes(r *Rule) bool {
	return r.Leader || p.memoized(r)
}

// entry returns a new entry: from the memo table's slabs, or, for an entry made in a context, which
// may be held only by a context table and is dropped with it, from the heap.
func (p *Parser) entry(ctx bool) *MemoEntry {
	if ctx {
		return new(MemoEntry)
	}
	return p.Memo.Alloc()
}

// GrowingSeed returns the seed of the growth of the leader of key at its position, if it grows.
func (p *Parser) GrowingSeed(key MemoKey) (*MemoEntry, bool) {
	for i := len(p.gamma) - 1; i >= 0 && p.gamma[i].key.Pos == key.Pos; i-- {
		if k := &p.gamma[i].key; k.Rule == key.Rule && k.Min == key.Min && k.MissingSilent == key.MissingSilent {
			return p.gamma[i].seed, true
		}
	}
	return nil, false
}

// InnermostSeed returns the seed of the innermost growth in progress, or nil.
func (p *Parser) InnermostSeed() *MemoEntry {
	if len(p.gamma) == 0 {
		return nil
	}
	return p.gamma[len(p.gamma)-1].seed
}

// GrowState is the grow-the-seed state of a left-recursion leader.
type GrowState struct {
	Key     MemoKey
	start   int
	Rec     int
	missing int
	Best    *MemoEntry
	gi      int // the index of the growth in gamma
	gp      int // the index in gamma of the first growth of the leader's cycle at the position
	// ctx reports a growth in a context, whose steps are memoized with the present set present.
	ctx bool
	// inCtx reports a growth that began where another growth was in progress at its position.
	inCtx   bool
	present uint64
	done    bool
	// evaluated reports that a growth in a context evaluated the body for a step (growIsolate); one that took
	// every step from the context memo evaluated nothing, and its call reuses that memo (GrowEnd). A
	// growth outside a context evaluates every step and does not set it.
	evaluated bool
	// The step being evaluated in a context: the examined range and expectation record it is kept
	// apart from.
	savedExamined, savedFrom int
	exp                      ExpMark
	// run holds the steps since the last step that read a seed other than the leader's own.
	run []runStep
	// The step that growKeepStep returned last, until growAdoptStep adopts it: its identity as a seed
	// and what it recorded.
	pendID   uint64
	pendStep runStep
}

// GrowBegin starts the growth of a leader at the current position from a failure. A growth in a
// context, where another leader grows, memoizes its steps (growMemoStep, growKeepStep); the body is evaluated
// repeatedly, and the result at the point where the match stops growing is adopted.
func (p *Parser) GrowBegin(key MemoKey, r *Rule) GrowState {
	cycle := r.Cycle
	g := GrowState{Key: key, start: p.Pos, Rec: len(p.Recovered), missing: p.missingMark(), gi: len(p.gamma)}
	gp := p.gammaAt(key.Pos, int32(cycle))
	g.gp = gp
	g.inCtx = gp < g.gi
	g.Best = p.entry(g.inCtx)
	*g.Best = MemoEntry{Ok: false, End: g.start, Examined: g.start, From: g.start, Growing: true, Gen: p.Gen, Vgen: p.Gen}
	if p.Missing != nil {
		p.RecordMissingSeed(g.Best, g.missing)
	}
	p.gamma = append(p.gamma, growRec{key: key, seed: g.Best, id: failSeed, idx: int32(r.LeaderIndex), cycle: int32(cycle)})
	if gp < g.gi && !p.strictCtx() && !p.inserts(r) {
		g.present = p.present(gp)
		g.ctx = p.recordsIn(g.present)
	}
	return g
}

func (g *GrowState) growKey() growKey {
	return growKey{rule: int32(g.Key.Rule), min: int32(g.Key.Min), ms: g.Key.MissingSilent}
}

// growLonger reports whether a step of a growth outside a context, with success ok and ending at
// the position, matched more than the seed.
func (p *Parser) growLonger(g *GrowState, ok bool) bool {
	return ok && (!g.Best.Ok || p.Pos > g.Best.End)
}

// growAdopt adopts the successful longer step of a growth outside a context, with value v and the
// errors errs that it recovered, as its seed, and returns to the start of the growth. The caller drops
// the errors and the missing tokens after the growth's marks.
func (p *Parser) growAdopt(g *GrowState, v *value.Node, errs RecEntry) {
	old := g.Best
	g.Best = p.entry(g.inCtx)
	if !g.inCtx {
		// Only this growth holds its replaced seed.
		p.Memo.Retire(old)
	}
	*g.Best = MemoEntry{Node: v, Ok: true, End: p.Pos, Examined: p.Hw, From: p.lw, Growing: true, Gen: p.Gen, Vgen: p.Gen, Errs: errs}
	if p.Missing != nil {
		p.RecordMissingSeed(g.Best, g.missing)
	}
	p.gamma[g.gi].seed, p.gamma[g.gi].id = g.Best, p.nextSeedID()
	p.Pos = g.start
}

// growKeepStep ends the evaluation of a step of a growth in a context that growIsolate began, with
// the body's result and the errors errs that it recovered if it succeeded, keeps the result in the
// table of the context and returns it. It does not change the recovered errors: the printed growth
// lists the errors of the step with recSince and drops them after the step.
func (p *Parser) growKeepStep(g *GrowState, v *value.Node, ok bool, errs RecEntry) *MemoEntry {
	ds, st := p.growStepEnd(g)
	res := new(MemoEntry)
	*res = MemoEntry{Node: v, Ok: ok, End: p.Pos, Examined: st.examined, From: st.from, Growing: true, silent: st.silent, Gen: p.Gen, Vgen: p.Gen}
	if ok {
		res.Errs = errs
	} else {
		res.End = g.start
	}
	return p.growKeep(g, res, ds, st)
}

// growth is the state of a growth that the shared steps read (context_grow.go).
type growth = GrowState

// touchRange extends the examined range of the current evaluation by a reused result's.
func (p *Parser) touchRange(examined, from int) {
	p.touch(examined)
	p.lw = Min2(p.lw, from)
}

// setSeed makes e, with identity id, the seed of the growth.
func (p *Parser) setSeed(g *GrowState, e *MemoEntry, id uint64) {
	g.Best = e
	p.gamma[g.gi].seed, p.gamma[g.gi].id = e, id
}

// GrowEnd finalizes and returns the last result. CallEnd stores it (Parser.grown). It does not change
// the recovered errors: the printed growth drops the entries after the growth's mark and records the
// errors of the result.
func (p *Parser) GrowEnd(r *Rule, g *GrowState) (*value.Node, bool) {
	if g.ctx && !g.evaluated {
		// Every step came from the context memo: the call reuses memoized results without evaluating
		// its body, which its trace reports as a memo hit (Memo=true, Evals=0), and counts the reuse.
		p.Stats.Reused++
	}
	p.recordJumps(g)
	p.dropTable(g.gi)
	p.gamma = p.gamma[:g.gi]
	if len(p.gamma) == 0 && len(p.interned) > 0 {
		clear(p.interned)
		p.internSeq = 0
	}
	best := g.Best
	if g.ctx {
		// The seed may be shared by equal results of other growths (canonicalSeed).
		cp := new(MemoEntry)
		*cp = MemoEntry{Node: best.Node, Ok: best.Ok, End: best.End, Errs: best.Errs, Gen: p.Gen, Vgen: p.Gen}
		best = cp
	}
	best.Growing = false
	best.silent = p.Silent > 0
	best.Examined = p.Hw
	best.From = p.lw
	best.Positional = r.Positional
	p.grown = best
	p.Pos = best.End
	return best.Node, best.Ok
}

// GrowNextVM reports whether the iterative VM's growth evaluates the body for its next step. In a
// context it first takes the steps that the table gives, as the printed growth does, and begins the
// evaluation of the step. Each step it evaluates counts one unit; a step that the table gives counts
// none (spec/runtime/parsing.md#compute-units).
func (p *Parser) GrowNextVM(g *GrowState) bool {
	if g.done {
		return false
	}
	if !g.ctx {
		p.Spend()
		return true
	}
	if p.growMemoStep(g) != nil {
		g.done = true
		return false
	}
	p.growIsolate(g)
	p.Spend()
	return true
}

// GrowStepVM is a step of the iterative VM's growth: it takes the step as the printed growth does, and
// drops the errors that the step recovered; outside a context, it adopts a longer step with its errors
// and the tokens it inserted.
func (p *Parser) GrowStepVM(g *GrowState, v *value.Node, ok bool) {
	if !g.ctx {
		if !p.growLonger(g, ok) {
			g.done = true
			return
		}
		p.takeEntries(g.Rec)
		p.growAdopt(g, v, p.recSince(g.Rec))
		p.Recovered = p.Recovered[:g.Rec]
		if p.Missing != nil {
			p.ResetMissing(g.missing)
		}
		return
	}
	var errs RecEntry
	if ok {
		p.takeEntries(g.Rec)
		errs = p.recSince(g.Rec)
	}
	s := p.growKeepStep(g, v, ok, errs)
	p.Recovered = p.Recovered[:g.Rec]
	if !p.growGrew(g, s) {
		g.done = true
		return
	}
	p.growAdoptStep(g, s)
}

// GrowEndVM ends the iterative VM's growth as the printed growth does: GrowEnd, then the entries after
// the growth's mark are dropped and the errors of the result recorded, as the missing-token mode records
// them.
func (p *Parser) GrowEndVM(r *Rule, g *GrowState) (*value.Node, bool) {
	v, ok := p.GrowEnd(r, g)
	best := p.grown
	p.Recovered = p.Recovered[:g.Rec]
	if p.Missing != nil {
		p.ResetMissing(g.missing)
	}
	if p.Missing == nil {
		p.recAdd(best.Errs)
	} else {
		p.ReplayMissing(p.MissingSeedEvents(best), best.Errs)
	}
	return v, ok
}

// invoke evaluates the rule body and builds the value with the action or the CST rules.
func (p *Parser) invoke(r *Rule, min int) (*value.Node, bool) {
	st := p.InvokeBegin(r)
	v, ok := r.Body(p, min)
	return p.InvokeEnd(r, &st, v, ok)
}

// InvokePlain is an unmemoized call of a rule without captures: invoke with the steps of
// InvokeBegin and InvokeEnd inlined, minus the capture frame (the body never writes one, so the
// caller's stays current), and with the failure handling of call.
func (p *Parser) InvokePlain(r *Rule, min int) (*value.Node, bool) {
	start, rec, trail, missing := p.Pos, len(p.Recovered), len(p.Trail), p.missingMark()
	prevEnv, prevCut := p.Env, p.Cut
	p.Cut = false
	if r.Rec {
		p.Enter()
	}
	p.Stats.Evaluated++
	v, ok := r.Body(p, min)
	if r.Rec {
		p.Depth--
	}
	p.Cut = prevCut
	p.Trail = p.Trail[:Min2(trail, len(p.Trail))]
	if ok {
		v = p.Finish(r, EmptyFrame, v, start)
	} else {
		p.Pos = start
		p.Recovered = p.Recovered[:rec]
		if p.Missing != nil {
			p.ResetMissing(missing)
		}
	}
	p.Env = prevEnv
	return v, ok
}

// InvokeState is the state saved during a body evaluation.
type InvokeState struct {
	f         *Frame
	prevFrame *Frame
	PrevEnv   *Env
	PrevCut   bool
	Trail     int
	Start     int
}

func (p *Parser) InvokeBegin(r *Rule) InvokeState {
	st := InvokeState{f: p.NewFrame(len(r.Scope.Names)), prevFrame: p.Frame, PrevEnv: p.Env, PrevCut: p.Cut, Trail: len(p.Trail), Start: p.Pos}
	p.Frame, p.Cut = st.f, false
	if r.Rec {
		p.Enter()
	}
	p.Stats.Evaluated++
	return st
}

func (p *Parser) InvokeEnd(r *Rule, st *InvokeState, v *value.Node, ok bool) (*value.Node, bool) {
	if r.Rec {
		p.Depth--
	}
	p.Frame, p.Cut = st.prevFrame, st.PrevCut
	p.Trail = p.Trail[:Min2(st.Trail, len(p.Trail))]
	if ok {
		v = p.Finish(r, st.f, v, st.Start)
	}
	p.Env = st.PrevEnv
	return v, ok
}

// Finish builds the rule's value from the value of the rule body, and converts it to the rule's named node type, if any. A value-free
// rule is never checked, and neither is the top call of the start rule of a stream (StreamTop).
func (p *Parser) Finish(r *Rule, f *Frame, v *value.Node, start int) *value.Node {
	if r.Novalue || p.StreamTop(r) {
		return nil
	}
	if r.Act != nil {
		var locals *Local
		if r.OperatorsRule != nil {
			// The whole-rule action of an Operators Rule sees the folded value as $value.
			p.oplocals[0] = Local{Name: program.OperatorsRuleValue, Val: NodeOrNil(v)}
			locals = &p.oplocals[0]
		}
		v = p.runAction(r, f, locals, start, p.Pos)
		if r.NodeKinds != nil {
			v = p.convertResult(r, v)
		}
		return v
	}
	v = r.Value(p, v, start)
	if r.Splice {
		// No node of its own, and its captures end here: the caller takes the elements.
		if v != nil {
			nodes.D(v).Fresh = false
		}
		return v
	}
	if len(r.Scope.Names) > 0 {
		v = p.AttachCaptures(v, r.Scope, f, start, p.Pos)
	}
	owned := v != nil && nodes.D(v).Fresh
	if owned {
		if p.Prog.RecordsSourceInfo && nodes.D(v).Kind.Rule == "" {
			nodes.D(v).Kind = r.Kinds.Named(nodes.D(v).Kind, r.Name)
		}
		nodes.D(v).Fresh = false
	}
	if r.NodeKinds != nil {
		if owned && r.NodeKinds.ConvertOwned(v) {
			return v
		}
		v = p.convertResult(r, v)
	}
	return v
}

// StreamTop reports whether the call of r that ends is the top call of the start rule of a stream
// parse, whose value the parse does not build: a stream returns no tree, so it neither runs the
// rule's action nor converts its value. A recursive rule has left its nesting level when its call
// ends, so its top call ends at depth 0; any other rule is entered once.
func (p *Parser) StreamTop(r *Rule) bool {
	return p.Input.Stream && r.ID == p.StreamID && (!r.Rec || p.Depth == 0)
}

// convertResult converts v, the value of rule r, to the rule's named node type.
func (p *Parser) convertResult(r *Rule, v *value.Node) *value.Node {
	n, ok := r.NodeKinds.Convert(nodes.UnboxNode(v))
	if !ok {
		p.Fail("cannot convert %s to %s in result of rule %s", TypeName(nodes.UnboxNode(v)), r.NodeType, r.WhereName())
	}
	return n
}

// AttachCaptures attaches captures to the node as fields. Captures that did not match (nil) are
// not attached. If the node belongs to another rule, or is itself captured, it is wrapped in a tuple
// node first. It charges nothing: captures and the nodes that the parse builds are not an item of
// the cost (spec/runtime/parsing.md#cost), and the slots and names of a scope, and so the fields
// that it looks at, are a constant of the grammar.
func (p *Parser) AttachCaptures(v *value.Node, s *program.Scope, f *Frame, start, end int) *value.Node {
	found := false
	wrap := v == nil || !nodes.D(v).Fresh
	for _, c := range f.Vals {
		if c != nil {
			found = true
		}
		if c != nil && c == v {
			// Also wrap when a capture refers to the node itself (a capture of the whole body).
			wrap = true
		}
	}
	if !found {
		return v
	}
	if wrap {
		v = p.NewNode(nodes.Data{Kind: valueimpl.SeqKind, Start: int32(start), End: int32(end), Children: append(p.Nodes(1)[:0], v), Fresh: true})
	}
	if nodes.D(v).Fields == nil {
		nodes.D(v).Fields = p.fields(len(s.Names))
	}
	nodes.D(v).Labeled = p.Prog.RecordsSourceInfo
	for i, name := range s.Names {
		if f.Vals[i] != nil {
			valueimpl.SetField(&nodes.D(v).Fields, name, f.Vals[i])
		}
	}
	return v
}

// NodeOrNil converts a nil *Node to an interface nil.
func NodeOrNil(n *value.Node) any {
	if n == nil {
		return nil
	}
	return n
}

// Fatal is an error after which the parse cannot continue, such as a runtime error in an action.
type Fatal struct{ Err error }

func (p *Parser) Fail(format string, args ...any) {
	panic(Fatal{fmt.Errorf(format, args...)})
}

// Enter counts one more nested entry of a recursive rule and fails the parse beyond the nesting
// limit.
func (p *Parser) Enter() {
	p.Depth++
	if p.Depth > p.MaxDepth {
		p.tooDeep()
	}
}

//go:noinline
func (p *Parser) tooDeep() {
	panic(Fatal{Err: &runtime.LimitError{Kind: runtime.LimitDepth, Limit: uint64(p.MaxDepth), Used: uint64(p.MaxDepth), Requested: 1}})
}

func Min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// SyntaxError builds the syntax error of a failed parse, an event of one unit
// (spec/runtime/parsing.md#compute-units), which it counts and checks before it builds it.
func (p *Parser) SyntaxError() *value.SyntaxError {
	p.Spend()
	return p.MakeError(p.Farthest, p.Exp[p.ExpBase:])
}

// RecoveryError builds the syntax error of a recovery or of a missing-token insertion at pos with
// the expectations expected, an event of one unit (spec/runtime/parsing.md#compute-units), which it
// counts and checks before it builds it.
func (p *Parser) RecoveryError(pos int, expected []ExpID) *value.SyntaxError {
	p.Spend()
	return p.MakeError(pos, expected)
}

func (p *Parser) MakeError(pos int, expected []ExpID) *value.SyntaxError {
	e := &value.SyntaxError{Pos: pos}
	for _, x := range expected {
		if x == ExpFar {
			continue
		}
		if x&MsgBit != 0 {
			e.Messages = append(e.Messages, p.Descs[x&^MsgBit])
		} else {
			e.Expected = append(e.Expected, p.Descs[x])
		}
	}
	p.sortExpected(e.Expected)
	p.chargeError(e)
	e.Line, e.Col = p.lineCol(pos)
	return e
}

// nodeValue returns the Value of the node n: its text for a string node, and a tuple or list
// value sharing its elements otherwise.
func (p *Parser) nodeValue(n *value.Node) any {
	switch {
	case nodes.D(n).Terminal:
		return nodes.D(n).Text
	case nodes.D(n).Kind != nil && nodes.D(n).Kind.Base == valueimpl.BaseList:
		return p.NewNode(nodes.Data{Kind: valueimpl.ListValueKind, Start: nodes.D(n).Start, End: nodes.D(n).End, Children: nodes.D(n).Children})
	}
	return p.NewNode(nodes.Data{Kind: valueimpl.TupleValueKind, Start: nodes.D(n).Start, End: nodes.D(n).End, Children: nodes.D(n).Children})
}

// SeedVars gives the parser the initial environment of the program's variables. Every parse
// calls it once, before it enters the start rule.
func (p *Parser) SeedVars() {
	if t := p.Prog.Vars; t != nil {
		p.Env, p.initEnv, p.envLevels = p.Prog.initEnv, p.Prog.initEnv, p.Prog.envLevels
	}
}
