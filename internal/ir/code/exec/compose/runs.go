package compose

import (
	. "syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/ir/code/exec"
)

// addRuns adds the compositions of the repetition runs of a Document (spec/runtime/documents.md, "Runs"):
// which record a repetition takes over, which recorded elements it reuses, and what it records. Every
// backend that parses Documents prints them, so that each reuses exactly the elements that the others
// reuse, and a reparse counts the same units and reports the same Reused on each. A backend runs the
// repetition itself and calls them around its element expression:
//
//	runStart(r, ...)
//	for runMore(r) {
//		if v, ok := runNext(r); ok { keep v; continue }
//		runBegin(r); evaluate the element; runEnd(r, ok, v)
//		stop as a repetition does
//	}
//	if the repetition matched { runFinish(r) }
//
// What a run reuses is not an event of the parse: a reused element counts no unit, as the take of a
// record skips the iterations that it stands for (record 076). The bookkeeping of the examined input
// and the expectations of the elements is work of the Document, which the forms specialized for a
// budget do in a free region.
func addRuns(m *Module) {
	run := THandle(exec.Run)
	value := THandle(exec.Value)
	kept := THandle(exec.Kept)
	r := L("r")
	field := func(name string) Expr { return C("run."+name, r) }
	set := func(b *Builder, name string, x Expr) {
		b.Do("run.set"+string(name[0]-'a'+'A')+name[1:], r, x)
	}
	feats := []string{exec.Document, exec.Budget}
	// free opens a free region around body in a form specialized for a budget: what the compositions it
	// calls charge there is work of the Document.
	free := func(b *Builder, body func(b *Builder)) {
		b.Var("fm", TU64)
		b.If(F(exec.Budget), func(b *Builder) { b.Set("fm", C("budget.freeBegin", BoolC(true))) })
		body(b)
		b.If(F(exec.Budget), func(b *Builder) { b.Do("budget.freeEnd", L("fm")) })
	}

	start := m.AddFunc("runStart", []Param{P("r", run), P("site", TInt), P("min", TInt), P("max", TInt), P("shiftable", TBool)}, nil, feats, func(b *Builder) {
		b.Do("run.init", r, L("site"), L("min"), L("max"), L("shiftable"))
		// Whole: the record of this repetition at this position that this parse made, or that the parse
		// before made of the same text.
		b.If(C("run.takeHere", r), func(b *Builder) { b.Return() })
		b.If(Not(C("run.previous")), func(b *Builder) { b.Return() })
		// Across one edit: the record of the parse before, which exactly one edit [a, b) by delta
		// followed, at the position or where the edit moved it from.
		log := b.Let("log", C("doc.log"))
		last := b.Let("last", Bin("-", C("edit.count", log), Int(1)))
		set(b, "editStart", C("edit.start", log, last))
		set(b, "editEnd", C("edit.end", log, last))
		set(b, "editDelta", C("edit.delta", log, last))
		b.Var("at", TInt)
		b.Set("at", R(exec.Pos))
		b.If(Bin(">", R(exec.Pos), field("editStart")), func(b *Builder) {
			// Inside the inserted text no record was.
			b.If(Lt(R(exec.Pos), Add(field("editEnd"), field("editDelta"))), func(b *Builder) { b.Return() })
			b.Set("at", Bin("-", R(exec.Pos), field("editDelta")))
			set(b, "moved", BoolC(true))
		})
		b.Do("run.takeLast", r, L("at"), last)
	})
	start.Doc = `runStart starts the run r of the repetition site, with at least min and at most max elements (max -1 for
no bound), at the position, and finds the record it takes over: the record of this site here that this
parse made, or that the parse before made of the same text; or else, when exactly one edit [a, b) by
delta came since the parse before, the record of that parse here when the position is at most a, or at
the position less delta when it is at least b + delta. Between them no record is taken. shiftable tells
whether the values of the elements hold no position, so that the run may reuse old elements that the
edit moved.`

	more := m.AddFunc("runMore", []Param{P("r", run)}, []Type{TBool}, feats, func(b *Builder) {
		b.Return(And(Not(field("done")), Bin("||", Lt(field("max"), Int(0)), Lt(field("count"), field("max")))))
	})
	more.Doc = `runMore reports whether the run r may take another element: no reused element ended it, and it has
fewer than its maximum.`

	// runReuse replays the old element i of r, moved by shift since the generation gen: what evaluating it
	// did, except keeping its value, which it returns.
	reuse := m.AddFunc("runReuse", []Param{P("r", run), P("i", TInt), P("shift", TInt), P("gen", TInt)}, []Type{value}, feats, func(b *Builder) {
		i := L("i")
		// A reused element charges no unit, but it is a step of the parse at which a cancellation is observed.
		b.If(Bin("||", F(exec.Budget), F(exec.Units)), func(b *Builder) { b.Do("budget.poll") })
		v := b.Let("v", C("run.take", r, i, L("shift"), L("gen")))
		b.Do("doc.touch", C("run.oldExamined", r, i))
		b.Do("doc.touchFrom", C("run.oldFrom", r, i))
		k := b.Let("k", C("run.oldKept", r, i))
		far := b.Let("far", C("run.oldFar", r, i))
		b.If(And(Bin(">", C("exp.keptCount", k), Int(0)), Bin(">=", far, C("exp.farthest"))), func(b *Builder) {
			free(b, func(b *Builder) { b.Do("mergeKept", far, k) })
		})
		b.SetReg(exec.Pos, C("run.oldEnd", r, i))
		b.Do("run.resumed")
		set(b, "count", Add(field("count"), Int(1)))
		// The old run ended after an empty element or at its maximum.
		set(b, "done", Bin("||", Eq(C("run.oldEnd", r, i), C("run.oldStart", r, i)),
			And(Bin(">=", field("max"), Int(0)), Bin(">=", field("count"), field("max")))))
		b.Return(v)
	})
	reuse.Doc = `runReuse reuses the old element i of the run r, moved by shift since the parse that recorded it when the
Document had had gen edits: it replays what evaluating the element did (the examined input, the
expectations, the position and the statistics) and returns its value, for the caller to keep.`

	prefix := m.AddFunc("runPrefix", []Param{P("r", run)}, []Type{value, TBool}, feats, func(b *Builder) {
		k := field("k")
		b.If(Bin("||", Bin("||", field("moved"), field("done")), Bin(">=", k, C("run.oldCount", r))), func(b *Builder) {
			b.Return(Null(value), BoolC(false))
		})
		b.If(Bin(">", C("run.oldExamined", r, k), field("editStart")), func(b *Builder) { b.Return(Null(value), BoolC(false)) })
		v := b.Let("v", C("runReuse", r, k, Int(0), Int(0)))
		set(b, "k", Add(field("k"), Int(1)))
		b.Return(v, BoolC(true))
	})
	prefix.Doc = `runPrefix reuses the next old element of the run r if it examined only input before the edit since the
old run, whatever the edit's change in length, and returns its value and true; it returns false when the
reused prefix ends: at an element that examined input at or after the start of the edit, at the end of
the old run, after an element that ended the run, or at once when the old run is the one that the edit
moved here.`

	resync := m.AddFunc("runResync", []Param{P("r", run)}, []Type{TBool}, feats, func(b *Builder) {
		delta := field("editDelta")
		n := b.Let("n", C("run.oldCount", r))
		b.If(Bin("||", Bin("||", Bin(">=", field("j"), Int(0)), Eq(n, Int(0))), Lt(Bin("-", R(exec.Pos), delta), field("editEnd"))),
			func(b *Builder) { b.Return(BoolC(false)) })
		b.If(And(Not(field("shiftable")), Bin("!=", delta, Int(0))), func(b *Builder) { b.Return(BoolC(false)) })
		at := b.Let("at", Bin("-", R(exec.Pos), delta))
		// Neither the search nor the scan below charges: each passes an old element at most once in a
		// parse, so their work is bounded by the elements that the parse before recorded, which it
		// charged (spec/runtime/documents.md, "Work of a Document Parse").
		// The search: the first old element from k on that starts at or after at. The starts of the old
		// elements increase, and at only grows during a run (the position before each element), so the
		// search resumes where the last one stopped.
		b.If(Lt(field("seek"), field("k")), func(b *Builder) { set(b, "seek", field("k")) })
		b.Loop("search", func(b *Builder) {
			b.If(Bin("||", Bin(">=", field("seek"), n), Bin(">=", C("run.oldStart", r, field("seek")), at)), func(b *Builder) { b.Break("search") })
			b.If(Bin("||", F(exec.Budget), F(exec.Units)), func(b *Builder) { b.Do("budget.poll") })
			set(b, "seek", Add(field("seek"), Int(1)))
		})
		lo := field("seek")
		b.If(Bin("||", Eq(lo, n), Bin("!=", C("run.oldStart", r, lo), at)), func(b *Builder) { b.Return(BoolC(false)) })
		// The scan: every old element from it on examined only input after the edit. An old element that
		// examined input before the end of the edit bars every element up to it, which the later searches
		// pass, so the scan starts past every element that an earlier scan of the parse reached.
		b.If(Lt(lo, field("barred")), func(b *Builder) { b.Return(BoolC(false)) })
		b.Var("q", TInt)
		q := L("q")
		b.For("after", Assign("q", lo), Lt(q, n), Assign("q", Add(q, Int(1))), func(b *Builder) {
			b.If(Bin("||", F(exec.Budget), F(exec.Units)), func(b *Builder) { b.Do("budget.poll") })
			b.If(Lt(C("run.oldFrom", r, q), field("editEnd")), func(b *Builder) {
				set(b, "barred", Add(q, Int(1)))
				b.Return(BoolC(false))
			})
		})
		set(b, "j", lo)
		b.Return(BoolC(true))
	})
	resync.Doc = `runResync reports whether the run r is back in step with its old run after the edit: an old element
starts where the next element would, moved by the edit, and it and every old element after it examined
only input after the edit; their values move with the edit only if the run is shiftable. runTail then
reuses them.`

	tail := m.AddFunc("runTail", []Param{P("r", run)}, []Type{value, TBool}, feats, func(b *Builder) {
		i := b.Let("i", Add(field("j"), field("t")))
		b.If(Bin("||", Lt(field("j"), Int(0)), Bin(">=", i, C("run.oldCount", r))), func(b *Builder) { b.Return(Null(value), BoolC(false)) })
		b.If(Not(C("runMore", r)), func(b *Builder) { b.Return(Null(value), BoolC(false)) })
		v := b.Let("v", C("runReuse", r, i, field("editDelta"), Bin("-", C("edit.count", C("doc.log")), Int(1))))
		set(b, "t", Add(field("t"), Int(1)))
		b.Return(v, BoolC(true))
	})
	tail.Doc = `runTail reuses the next old element after the edit that runResync found the run r in step with, moved by
the edit, and returns its value and true; it returns false at the end of the old run or when the run may
take no more elements.`

	next := m.AddFunc("runNext", []Param{P("r", run)}, []Type{value, TBool}, feats, func(b *Builder) {
		b.If(Not(field("prefixed")), func(b *Builder) {
			b.LetN([]string{"pv", "pok"}, C("runPrefix", r))
			b.If(L("pok"), func(b *Builder) { b.Return(L("pv"), BoolC(true)) })
			set(b, "prefixed", BoolC(true))
		})
		b.If(And(Lt(field("j"), Int(0)), Not(C("runResync", r))), func(b *Builder) { b.Return(Null(value), BoolC(false)) })
		b.LetN([]string{"tv", "tok"}, C("runTail", r))
		b.Return(L("tv"), L("tok"))
	})
	next.Doc = `runNext reuses the next element of the run r that the old run supplies, when runMore(r) holds, and returns
its value and true: first the reused prefix (runPrefix), then, once the run is back in step with the old
run after the edit (runResync), the old elements after it (runTail). It returns false when the
repetition evaluates its next element instead.`

	begin := m.AddFunc("runBegin", []Param{P("r", run)}, nil, feats, func(b *Builder) {
		set(b, "elemStart", R(exec.Pos))
		b.LetN([]string{"oe", "ol"}, C("doc.enter", R(exec.Pos)))
		set(b, "outerExamined", L("oe"))
		set(b, "outerLow", L("ol"))
		// The mark is bound before it is stored, as the memoized call binds its own, for the targets that
		// hold a mark as its parts.
		m := b.Let("m", C("exp.isolate", R(exec.Pos)))
		set(b, "mark", m)
	})
	begin.Doc = `runBegin starts evaluating an element of the run r: its examined input and its expectations are recorded
apart from those of the enclosing call, so that the run can record them with the element.`

	end := m.AddFunc("runEnd", []Param{P("r", run), P("ok", TBool), P("v", value)}, nil, feats, func(b *Builder) {
		free(b, func(b *Builder) {
			b.LetN([]string{"far", "inner"}, C("exp.unisolate", field("mark")))
			b.Var("kept", kept)
			b.If(L("ok"), func(b *Builder) { b.Set("kept", C("keep", L("inner"))) })
			b.Do("mergeExpected", L("far"), L("inner"))
			from := b.Let("from", C("doc.low"))
			examined := b.Let("examined", C("doc.examined"))
			b.Do("doc.leave", field("outerExamined"), field("outerLow"))
			b.If(L("ok"), func(b *Builder) {
				set(b, "count", Add(field("count"), Int(1)))
				b.Do("run.add", r, Bin(">=", field("j"), Int(0)), field("elemStart"), R(exec.Pos), from, examined, L("far"), L("kept"), L("v"))
			})
		})
	})
	end.Doc = `runEnd ends evaluating an element of the run r, with its success ok and value v, before the parse resets
its state if it failed: it adds the element's examined input and expectations to those of the enclosing
call, and records the element if it matched.`

	finish := m.AddFunc("runFinish", []Param{P("r", run)}, nil, feats, func(b *Builder) {
		b.If(Bin("||", Lt(field("count"), C("run.minimum")), Not(C("run.clean", r))), func(b *Builder) { b.Return() })
		b.Do("run.record", r)
	})
	finish.Doc = `runFinish records the run r of a repetition that matched, for the rest of this parse and the next one,
unless it has fewer elements than the least a Document records, a recovered error remains, or a call in it
read the current result of a growth.`
}
