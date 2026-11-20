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
}

// ConfigOf returns the part of cfg that the bindings read.
func ConfigOf(cfg ref.Config) Config {
	return Config{Rules: cfg.Rules, Limit: cfg.Limit, MaxDepth: cfg.MaxDepth, Vars: cfg.Vars, RuleVars: cfg.RuleVars, NoExpectations: cfg.NoExpectations}
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

// Start starts Node on the library source lib (the bundle of a generated parser that holds the whole
// runtime) and the bindings of this package. The process ends with the test.
func Start(t testing.TB, node, lib string) *Session {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string]string{"package.json": `{"type": "module"}` + "\n", "parser.ts": lib, "harness.ts": harness} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(node, "harness.ts")
	cmd.Dir = dir
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
	t.Cleanup(func() {
		s.in.Close()
		s.cmd.Wait()
	})
	return s
}

// Run replays the request on the library.
func (s *Session) Run(req Request) (Response, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return Response{}, err
	}
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
