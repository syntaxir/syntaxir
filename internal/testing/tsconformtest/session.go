package tsconformtest

import (
	"bufio"
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"zeroparse.dev/zp/internal/ir/code/exec/ref"
)

//go:embed harness.ts
var harness string

// Config is the part of ref.Config that the bindings of the library read.
type Config struct {
	Rules    int       `json:"rules"`
	Limit    uint64    `json:"limit"`
	MaxDepth int64     `json:"maxDepth"`
	Vars     []int64   `json:"vars"`
	RuleVars [][]int64 `json:"ruleVars"`
	// NoExpectations makes a parse that does not record expectations.
	NoExpectations bool `json:"noExpectations"`
	// CtxRules are the left-recursion shapes of the rules.
	CtxRules []CtxRule `json:"ctxRules"`
	// Observed makes a parse that is observed.
	Observed bool `json:"observed"`
	// RuleSpecs are the properties and the scripted bodies of the rules, with their seen indexes.
	RuleSpecs []RuleSpec `json:"ruleSpecs"`
	// Oprs are the Operators Rules, the rules of the indexes of RuleRef.
	Oprs []OprRule `json:"oprs"`
}

// RuleSpec is ref.RuleSpec as the bindings read it.
type RuleSpec struct {
	Memo       bool   `json:"memo"`
	DocMemo    bool   `json:"docMemo"`
	Leader     bool   `json:"leader"`
	Positional bool   `json:"positional"`
	Rec        bool   `json:"rec"`
	Captures   int64  `json:"captures"`
	Seen       int64  `json:"seen"`
	Body       []Eval `json:"body"`
}

// Eval is one evaluation of a scripted body (ref.Eval).
type Eval struct {
	OK       bool    `json:"ok"`
	Len      int64   `json:"len"`
	Charge   uint64  `json:"charge"`
	Errs     []int64 `json:"errs"`
	Captures []int64 `json:"captures"`
}

// CtxRule is the cycle of a rule and whether it is a leader.
type CtxRule struct {
	Cycle  int64 `json:"cycle"`
	Leader bool  `json:"leader"`
}

// OprOp is an operator of an Operators Rule.
type OprOp struct {
	Kind  string `json:"kind"`
	Assoc string `json:"assoc"`
	Level int64  `json:"level"`
	Scope int64  `json:"scope"`
}

// OprRule is an Operators Rule: its operators, the capture slots of its operand lines, and whether it skips trivia.
type OprRule struct {
	Prefix   []OprOp `json:"prefix"`
	Led      []OprOp `json:"led"`
	Operands []int64 `json:"operands"`
	Skip     bool    `json:"skip"`
}

func oprRules(rs []ref.OprRule) []OprRule {
	var out []OprRule
	for _, r := range rs {
		ops := func(os []ref.OprOp) []OprOp {
			var l []OprOp
			for _, o := range os {
				l = append(l, OprOp{Kind: o.Kind, Assoc: o.Assoc, Level: o.Level, Scope: o.Scope})
			}
			return l
		}
		out = append(out, OprRule{Prefix: ops(r.Prefix), Led: ops(r.Led), Operands: r.Operands, Skip: r.Skip})
	}
	return out
}

// ConfigOf returns the part of cfg that the bindings read.
func ConfigOf(cfg ref.Config) Config {
	return Config{Rules: cfg.Rules, Limit: cfg.Limit, MaxDepth: cfg.MaxDepth, Vars: cfg.Vars, RuleVars: cfg.RuleVars, NoExpectations: cfg.NoExpectations, CtxRules: ctxRules(cfg.CtxRules), RuleSpecs: ruleSpecs(cfg), Observed: cfg.Observed, Oprs: oprRules(cfg.Oprs)}
}

// Request is a case for the library: the trace to replay, on a parser made for Family with Config.
type Request struct {
	Family     string `json:"family"`
	Config     Config `json:"config"`
	Controlled bool   `json:"controlled"`
	// Options are the options of the family's parser, as harness.ts reads them.
	Options map[string]any `json:"options"`
	Entries []Entry        `json:"entries"`
}

// Response is what the library answered.
type Response struct {
	Answers []Answer `json:"answers"`
	// Error is a failure of the harness in Node, not a difference of the library.
	Error string `json:"error,omitempty"`
}

// Session is a Node process that holds the library and replays traces on it, one request at a time.
type Session struct {
	t      testing.TB
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    *bufio.Reader
	stderr *bytes.Buffer
}

// Sources are the files that a session runs: the library, the printed code of the operations that the
// bindings call, and the bindings.
type Sources struct {
	// Lib is the bundle of a generated parser that holds the whole runtime.
	Lib string
	// Printed is the code that the TypeScript printer prints for the operations (print.OperationMethods),
	// which extends the library's source.
	Printed string
	// Harness is the bindings of this package (harness.ts).
	Harness string
}

// DefaultSources returns the sources of a session on the library lib.
func DefaultSources(lib string) (Sources, error) {
	p, err := Printed()
	if err != nil {
		return Sources{}, err
	}
	return Sources{Lib: lib, Printed: p.Source, Harness: harness}, nil
}

// Start starts Node on the library source lib, the printed code of the operations and the bindings of
// this package. The process ends with the test.
func Start(t testing.TB, node, lib string) *Session {
	t.Helper()
	s, err := DefaultSources(lib)
	if err != nil {
		t.Fatal(err)
	}
	return StartSources(t, node, s)
}

// StartSources is Start with the sources src, which a test may change.
func StartSources(t testing.TB, node string, src Sources) *Session {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string]string{"package.json": `{"type": "module"}` + "\n", "parser.ts": src.Lib + "\n" + src.Printed, "harness.ts": src.Harness} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(node, "harness.ts")
	cmd.Dir = dir
	ownGroup(cmd)
	s := &Session{t: t, cmd: cmd, stderr: &bytes.Buffer{}}
	cmd.Stderr = s.stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	s.in, s.out = in, bufio.NewReaderSize(out, 1<<20)
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting %s: %v", node, err)
	}
	// The process ends with the test, whatever it does: a harness that does not end by itself after its input
	// closes is killed with its group, so that a failed or interrupted run leaves no Node behind.
	t.Cleanup(func() {
		s.in.Close()
		done := make(chan struct{})
		timer := time.AfterFunc(10*time.Second, func() { killGroup(s.cmd) })
		go func() { s.cmd.Wait(); close(done) }()
		<-done
		timer.Stop()
	})
	return s
}

// runTimeout is how long the library may take to answer one request. A request of the suites takes
// milliseconds; a request that takes longer is a loop in the code under test.
const runTimeout = 2 * time.Minute

// Run replays the request on the library.
func (s *Session) Run(req Request) (Response, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return Response{}, err
	}
	watchdog := time.AfterFunc(runTimeout, func() {
		s.t.Errorf("node did not answer in %v; its process group is killed", runTimeout)
		killGroup(s.cmd)
	})
	defer watchdog.Stop()
	if _, err := s.in.Write(append(data, '\n')); err != nil {
		return Response{}, fmt.Errorf("node: %v\n%s", err, clip(s.stderr.String()))
	}
	line, err := s.out.ReadBytes('\n')
	if err != nil {
		return Response{}, fmt.Errorf("node: %v\n%s", err, clip(s.stderr.String()))
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return Response{}, fmt.Errorf("node answered %q: %v", clip(string(line)), err)
	}
	if resp.Error != "" {
		return resp, fmt.Errorf("the harness in node failed: %s", resp.Error)
	}
	return resp, nil
}

func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 2000 {
		return s[:2000] + "..."
	}
	return s
}

func ctxRules(rs []ref.CtxRule) []CtxRule {
	var out []CtxRule
	for _, r := range rs {
		out = append(out, CtxRule{Cycle: r.Cycle, Leader: r.Leader})
	}
	return out
}

func ruleSpecs(cfg ref.Config) []RuleSpec {
	var out []RuleSpec
	for i, r := range cfg.RuleSpecs {
		s := RuleSpec{Memo: r.Memo, DocMemo: r.DocMemo, Leader: r.Leader, Positional: r.Positional, Rec: r.Rec,
			Captures: r.Captures, Seen: ref.SeenIndex(cfg.RuleSpecs, int64(i))}
		for _, e := range r.Body {
			s.Body = append(s.Body, Eval{OK: e.OK, Len: e.Len, Charge: e.Charge, Errs: e.Errs, Captures: e.Captures})
		}
		out = append(out, s)
	}
	return out
}
