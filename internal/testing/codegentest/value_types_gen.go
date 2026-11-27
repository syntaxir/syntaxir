package codegentest

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/frontend/check"
	"syntaxir.dev/sxir/internal/testing/compiletest"
)

// ReferenceTraced compiles src with opts and returns the traced parse of its rule main and the parse
// of any of its rules on the engine (TracedParse, RuleParse), for the checks of the values of the
// generated parsers. The test package sets it, as it sets Reference.
var ReferenceTraced func(t *testing.T, src string, opts check.Options) (TracedParse, RuleParse)

// ValueTypeMismatch starts the description of a value that a generated parser returned and that does
// not have the type of its rule, so that a negative control can tell it from a difference from the
// engine's value.
const ValueTypeMismatch = "value not of its type: "

// valueJob is a parse that the checks of the values of generated parsers run: of the rule Rule of the
// case Case, on Input, with positions in bytes and the limit of missing tokens Missing.
type valueJob struct {
	Case    int    `json:"c"`
	Rule    string `json:"r"`
	Input   []byte `json:"i"`
	Missing int    `json:"m"`
}

// valueJobs returns the parses that the checks of the values of the generated parsers of cases run,
// and the engine's result of each, as ResultJSON writes it: the parse of main on each input of each
// case, and the parse of each rule on each distinct part of an input that a successful evaluation of
// the rule matched in the engine's traced parse of the input, at each limit of missing tokens of
// the case (ValueTypeLimits).
func valueJobs(t *testing.T, cases []Case, opts common.GenOptions) (jobs []valueJob, want []string) {
	t.Helper()
	if ReferenceTraced == nil {
		t.Fatal("codegentest.ReferenceTraced is not set: the test package must set it to trace the engine")
	}
	for i, c := range cases {
		// The generated code projects (internal/frontend/check/project.go), and records source information as asked.
		traced, rule := ReferenceTraced(t, c.Src, check.Options{NoProjections: true, SourceInfo: opts.SourceInfo})
		add := func(j valueJob) {
			n, err := rule(j.Rule, string(j.Input), j.Missing)
			jobs = append(jobs, j)
			want = append(want, DeepResultJSON(t, n, err))
		}
		for _, missing := range ValueTypeLimits(t, c) {
			spans := map[string]bool{}
			for _, in := range c.Inputs {
				add(valueJob{i, "main", []byte(in), missing})
				traced(in, missing, func(call RuleCall) {
					if key := call.Rule + "\x00" + in[call.Start:call.End]; !spans[key] && call.Rule != "main" {
						spans[key] = true
					}
				})
			}
			for _, key := range slices.Sorted(maps.Keys(spans)) {
				r, text, _ := strings.Cut(key, "\x00")
				add(valueJob{i, r, []byte(text), missing})
			}
		}
	}
	return jobs, want
}

// valueJobMismatches compares the results got of the generated parsers on jobs, one line each as
// ResultJSON writes it, with the engine's (want), and checks the value of each against the type of
// its rule (ValueTypes, on the JSON form). A value not of its type is described with
// ValueTypeMismatch.
func valueJobMismatches(t *testing.T, backend string, cases []Case, jobs []valueJob, got, want []string) []string {
	t.Helper()
	if len(got) != len(jobs) {
		t.Fatalf("%s: got %d results, want %d", backend, len(got), len(jobs))
	}
	vts := make([]*ValueTypes, len(cases))
	var out []string
	checked := 0
	for k, j := range jobs {
		c := cases[j.Case]
		in := string(j.Input)
		if len(in) > 200 {
			in = in[:200] + "..."
		}
		if got[k] != want[k] && !sameJSON(got[k], want[k]) {
			at := DiffAt(got[k], want[k])
			out = append(out, fmt.Sprintf("%s: %s: rule %s, input %q, missing tokens %d\n generated %s\n engine    %s", backend, c.Name, j.Rule, in, j.Missing, Clip(got[k], at), Clip(want[k], at)))
		}
		var res struct {
			Node any
			Err  *string
		}
		dec := json.NewDecoder(strings.NewReader(got[k]))
		dec.UseNumber()
		if err := dec.Decode(&res); err != nil {
			t.Fatalf("%s: %s: %v: %s", backend, c.Name, err, Clip(got[k], 0))
		}
		if res.Node == nil && res.Err != nil {
			continue
		}
		if vts[j.Case] == nil {
			vts[j.Case] = ValueTypesOf(t, c)
		}
		checked++
		if err := vts[j.Case].CheckJSON(j.Rule, res.Node); err != nil {
			out = append(out, fmt.Sprintf("%s%s: %s: input %q, missing tokens %d: %v", ValueTypeMismatch, backend, c.Name, in, j.Missing, err))
		}
	}
	if checked == 0 {
		out = append(out, fmt.Sprintf("%s: no parse of %d succeeded, so no value was checked", backend, len(jobs)))
	}
	t.Logf("%s: checked the values of %d of %d parses of %d grammars", backend, checked, len(jobs), len(cases))
	return out
}

// sameJSON reports whether the JSON texts a and b encode the same value, with their numbers exact: the
// TypeScript harness writes the characters that encoding/json escapes, such as < and &, unescaped.
func sameJSON(a, b string) bool {
	decode := func(s string) (any, bool) {
		dec := json.NewDecoder(strings.NewReader(s))
		dec.UseNumber()
		var v any
		return v, dec.Decode(&v) == nil && !dec.More()
	}
	x, okA := decode(a)
	y, okB := decode(b)
	return okA && okB && reflect.DeepEqual(x, y)
}

// GeneratedValueTypesMismatches checks the values of the generated Go parsers of cases, generated with
// opts and their source changed by mutate (when not nil), against the types of their rules: the
// value of each parse of valueJobs, which differs from the engine's in no mismatch that it does not
// describe. The values are checked in the JSON form of value.Node, which tells no tuple from a list
// and holds no captures (ValueTypes).
func GeneratedValueTypesMismatches(t *testing.T, goBin string, cases []Case, opts common.GenOptions, mutate func([]byte) []byte) []string {
	t.Helper()
	jobs, want := valueJobs(t, cases, opts)
	dir := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", GoModAt("codegentest", LibCopy()))
	var imports, parsers strings.Builder
	for i, c := range cases {
		g, err := compiletest.Parse(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		opts.Package, opts.Start = fmt.Sprintf("g%d", i), "main"
		code, err := GenerateCode(t, g, opts)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		if mutate != nil {
			code = mutate(code)
		}
		write(fmt.Sprintf("g%d/parser.go", i), string(code))
		fmt.Fprintf(&imports, "\tg%d \"codegentest/g%d\"\n", i, i)
		fmt.Fprintf(&parsers, "\tfunc(r, s string, m int) (any, error) { n, err := g%d.ParseRule(r, s, g%[1]d.WithUnit(g%[1]d.Bytes), sxirruntime.WithMissingTokens(m)); return n, err },\n", i)
	}
	write("main.go", `package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	sxirruntime "syntaxir.dev/sxir/pkg/runtime"
`+imports.String()+`)

var parsers = []func(string, string, int) (any, error){
`+parsers.String()+`}

func main() {
	var jobs []struct {
		C int    `+"`json:\"c\"`"+`
		R string `+"`json:\"r\"`"+`
		I []byte `+"`json:\"i\"`"+`
		M int    `+"`json:\"m\"`"+`
	}
	data, _ := os.ReadFile(os.Args[1])
	if err := json.Unmarshal(data, &jobs); err != nil {
		panic(err)
	}
	w := bufio.NewWriter(os.Stdout)
	for _, j := range jobs {
		n, err := parsers[j.C](j.R, string(j.I), j.M)
		out := map[string]any{"node": n}
		if err != nil {
			out["err"] = err.Error()
		}
		b, _ := json.Marshal(out)
		fmt.Fprintln(w, string(b))
	}
	w.Flush()
}
`)
	data, _ := json.Marshal(jobs)
	write("jobs.json", string(data))
	cmd := exec.Command(goBin, "run", CorpusGCFlags, ".", "jobs.json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), GoFlagsEnv, "GOTOOLCHAIN=local")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	got := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	return valueJobMismatches(t, "Go", cases, jobs, got, want)
}

// tsValueHarness runs the parses of jobs.json with the TypeScript parsers g<i>.ts and writes the
// result of each as ResultJSON writes it, in a worker with a large stack, as TSHarness does.
const tsValueHarness = `import { Worker, isMainThread, workerData } from "node:worker_threads";
import { closeSync, openSync, readFileSync, writeSync } from "node:fs";

if (isMainThread) {
  const w = new Worker(new URL(import.meta.url), { workerData: { jobs: process.argv[2], out: process.argv[3] }, resourceLimits: { stackSizeMb: 1024 } });
  w.on("error", (e) => {
    console.error(e);
    process.exit(1);
  });
  w.on("exit", (code) => process.exit(code));
} else {
  const jobs = JSON.parse(readFileSync(workerData.jobs, "utf8"));
  const out = openSync(workerData.out, "w");
  const mods = [];
  for (const j of jobs) {
    const m = (mods[j.c] ??= await import("./g" + j.c + ".ts"));
    const input = new Uint8Array(Buffer.from(j.i, "base64"));
    const r = m.parseRuleWithOptions(j.r, input, j.m > 0 ? { unit: m.Bytes, missingTokens: j.m } : { unit: m.Bytes });
    const node = m.marshal(r.node);
    writeSync(out, (r.error === null ? '{"node":' + node : '{"err":' + JSON.stringify(r.error.message) + ',"node":' + node) + "}\n");
  }
  closeSync(out);
}
`

// TSValueTypesMismatches is GeneratedValueTypesMismatches for the TypeScript parsers, generated with
// opts and their source changed by mutate (when not nil), run with node. The JSON form of a TypeScript
// value writes every number alike, so a number is an int when it is an integer (ValueTypes).
func TSValueTypesMismatches(t *testing.T, node string, cases []Case, opts common.GenOptions, mutate func(string) string) []string {
	t.Helper()
	jobs, want := valueJobs(t, cases, opts)
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"type": "module"}`+"\n")
	write("harness.mjs", tsValueHarness)
	opts.Start = "main"
	for i, c := range cases {
		g, err := compiletest.Parse(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		code, err := GenerateTSBundle(g, opts)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		if mutate != nil {
			code = []byte(mutate(string(code)))
		}
		write(fmt.Sprintf("g%d.ts", i), string(code))
	}
	type tsJob struct {
		C int    `json:"c"`
		R string `json:"r"`
		I string `json:"i"`
		M int    `json:"m"`
	}
	ts := make([]tsJob, len(jobs))
	for k, j := range jobs {
		ts[k] = tsJob{j.Case, j.Rule, base64.StdEncoding.EncodeToString(j.Input), j.Missing}
	}
	data, _ := json.Marshal(ts)
	write("jobs.json", string(data))
	cmd := exec.Command(node, "harness.mjs", "jobs.json", "out.jsonl")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	f, err := os.Open(filepath.Join(dir, "out.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var got []string
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<30)
	for sc.Scan() {
		got = append(got, sc.Text())
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return valueJobMismatches(t, "TypeScript", cases, jobs, got, want)
}

// ValueTypesMutantCase is the grammar of the negative controls of the checks of the values of
// generated parsers: a struct built from a capture of a named node type and one of an int.
var ValueTypesMutantCase = Case{Name: "values of a named node type and an int in a struct", Src: `type S = { A node[string]; N int }
type Id = node[string]
rule main S = a:id n:num => S{A: $a, N: $n}
rule id Id = ([a-z]+) #atomic
rule num = [0-9] => 1`, Inputs: []string{"ab1", "x7"}}

// RequireValueTypeMismatch fails t unless mismatches, of a negative control named name, describe a
// value not of its type (ValueTypeMismatch).
func RequireValueTypeMismatch(t *testing.T, name string, mismatches []string) {
	t.Helper()
	if !slices.ContainsFunc(mismatches, func(m string) bool { return strings.HasPrefix(m, ValueTypeMismatch) }) {
		t.Errorf("%s: no value is reported not of its type; the mismatches are:\n%s", name, strings.Join(mismatches, "\n"))
	}
}
