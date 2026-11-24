package state

import "sync/atomic"

// The thresholds of the charge model ("Run Sites" and "Scan Sites" in spec/runtime/parsing.md). They
// decide which walks look up and store records and which runs a Document records, so they decide
// the charged units; every backend uses the same values, and no option changes them.
const (
	// runHotSpan is the length of input that a walk of a run site may walk again, below the
	// furthest point that the walks before it reached, before the site turns hot. A walk shorter
	// than that neither turns a site hot nor can walk that far again, so a caller need not pass it
	// to RunWalked.
	runHotSpan = 64
	// scanHotSpan is the number of characters that a walk of a scan site matches before it looks
	// for the record of its start, and the length of a walk that can turn the site hot. A smaller
	// value lowers the cost of each call that a record answers and raises the cost of the walks that
	// find none: the steps of a parse with records exceed those of a parse without by two for each
	// walk of at least scanHotSpan characters. It equals flow.ScanHotSpan, which the Flow lowering
	// writes into generated code.
	scanHotSpan = 16
	// minRecordedLen is the number of elements below which a Document does not record a run: a
	// short repetition costs little to run again. It equals exec.MinRecordedRun, which the Go library
	// of generated parsers is printed with.
	minRecordedLen = 16
)

// Tuning holds the test-only settings of one parse: thresholds lowered below those of the charge
// model, so that short inputs reach the hot paths, and switches that turn records off or break one
// check on purpose, for the negative controls. A parse reads the settings of its own Parser only,
// and the zero value, which every parse outside tests has, selects the charge model and the
// correct algorithm. Nothing in a parse writes them.
type Tuning struct {
	// RunHot, when not 0, replaces the run-site hot span (runHotSpan).
	RunHot int
	// ScanHot, when not 0, replaces the scan-site hot span (scanHotSpan).
	ScanHot int
	// MinRec, when not 0, replaces the number of elements below which a run is not recorded
	// (minRecordedLen).
	MinRec int
	// SkipOff disables skip-run, run-site and scan-site records, for tests that compare parses
	// with and without them.
	SkipOff bool
	// SkipMut, when not 0, breaks the records: a reused record leaves out one part of what it
	// records (SkipKeepPos and the other Skip constants), or walks never record (SkipNeverRecord).
	SkipMut int
	// RunMut, when not 0, breaks the condition on seed reads (RunOpenGrowthsOnly) or on insertions
	// (RunIgnoresInsertions) under which a run is recorded.
	RunMut int
	// MemoMut, when not 0, breaks one check of the dependency memo (the Ctx constants name the
	// checks).
	MemoMut int
	// MissingMut, when not 0, breaks the missing-token limit of a stream parse
	// (MissingLimitPerElement).
	MissingMut int
	// Stats, when not nil, counts the sites that turn hot and the walks answered from a record in
	// the parses that carry it, for tests that check a parse reaches the hot paths. Nothing in a
	// parse reads it.
	Stats *HotStats
}

// HotStats counts what the hot paths of the parses that share it did (Tuning.Stats). The counters
// are atomic because the parses of one test may run concurrently; the tests of a package each
// make their own, so that parallel tests do not see each other's parses.
type HotStats struct {
	// RunHotTurns counts the run sites that turned hot: once for each site and lookahead state of
	// a parse, so counting it costs nothing on the hot path.
	RunHotTurns atomic.Int64
	// ScanHits counts the walks answered from a record.
	ScanHits atomic.Int64
}

// MissingLimitPerElement gives each element of a stream the whole missing-token limit, rather
// than the stream one limit (Tuning.MissingMut).
const MissingLimitPerElement = 1

// RunHotSpan returns the run-site hot span of the parse; that of the zero value is the charge
// model's.
func (t *Tuning) RunHotSpan() int {
	if t.RunHot == 0 {
		return runHotSpan
	}
	return t.RunHot
}

// ScanHotSpan returns the scan-site hot span of the parse; that of the zero value is the charge
// model's.
func (t *Tuning) ScanHotSpan() int {
	if t.ScanHot == 0 {
		return scanHotSpan
	}
	return t.ScanHot
}

// minRecorded returns the number of elements below which a run is not recorded in the parse.
func (t *Tuning) minRecorded() int {
	if t.MinRec == 0 {
		return minRecordedLen
	}
	return t.MinRec
}

// Tuning returns the settings of the parse, which the backends read for the hot spans.
func (p *Parser) Tuning() *Tuning { return &p.tune }

// SetTuning sets the settings of the parse. The engine sets them from ParseOptions.Tune, which only
// tests set, and the tests of this package set them on their own parsers.
func (p *Parser) SetTuning(t Tuning) { p.tune = t }
