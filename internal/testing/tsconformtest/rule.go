package tsconformtest

import (
	"zeroparse.dev/zp/internal/ir/code/exec/conform"
	"zeroparse.dev/zp/internal/ir/code/exec/ref"
)

// RuleOps are the operations of the rules that the library implements: those of conform.RuleOps and the
// cases' depth and recovered-error operations, without the operations of a Document parse (rule.docPlain,
// the statistics) and the invocation that generated calls pass (rule.run).
var RuleOps = []string{
	"rule.memo", "rule.leader", "rule.plain", "rule.docMemo", "rule.docOnlyMemo",
	"rule.positional", "rule.id", "rule.seen", "rule.invoke", "rule.invokePlain", "seen.defers",
	"depth.enter", "depth.exit", "rec.mark", "rec.truncate",
}

// Rule is the family of the rules: the descriptors of a rule and the invocation of its scripted body.
// The library's parser parses neither Documents nor streams, observes nothing and has no missing-token
// mode, so the cases of such a parse are skipped.
func Rule(controlled bool) Family {
	return Family{
		Name:       "rule",
		Controlled: controlled,
		Unsupported: func(c ref.Config) bool {
			return c.Document || c.Stream || c.Observed || c.Missing
		},
		Record: func(r *Recorder) conform.Impl { return ruleRecorder{execRecorder{r}} },
		Replay: func(p *Replay) conform.Impl {
			if controlled {
				return ruleMetered{ruleReplay{execReplay{p}}}
			}
			return ruleReplay{execReplay{p}}
		},
	}
}

type ruleRecorder struct{ execRecorder }

func (r ruleRecorder) UseRegisters(regs map[string]any) { r.Registers(regs) }
func (r ruleRecorder) Observe(component string) []string {
	return r.Model.Observe(component)
}

type ruleReplay struct{ execReplay }

func (r ruleReplay) UseRegisters(regs map[string]any)  { r.Registers(regs) }
func (r ruleReplay) Observe(component string) []string { return r.Observation(component) }

type ruleMetered struct{ ruleReplay }

func (r ruleMetered) Used() uint64 { return r.Units() }
