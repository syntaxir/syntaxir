package archtest

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The project was called PEGO and then ZeroParse before it became SyntaxIR, with the short forms
// pego and zp in commands, file extensions, packages, environment variables, build tags and
// identifiers. A rename that misses one of them leaves a command, a link or a name that no longer
// exists, and nothing else fails on it. So this test reads every tracked file and path and fails on
// each former name it finds.
//
// The full names are found anywhere, in any case. The short form zp (and ZP) is found as a token:
// where the byte before it is not an ASCII letter or digit, or follows the escape \n, \t or \r of
// a string literal, and in the identifiers that hold it mid-word before an upper-case letter or
// the end of the word (EncodingZP). Words that merely contain the letters, such as FuzzParse, are
// not names.
//
// A line that must keep a former name says why with the marker "keep-old-name:" (for example
// the mapping of a benchmark label that saved results still carry). formerNameExempt lists the
// files that are not read at all.

// formerNameExempt are the tracked files (path.Match patterns) whose contents are not checked; their
// paths still are.
var formerNameExempt = []string{
	// Recovery corpora: random inputs in which the letters occur by chance.
	"testdata/recovery/*.txt",
	// This test spells the former names.
	"internal/testing/archtest/names_test.go",
}

// formerNameMarker exempts the line that holds it.
const formerNameMarker = "keep-old-name:"

// integrityKey starts the base64 digest of a package in an npm lock file, in which the letters
// occur by chance; such a line is not read.
const integrityKey = `"integrity": "sha`

// formerFullName matches the former full name in any case.
var formerFullName = regexp.MustCompile(`(?i)` + "zero" + "parse")

// formerOldest holds the forms of the oldest name, which prefixed commands, extensions, packages
// and identifiers as the short form did: pegort, .pegoc, pegoGrammar, PegoHooks, PEGOC.
var formerOldest = [3]string{"pe" + "go", "Pe" + "go", "PE" + "GO"}

// formerShort is the short form, in both cases.
var formerShort = [2]string{"z" + "p", "Z" + "P"}

func asciiAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// formerNames returns the former names in s, each with a little of its context.
func formerNames(s string) []string {
	var found []string
	context := func(i, n int) string {
		lo, hi := max(i-12, 0), min(i+n+12, len(s))
		return strings.TrimSpace(s[lo:hi])
	}
	for _, m := range formerFullName.FindAllStringIndex(s, -1) {
		found = append(found, context(m[0], m[1]-m[0]))
	}
	at := func(s string, i int) bool {
		return i == 0 || !asciiAlnum(s[i-1]) ||
			i >= 2 && s[i-2] == '\\' && strings.IndexByte("ntr", s[i-1]) >= 0
	}
	for _, form := range formerOldest {
		for off := 0; ; {
			j := strings.Index(s[off:], form)
			if j < 0 {
				break
			}
			i := off + j
			off = i + len(form)
			// A capitalized form also counts mid-word after camelCase (newPegoParser). The
			// upper-case form followed by a lower-case letter is another word that begins with
			// the letters PEG, such as PEGOperator.
			midWord := form != formerOldest[0] && i > 0 && s[i-1] >= 'a' && s[i-1] <= 'z'
			other := form == formerOldest[2] && off < len(s) && s[off] >= 'a' && s[off] <= 'z'
			if (at(s, i) || midWord) && !other {
				found = append(found, context(i, len(form)))
			}
		}
	}
	for _, short := range formerShort {
		for off := 0; ; {
			j := strings.Index(s[off:], short)
			if j < 0 {
				break
			}
			i := off + j
			off = i + len(short)
			start := at(s, i)
			// Mid-word upper-case form: camelCase before it, and an upper-case letter or the
			// end of the word after it.
			midWord := short == formerShort[1] && i > 0 && (s[i-1] >= 'a' && s[i-1] <= 'z') &&
				(off == len(s) || !asciiAlnum(s[off]) || s[off] >= 'A' && s[off] <= 'Z')
			if start || midWord {
				found = append(found, context(i, len(short)))
			}
		}
	}
	return found
}

// formerNameFindings checks the tracked files of the repository at root.
func formerNameFindings(root string) (findings []string, checked int, err error) {
	ls := exec.Command("git", "-C", root, "ls-files", "-z")
	ls.Env = repoEnv()
	out, err := ls.Output()
	if err != nil {
		return nil, 0, fmt.Errorf("git ls-files: %v", err)
	}
	for p := range strings.SplitSeq(string(out), "\x00") {
		if p == "" {
			continue
		}
		for _, n := range formerNames(p) {
			findings = append(findings, fmt.Sprintf("%s: the path holds a former name (%q)", p, n))
		}
		if exemptFromNames(p) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			continue // listed but deleted in the working tree
		}
		if bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 {
			continue // binary, as git decides
		}
		checked++
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, formerNameMarker) || strings.Contains(line, integrityKey) {
				continue
			}
			for _, n := range formerNames(line) {
				findings = append(findings, fmt.Sprintf("%s:%d: former name in %q", p, i+1, n))
			}
		}
	}
	return findings, checked, nil
}

func exemptFromNames(p string) bool {
	for _, pat := range formerNameExempt {
		if ok, _ := path.Match(pat, p); ok {
			return true
		}
	}
	return false
}

// TestNoFormerNames fails on each former project name in a tracked file or path.
func TestNoFormerNames(t *testing.T) {
	findings, checked, err := formerNameFindings(moduleRoot)
	if err != nil {
		t.Fatal(err)
	}
	if checked < 1000 {
		t.Fatalf("checked only %d files; the listing of tracked files is wrong", checked)
	}
	for _, f := range findings {
		t.Error(f)
	}
	for _, pat := range formerNameExempt {
		if !strings.Contains(pat, "*") {
			if _, err := os.Stat(filepath.Join(moduleRoot, filepath.FromSlash(pat))); err != nil {
				t.Errorf("formerNameExempt lists %s, which does not exist; remove it", pat)
			}
		}
	}
}

// TestFormerNamesNegative runs the check on text that holds each kind of former name, and on text
// that only resembles one.
func TestFormerNamesNegative(t *testing.T) {
	z, Z := formerShort[0], formerShort[1]
	for _, s := range []string{
		z + " gen -g x." + z,
		"g." + z + "c",
		"cmd/" + z,
		Z + "_LONG",
		"-tags " + z + "_long",
		`"\t` + z + `Grammar"`,
		`"\n` + z + ` gen"`,
		"grammar.Encoding" + Z,
		"classify" + Z + "Tokens",
		Z + "Hooks",
		"Zero" + "Parse",
		"https://" + "zero" + "parse.dev/",
		"PE" + "GO",
		"pe" + "go.dev",
		"pe" + "gort.D(n)",
		"x.pe" + "goc",
		"pe" + "goGrammar",
		"Pe" + "goHooks",
		"PE" + "GOC",
		"newPe" + "goParser",
		`"\n` + "pe" + `go gen"`,
		"-tags " + "pe" + "go_long",
		"```" + z,
	} {
		if len(formerNames(s)) == 0 {
			t.Errorf("formerNames(%q) found nothing", s)
		}
	}
	for _, s := range []string{
		"FuzzParse",
		"gzip",
		"sxir gen -g x.sxir",
		"SyntaxIR",
		"SXIR_LONG",
		"PE" + "GOperator",
		"PE" + "Gopher",
		"LR" + z, // random corpus text: a letter before the short form
		"4" + z,
	} {
		if got := formerNames(s); len(got) != 0 {
			t.Errorf("formerNames(%q) = %q, want nothing", s, got)
		}
	}
}
