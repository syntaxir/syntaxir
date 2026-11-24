package codegentest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"syntaxir.dev/sxir/internal/testing/tooltest"
)

// poolScript writes the module of one script (main.ts and the extra files) and returns its directory.
func poolScript(t *testing.T, src string, extra map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{"package.json": `{"type": "module"}`, "main.ts": src}
	for k, v := range extra {
		files[k] = v
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestRunNodeScriptOutcomes checks what a script reports: its output, a file it reads from its own
// directory, a throw, an exit code and the output it wrote before failing.
func TestRunNodeScriptOutcomes(t *testing.T) {
	node := tooltest.RequireNode(t)
	for _, c := range []struct {
		name, src string
		extra     map[string]string
		stdout    string
		stderr    string // substring
		code      int
	}{
		{"output", `const n: number = 3; console.log("a" + n); process.stdout.write("b\n");`, nil, "a3\nb\n", "", 0},
		{"stderr", `console.error("warn"); console.log("x");`, nil, "x\n", "warn", 0},
		{"relative file", `import { readFileSync } from "node:fs"; console.log(readFileSync("data.txt", "utf8"));`, map[string]string{"data.txt": "hello"}, "hello\n", "", 0},
		{"throw", `console.log("before"); throw new Error("boom");`, nil, "before\n", "boom", 1},
		{"exit", `console.log("x"); process.exit(3);`, nil, "x\n", "", 3},
		{"rejection", `Promise.reject(new Error("late"));`, nil, "", "late", 1},
		{"argv", `console.log(process.argv.slice(2).join(","));`, nil, "p,q\n", "", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := poolScript(t, c.src, c.extra)
			var args []string
			if c.name == "argv" {
				args = []string{"p", "q"}
			}
			res, err := RunNodeScript(t, node, dir, "main.ts", args...)
			if err != nil {
				t.Fatal(err)
			}
			if res.Stdout != c.stdout || res.Code != c.code || !strings.Contains(res.Stderr, c.stderr) {
				t.Errorf("got stdout %q stderr %q code %d; want stdout %q stderr containing %q code %d", res.Stdout, res.Stderr, res.Code, c.stdout, c.stderr, c.code)
			}
		})
	}
}

// TestRunNodeScriptIndependence checks that scripts do not affect each other: a script that throws, one that
// exits and one that hangs, among scripts that succeed, fail alone and leave the others their own output, in
// the same pool and from many goroutines. The hang is cut at a shortened NodeJobTimeout.
func TestRunNodeScriptIndependence(t *testing.T) {
	node := tooltest.RequireNode(t)
	old := NodeJobTimeout
	NodeJobTimeout = 3 * time.Second
	defer func() { NodeJobTimeout = old }()
	const n = 30
	type outcome struct {
		res NodeResult
		err error
	}
	got := make([]outcome, n)
	var wg sync.WaitGroup
	for i := range n {
		src := fmt.Sprintf(`const leak = %d; console.log("job %d", leak);`, i, i)
		switch i % 10 {
		case 3:
			src = `throw new Error("mutant fails")`
		case 5:
			src = `process.exit(7)`
		case 7:
			src = `console.log("started"); while (true) {}`
		}
		dir := poolScript(t, src, nil)
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := RunNodeScript(t, node, dir, "main.ts")
			got[i] = outcome{res, err}
		}()
	}
	wg.Wait()
	for i, o := range got {
		switch i % 10 {
		case 3:
			if o.err != nil || o.res.Code != 1 || !strings.Contains(o.res.Stderr, "mutant fails") {
				t.Errorf("job %d: %+v, %v; want the throw", i, o.res, o.err)
			}
		case 5:
			if o.err != nil || o.res.Code != 7 {
				t.Errorf("job %d: %+v, %v; want exit 7", i, o.res, o.err)
			}
		case 7:
			if o.err == nil || !strings.Contains(o.err.Error(), "ran past") {
				t.Errorf("job %d: %+v, %v; want the hang reported", i, o.res, o.err)
			}
		default:
			if want := fmt.Sprintf("job %d %d\n", i, i); o.err != nil || o.res.Code != 0 || o.res.Stdout != want {
				t.Errorf("job %d: %+v, %v; want %q", i, o.res, o.err, want)
			}
		}
	}
}

// TestRunNodeScriptLeakedState checks that a script that changes the state of its worker, which the next
// script that runs in it would see, fails with a list of what it changed, and that the scripts that follow
// find the worker as a new one: each pollution is followed by scripts that observe it, which would run in
// the worker of the polluter (the pool reuses the last worker that finished) if it were not replaced.
func TestRunNodeScriptLeakedState(t *testing.T) {
	node := tooltest.RequireNode(t)
	const observe = `const seen = [typeof (globalThis as any).leaked, String(Array.prototype.indexOf).includes("native code"), typeof (Object.prototype as any).extra, String(Math.max).includes("native code"), String(process.env.LEAKED)];
console.log(seen.join(","));`
	const clean = "undefined,true,undefined,true,undefined\n"
	for _, c := range []struct {
		name, src, diff string
	}{
		{"a global", `(globalThis as any).leaked = 1;`, "+globalThis.leaked"},
		{"a replaced method", `const f = Array.prototype.indexOf; Array.prototype.indexOf = function (this: any, ...a: any[]) { return f.apply(this, a); };`, "Array.prototype.indexOf"},
		{"a new method of Object.prototype", `Object.defineProperty(Object.prototype, "extra", { value: 1, configurable: true });`, "+Object.prototype.extra"},
		{"a changed built-in function", `(Math as any).max = () => 0;`, "Math.max"},
		{"the environment", `process.env.LEAKED = "yes";`, "+env.LEAKED=yes"},
		{"a frozen prototype", `Object.freeze(Map.prototype);`, "Map.prototype"},
		{"a listener of process", `process.on("exit", () => {});`, "on exit"},
		{"a patched module", `import fs from "node:fs"; (fs as any).patched = 1;`, "fs.patched"},
		{"a replaced prototype", `Object.setPrototypeOf(Map.prototype, { x: 1 });`, "Map.prototype"},
	} {
		t.Run(c.name, func(t *testing.T) {
			res, err := RunNodeScript(t, node, poolScript(t, c.src, nil), "main.ts")
			if err == nil || !strings.Contains(err.Error(), "changed the state") || !strings.Contains(err.Error(), c.diff) {
				t.Fatalf("got %+v, %v; want an error that names %q", res, err, c.diff)
			}
			for range 4 {
				res, err := RunNodeScript(t, node, poolScript(t, observe, nil), "main.ts")
				if err != nil || res.Code != 0 || res.Stdout != clean {
					t.Fatalf("a later script saw %q (code %d, error %v); want %q", res.Stdout, res.Code, err, clean)
				}
			}
		})
	}
}

// TestRunNodeScriptCleanScriptsLeaveNoTrace checks that scripts that use the language and the libraries
// that the harnesses use (modules, JSON, regular expressions, typed arrays, text encoding, Maps, classes,
// timers, console and process.stdout) are not mistaken for pollution.
func TestRunNodeScriptCleanScriptsLeaveNoTrace(t *testing.T) {
	node := tooltest.RequireNode(t)
	src := `import { readFileSync } from "node:fs";
import { join } from "node:path";
class A { m = new Map<string, number>([["a", 1]]); }
const enc = new TextEncoder().encode("é"); const dec = new TextDecoder().decode(enc);
const re = /(\d+)/u.exec("a12"); const big = 10n ** 20n; const buf = Buffer.from("abc").toString("base64");
await new Promise((r) => setTimeout(r, 1));
console.log(JSON.stringify([new A().m.get("a"), dec, re?.[1], String(big), buf, join("a", "b"), typeof readFileSync, new Date(0).toISOString(), [3, 1, 2].sort().join()]));
console.error("e"); process.stdout.write("w\n");`
	for range 3 {
		res, err := RunNodeScript(t, node, poolScript(t, src, nil), "main.ts")
		if err != nil || res.Code != 0 || !strings.HasSuffix(res.Stdout, "w\n") {
			t.Fatalf("got %+v, %v", res, err)
		}
	}
}
