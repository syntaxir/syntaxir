package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// strategyTargets reads the rows of the target table of docs/strategy.md: the text of the measure
// and of the Credibility target of each.
func strategyTargets(t *testing.T) [][2]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "docs", "strategy.md"))
	if err != nil {
		t.Fatal(err)
	}
	var rows [][2]string
	in := false
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "| Measure (output class) | Credibility |"):
			in = true
		case in && strings.HasPrefix(line, "|:--"):
		case in && strings.HasPrefix(line, "| "):
			cells := strings.Split(strings.TrimSuffix(strings.TrimPrefix(line, "| "), " |"), " | ")
			if len(cells) < 4 {
				t.Fatalf("strategy table row has %d cells, want 4: %q", len(cells), line)
			}
			rows = append(rows, [2]string{cells[0], cells[1]})
		case in:
			return rows
		}
	}
	if !in {
		t.Fatal("docs/strategy.md has no target table (a header starting \"| Measure (output class) | Credibility |\")")
	}
	return rows
}

// TestScoreboardFollowsStrategy checks that the scoreboard has one entry for each row of the
// target table in docs/strategy.md, in its order, with the same measure and target text.
func TestScoreboardFollowsStrategy(t *testing.T) {
	rows := strategyTargets(t)
	if len(rows) != len(scoreboard) {
		t.Errorf("docs/strategy.md has %d target rows, the scoreboard has %d entries", len(rows), len(scoreboard))
	}
	for i, row := range rows {
		if i >= len(scoreboard) {
			t.Errorf("target row %d has no scoreboard entry: %q", i+1, row[0])
			continue
		}
		e := scoreboard[i]
		if e.measure != row[0] || e.target != row[1] {
			t.Errorf("scoreboard entry %d differs from the strategy row:\n  entry:    %q | %q\n  strategy: %q | %q", i+1, e.measure, e.target, row[0], row[1])
		}
	}
	for i := len(rows); i < len(scoreboard); i++ {
		t.Errorf("scoreboard entry %d has no row in docs/strategy.md: %q", i+1, scoreboard[i].measure)
	}
}

// TestScoreboardIsUpToDate checks that docs/scoreboard.md is what the saved results render.
func TestScoreboardIsUpToDate(t *testing.T) {
	data, tsData, _, err := loadResults(filepath.Join(root, "bench", "results.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := renderScoreboard(data, tsData)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "docs", "scoreboard.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Error("docs/scoreboard.md is out of date: run go run ./bench/report -scoreboard")
	}
}

// TestScoreboardStandings checks the standing of a row against its target on made-up results, and
// that a missing benchmark makes a row open and names the benchmark.
func TestScoreboardStandings(t *testing.T) {
	line := func(name string, ns, b, allocs float64) string {
		return "Benchmark" + name + "-16 10 " + strconv.FormatFloat(ns, 'f', -1, 64) + " ns/op " + strconv.FormatFloat(b, 'f', -1, 64) + " B/op " + strconv.FormatFloat(allocs, 'f', -1, 64) + " allocs/op\n"
	}
	data := "# date: 2026-01-01\n# commit: abc\n" +
		line("Recognize/JSON/generated", 300, 0, 0) + line("Recognize/JSON/json_Valid", 100, 0, 0)
	doc, err := renderScoreboard([]byte(data), nil)
	if err != nil {
		t.Fatal(err)
	}
	text := string(doc)
	for _, want := range []string{
		"| 3.00× | met |", // 300 / 100 is at the target of 3.0
		"not measured: the results lack ControlledParse/JSON/generated/codepoints | open |",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("scoreboard lacks %q:\n%s", want, text)
		}
	}
}
