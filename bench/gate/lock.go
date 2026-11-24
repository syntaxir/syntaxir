package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// locker holds the benchmark lock while the timing phase of a plan runs.
type locker interface {
	hold(planFile string) error
}

// execLocker takes the lock the way the team does, with .agents/scripts/benchlock.sh (exclusive: a timed run is
// alone on the machine): it starts this program again under the script with the plan in the environment, and
// the script waits for its turn. While it waits, the holders are printed from the queue status.
type execLocker struct{ root string }

func (l execLocker) hold(planFile string) error {
	lock := filepath.Join(l.root, ".agents", "scripts", "benchlock.sh")
	if _, err := os.Stat(lock); err != nil {
		return fmt.Errorf("the benchmark lock script is missing: %w", err)
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if status := filepath.Join(l.root, ".agents", "scripts", "queue-status.sh"); fileExists(status) {
		out, _ := exec.Command(status).CombinedOutput()
		fmt.Fprintf(os.Stderr, "waiting for the benchmark lock; the queue now:\n%s\n", strings.TrimSpace(string(out)))
	}
	cmd := exec.Command(lock, "bench-gate", self)
	cmd.Env = append(os.Environ(), planEnv+"="+planFile)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	activeMu.Lock()
	activeChild = cmd.Process
	activeMu.Unlock()
	err = cmd.Wait()
	activeMu.Lock()
	activeChild = nil
	activeMu.Unlock()
	return err
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// directLocker holds nothing and runs the timing phase in this process: for the tests of the gate.
type directLocker struct {
	out io.Writer
	run runner
}

func (l directLocker) hold(planFile string) error {
	return timePlanWith(planFile, l.out, l.run)
}
