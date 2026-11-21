package tsconformtest

import (
	"syntaxir.dev/sxir/internal/ir/code/exec/conform"
)

// DocOps are the doc operations of the examined input of a Document parse, all of which the library
// implements: the Document bookkeeping of DocumentParser, and the examined end of the calls of an
// observed parse, which DocumentParser and ObservedParser both measure.
var DocOps = conform.DocOps

// DocClasses are the classes of the printed operations (print.OperationClasses) that a case of the doc
// operations runs on: the batch Parser of a parse from scratch, the DocumentParser of a Document parse and
// the ObservedParser of an observed parse without a Document, as harness.ts picks them for the case.
var DocClasses = []string{"PrintedParser", "PrintedDocumentParser", "PrintedObservedParser"}

// Doc is the family of the doc operations: the parser of a case is the library's parser of its parse.
func Doc() Family {
	return Family{
		Name:   "doc",
		Record: func(r *Recorder) conform.Impl { return docRecorder{execRecorder{r}} },
		Replay: func(p *Replay) conform.Impl { return docReplay{execReplay{p}} },
	}
}

type docRecorder struct{ execRecorder }

func (r docRecorder) UseRegisters(regs map[string]any) { r.Registers(regs) }

// DocStat logs the read of a statistic of the Document, which the harness makes outside the operations.
func (r docRecorder) DocStat(i int64) int64 {
	v := r.Model.DocStat(i)
	r.trace = append(r.trace, Entry{Op: conform.DocStatOp, Args: []any{i}, Res: []any{nil}})
	return v
}

type docReplay struct{ execReplay }

func (r docReplay) UseRegisters(regs map[string]any) { r.Registers(regs) }

// DocStat returns the statistic that the library answered.
func (r docReplay) DocStat(i int64) int64 {
	_, a := r.advance(conform.DocStatOp)
	if a.Err != "" {
		panic(a.Err)
	}
	return int64(a.Res[0].(float64))
}
