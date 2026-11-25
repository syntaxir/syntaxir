//go:build unix

package codegentest

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestSetGoCacheChild is the child process of TestSetGoCacheReapsKilledProcesses: it sets its
// build cache, reports the directory and waits to be killed.
func TestSetGoCacheChild(t *testing.T) {
	if os.Getenv("SXIR_GOCACHE_CHILD") == "" {
		t.Skip("runs only as the child of TestSetGoCacheReapsKilledProcesses")
	}
	SetGoCache()
	os.MkdirAll(os.Getenv("GOCACHE"), 0o755)
	os.Stdout.WriteString("CACHE " + os.Getenv("GOCACHE") + "\n")
	time.Sleep(time.Hour)
}

func startCacheChild(t *testing.T, tmp string) (*exec.Cmd, string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestSetGoCacheChild$")
	cmd.Env = append(os.Environ(), "SXIR_GOCACHE_CHILD=1", "TMPDIR="+tmp)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		if cache, ok := cutPrefix(sc.Text(), "CACHE "); ok {
			return cmd, cache
		}
	}
	t.Fatal("child did not report its cache")
	return nil, ""
}

func cutPrefix(s, p string) (string, bool) {
	if len(s) >= len(p) && s[:len(p)] == p {
		return s[len(p):], true
	}
	return "", false
}

// TestSetGoCacheReapsKilledProcesses checks that a build cache outlives neither its killed process
// nor the next SetGoCache, and that the cache of a running process is left alone.
func TestSetGoCacheReapsKilledProcesses(t *testing.T) {
	tmp := t.TempDir()
	_, liveCache := startCacheChild(t, tmp)
	killed, deadCache := startCacheChild(t, tmp)
	if err := killed.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	killed.Wait()
	for _, c := range []string{liveCache, deadCache} {
		if _, err := os.Stat(c); err != nil {
			t.Fatalf("cache %s missing before the reap: %v", c, err)
		}
	}
	reapCaches(filepath.Join(tmp, cacheRoot))
	if _, err := os.Stat(deadCache); !os.IsNotExist(err) {
		t.Errorf("cache of the killed process remains (stat error %v)", err)
	}
	if _, err := os.Stat(filepath.Dir(deadCache)); !os.IsNotExist(err) {
		t.Errorf("directory of the killed process remains (stat error %v)", err)
	}
	if _, err := os.Stat(liveCache); err != nil {
		t.Errorf("cache of the running process was removed: %v", err)
	}
}
