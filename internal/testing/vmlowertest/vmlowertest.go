// Package vmlowertest gives the tests of the bytecode lowering (internal/backend/vmlower) and of the
// VM what the lowering takes, as the VM pipeline of the driver builds it.
package vmlowertest

import (
	"context"
	"sync"
	"testing"

	"syntaxir.dev/sxir/internal/backend/vmlower"
	"syntaxir.dev/sxir/internal/driver"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/frontend"
	"syntaxir.dev/sxir/internal/ir/code"
	"syntaxir.dev/sxir/internal/middleend/flowlower"
	"syntaxir.dev/sxir/internal/middleend/flowpass"
	"syntaxir.dev/sxir/internal/middleend/flowplan/plandata"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/internal/pass/pipe"
	"syntaxir.dev/sxir/internal/testing/pipetest"
	"syntaxir.dev/sxir/pkg/bytecode"
)

// Input returns the input of the bytecode lowering of prog, the parser program of its grammar, at
// the settings s: the table that the VM target of the driver lowers, which an observer of the
// compilation keeps as flowlower makes it, with its module as flow-finish ends it. The compilation
// runs to its end, so a program whose lowering fails fails the test.
func Input(tb testing.TB, prog *frontend.Result, s pass.Settings) vmlower.Input {
	tb.Helper()
	var mu sync.Mutex
	var table *plandata.Table[code.Flow]
	var module code.At[code.Flow]
	watch := &pipetest.Watch{Units: true, Fn: func(e pipe.Event, r *pipe.Result) {
		if r.Err != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch e.Step.Name {
		case flowlower.Name:
			table = r.Unit.(*plandata.Table[code.Flow])
		case flowpass.FinishName:
			module = r.Unit.(code.At[code.Flow])
		}
	}}
	p := driver.ProgramOf(prog, driver.Parser, prog.Opts)
	if _, err := driver.Compile(context.Background(), p, driver.Options{Settings: s, Observers: []pipe.Observer{watch}}, driver.Target{Backend: result.VM}); err != nil {
		tb.Fatal(err)
	}
	if table == nil || module.Module() == nil {
		tb.Fatal("the VM pipeline lowered no table")
	}
	t, err := table.With(module)
	if err != nil {
		tb.Fatal(err)
	}
	return vmlower.Input{Run: prog.Runtime(), Elab: prog, Table: t}
}

// Lower runs the bytecode lowering (vmlower.Lowering) on in, as the VM pipeline ends, and returns the
// module and the matches of each pattern of statements of the selection table, which the step
// counts.
func Lower(in vmlower.Input) (*bytecode.Module, map[string]int, error) {
	var counts map[string]int
	watch := &pipetest.Watch{Fn: func(e pipe.Event, r *pipe.Result) {
		if e.Step.Name == vmlower.Name {
			counts = r.Counters
		}
	}}
	out, err := pipe.Execute(context.Background(), &pipe.Run{Observers: []pipe.Observer{watch}}, pipe.Of(vmlower.Lowering), in)
	if err != nil {
		return nil, nil, err
	}
	return out.(*vmlower.Bytecode).Module, counts, nil
}
