package tooltest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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
	node := RequireNode(t)
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
		{"exit code", `process.exitCode = 2;`, nil, "", "", 2},
		{"bare exit uses the exit code", `process.exitCode = 4; process.exit();`, nil, "", "", 4},
		{"an exit that the script caught", `try { console.log("checked"); process.exit(3); } catch {} console.log("after");`, nil, "checked\n", "", 3},
		{"the first exit decides", `try { process.exit(5); } catch {} process.exit(0);`, nil, "", "", 5},
		{"a late log", `setTimeout(() => console.log("late"), 30); console.log("early");`, nil, "early\nlate\n", "", 0},
		{"a late throw", `setTimeout(() => { throw new Error("late throw"); }, 20);`, nil, "", "late throw", 1},
		{"a late rejection", `setTimeout(() => { Promise.reject(new Error("late rejection")); }, 20);`, nil, "", "late rejection", 1},
		{"a late exit", `setTimeout(() => process.exit(6), 20);`, nil, "", "", 6},
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
			// The scripts that follow find the exit code and the output of their own.
			if next, err := RunNodeScript(t, node, poolScript(t, `console.log("next");`, nil), "main.ts"); err != nil || next.Code != 0 || next.Stdout != "next\n" || next.Stderr != "" {
				t.Errorf("the script after it: %+v, %v", next, err)
			}
			if res.Stdout != c.stdout || res.Code != c.code || !strings.Contains(res.Stderr, c.stderr) {
				t.Errorf("got stdout %q stderr %q code %d; want stdout %q stderr containing %q code %d", res.Stdout, res.Stderr, res.Code, c.stdout, c.stderr, c.code)
			}
		})
	}
}

// TestRunNodeScriptIndependence checks that scripts do not affect each other: a script that throws, one that
// exits and one that hangs, among scripts that succeed, fail alone and leave the others their own output, in
// the same pool and from many goroutines. The hang alone is cut short (RunNodeScriptWithin).
func TestRunNodeScriptIndependence(t *testing.T) {
	node := RequireNode(t)
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
			// Only the script that hangs on purpose is cut short; the others have the usual time, which a loaded
			// machine may need to start a worker.
			timeout := NodeJobTimeout
			if i%10 == 7 {
				timeout = 3 * time.Second
			}
			res, err := RunNodeScriptWithin(t, timeout, node, dir, "main.ts")
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
	node := RequireNode(t)
	const observe = `const seen = [typeof (globalThis as any).leaked, String(Array.prototype.indexOf).includes("native code"), typeof (Object.prototype as any).extra, String(Math.max).includes("native code"), String(process.env.LEAKED)];
console.log(seen.join(","));`
	const clean = "undefined,true,undefined,true,undefined\n"
	for _, c := range []struct {
		name, src, diff string
	}{
		{"a global", `(globalThis as any).leaked = 1;`, "+globalThis.leaked"},
		{"a replaced method", `const f = Array.prototype.indexOf; Array.prototype.indexOf = function (this: any, ...a: any[]) { return f.apply(this, a); };`, "Array.prototype.indexOf"},
		{"a new method of Object.prototype", `Object.defineProperty(Object.prototype, "extra", { value: 1, configurable: true });`, "+globalThis.Object.prototype.extra"},
		{"a changed built-in function", `(Math as any).max = () => 0;`, "Math.max"},
		{"the environment", `process.env.LEAKED = "yes";`, "+process.env.LEAKED"},
		{"a frozen prototype", `Object.freeze(Map.prototype);`, "Map.prototype"},
		{"a listener of process", `process.on("exit", () => {});`, "on exit"},
		{"a patched module", `import fs from "node:fs"; (fs as any).patched = 1;`, "fs.patched"},
		{"a replaced console method", `console.log = () => {};`, "globalThis.console.log"},
		{"a new property of console", `(console as any).extra = 1;`, "+globalThis.console.extra"},
		{"a patched typed array method", `(Int32Array.prototype as any).fill = function () { return this; };`, "Int32Array.prototype.fill"},
		{"a patched Atomics function", `(Atomics as any).add = () => 0;`, "globalThis.Atomics.add"},
		{"a patched DataView method", `(DataView.prototype as any).getInt8 = () => 0;`, "DataView.prototype.getInt8"},
		{"a patched ArrayBuffer method", `(ArrayBuffer.prototype as any).slice = () => null;`, "ArrayBuffer.prototype.slice"},
		{"a patched WeakRef method", `(WeakRef.prototype as any).deref = () => 1;`, "WeakRef.prototype.deref"},
		{"a replaced getter of Intl", `Object.defineProperty(Intl.Collator.prototype, "compare", { get() { return () => 0; }, configurable: true });`, "Intl.Collator.prototype.compare"},
		{"the process title", `process.title = "changed";`, "process.title"},
		{"the umask", `const mask = process.umask(0); process.umask(mask === 0o7 ? 0o70 : 0o7);`, "process.umask"},
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

// TestRunNodeScriptLeftoverResources checks that a script that leaves a timer running that does not end
// fails, and that its worker is replaced; a timer that ends is part of the script.
func TestRunNodeScriptLeftoverResources(t *testing.T) {
	node := RequireNode(t)
	old := NodeDrainTimeout
	NodeDrainTimeout = 400 * time.Millisecond
	defer func() { NodeDrainTimeout = old }()
	for _, src := range []string{`setInterval(() => console.log("tick"), 20);`, `setTimeout(() => console.log("never"), 600000);`} {
		res, err := RunNodeScript(t, node, poolScript(t, src, nil), "main.ts")
		if err == nil || !strings.Contains(err.Error(), "left active resources running: Timeout") {
			t.Fatalf("got %+v, %v; want the leftover timer reported", res, err)
		}
		for range 3 {
			res, err := RunNodeScript(t, node, poolScript(t, `console.log("next");`, nil), "main.ts")
			if err != nil || res.Stdout != "next\n" {
				t.Fatalf("a later script: %+v, %v", res, err)
			}
		}
	}
}

// TestRunNodeScriptCleanScriptsLeaveNoTrace checks that scripts that use the language and the libraries
// that the harnesses use (modules, JSON, regular expressions, typed arrays, text encoding, Maps, classes,
// timers, console and process.stdout) are not mistaken for pollution.
func TestRunNodeScriptCleanScriptsLeaveNoTrace(t *testing.T) {
	node := RequireNode(t)
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
