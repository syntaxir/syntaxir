package sxirls

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNeovimClient edits a grammar through Neovim's built-in client, with the synchronous and the
// responsive server, and checks that the server's text follows every edit. It needs Neovim 0.11 or
// later and runs only with SXIR_LSP_NEOVIM=1.
func TestNeovimClient(t *testing.T) {
	if os.Getenv("SXIR_LSP_NEOVIM") != "1" {
		t.Skip("set SXIR_LSP_NEOVIM=1 to run Neovim")
	}
	nvim, err := exec.LookPath("nvim")
	if err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs(filepath.Join("testdata", "neovim_client.lua"))
	if err != nil {
		t.Fatal(err)
	}
	sxirBin := filepath.Join(t.TempDir(), "sxir")
	build := exec.Command("go", "build", "-o", sxirBin, "../../../cmd/sxir")
	build.Env = append(os.Environ(), "GOFLAGS="+os.Getenv("GOFLAGS"))
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build sxir: %v\n%s", err, out)
	}
	for _, responsive := range []string{"0", "1"} {
		t.Run("responsive="+responsive, func(t *testing.T) {
			cmd := exec.Command(nvim, "--headless", "--clean", "-l", script, t.TempDir())
			cmd.Env = append(os.Environ(), "SXIR="+sxirBin, "RESPONSIVE="+responsive)
			out, err := cmd.CombinedOutput()
			t.Logf("%s", out)
			if err != nil || !strings.Contains(string(out), "\nPASS\n") {
				t.Fatalf("Neovim client: %v", err)
			}
		})
	}
}
