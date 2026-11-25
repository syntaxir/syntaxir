// Package metrics is the format of the deterministic metrics report that bench/metrics writes and
// compares: counts that are the same on every run of a commit with one Go release and target,
// whatever the machine's load.
//
// A report is text. Lines that start with # are a header (provenance) and are not compared, except
// the toolchain line (Toolchain): allocation counts and sizes depend on the Go release, the target
// and its build settings, so two reports are comparable only when they have the same one. Every other line is one
// metric:
//
//	<key> <value> <policy>
//
// The key is one or more space-separated fields (the metric's name, then what it measures, such
// as the workload and the backend); the value is an integer (ratios are given in thousandths);
// the policy says how a change of the value is judged:
//
//	lower      lower is better; any increase is a regression
//	lower~N%   lower is better; an increase of more than N percent is a regression
//	lower~N    lower is better; an increase of more than N is a regression
//	equal      the value must not change
//	info       a change is listed but never a regression
//
// A policy can add ",max=M": a value above M is a regression whatever the base. The lines of a
// report are sorted by key, so that two reports diff line by line.
package metrics

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
)

// Kind is how a change of a metric is judged.
type Kind string

const (
	Lower Kind = "lower" // lower is better
	Equal Kind = "equal" // must not change
	Info  Kind = "info"  // listed, never a regression
)

// Policy is how a change of a metric is judged.
type Policy struct {
	Kind Kind
	// Tolerance is the relative increase that a Lower metric may have before it is a regression,
	// as a percentage, and Slack the absolute one; a metric has at most one of them, and with
	// neither it is exact.
	Tolerance float64
	Slack     int64
	// Max, when HasMax, is the largest value that is not a regression.
	Max    int64
	HasMax bool
}

func (p Policy) String() string {
	s := string(p.Kind)
	if p.Tolerance != 0 {
		s += "~" + strconv.FormatFloat(p.Tolerance, 'f', -1, 64) + "%"
	}
	if p.Slack != 0 {
		s += "~" + strconv.FormatInt(p.Slack, 10)
	}
	if p.HasMax {
		s += ",max=" + strconv.FormatInt(p.Max, 10)
	}
	return s
}

// ParsePolicy parses the policy field of a metric line.
func ParsePolicy(s string) (Policy, error) {
	var p Policy
	rest := s
	if i := strings.Index(rest, ",max="); i >= 0 {
		m, err := strconv.ParseInt(rest[i+len(",max="):], 10, 64)
		if err != nil {
			return Policy{}, fmt.Errorf("policy %q: bad max: %v", s, err)
		}
		p.Max, p.HasMax = m, true
		rest = rest[:i]
	}
	if i := strings.IndexByte(rest, '~'); i >= 0 {
		t := rest[i+1:]
		if pct, ok := strings.CutSuffix(t, "%"); ok {
			f, err := strconv.ParseFloat(pct, 64)
			if err != nil || f <= 0 || math.IsInf(f, 0) || math.IsNaN(f) {
				return Policy{}, fmt.Errorf("policy %q: bad tolerance", s)
			}
			p.Tolerance = f
		} else {
			n, err := strconv.ParseInt(t, 10, 64)
			if err != nil || n <= 0 {
				return Policy{}, fmt.Errorf("policy %q: bad tolerance", s)
			}
			p.Slack = n
		}
		rest = rest[:i]
	}
	p.Kind = Kind(rest)
	switch p.Kind {
	case Lower:
	case Equal, Info:
		if p.Tolerance != 0 || p.Slack != 0 {
			return Policy{}, fmt.Errorf("policy %q: only lower takes a tolerance", s)
		}
	default:
		return Policy{}, fmt.Errorf("policy %q: unknown kind %q (want lower, equal or info)", s, rest)
	}
	return p, nil
}

// Metric is one line of a report.
type Metric struct {
	Key    string // space-separated fields
	Value  int64
	Policy Policy
}

func (m Metric) String() string {
	return fmt.Sprintf("%s %d %s", m.Key, m.Value, m.Policy)
}

// New returns the metric with the key of the fields.
func New(value int64, p Policy, fields ...string) Metric {
	return Metric{Key: strings.Join(fields, " "), Value: value, Policy: p}
}

// Ratio returns num/den in thousandths, rounded down; a zero den counts as 1.
func Ratio(num, den uint64) int64 {
	return int64(num * 1000 / max(den, 1))
}

// Write writes the metrics sorted by key, after the header lines (each prefixed with "# ").
// It fails on two metrics with the same key.
func Write(w io.Writer, header []string, ms []Metric) error {
	ms = slices.Clone(ms)
	slices.SortFunc(ms, func(a, b Metric) int { return strings.Compare(a.Key, b.Key) })
	for i := 1; i < len(ms); i++ {
		if ms[i].Key == ms[i-1].Key {
			return fmt.Errorf("metric %q twice", ms[i].Key)
		}
	}
	bw := bufio.NewWriter(w)
	for _, h := range header {
		fmt.Fprintf(bw, "# %s\n", h)
	}
	for _, m := range ms {
		fmt.Fprintln(bw, m)
	}
	return bw.Flush()
}

// Toolchain is the header line that names the Go release, the target and the build settings that
// change the code the toolchain makes for it, such as
// "go go1.27.1 linux/amd64 GOAMD64=v1 CGO_ENABLED=1". A collector writes it, and Comparable checks
// it: the microarchitecture level (GOAMD64, GOARM64 and the like) changes the compiled code and the
// sizes of the binaries, and cgo the binaries and the runtime's allocations.
func Toolchain() string {
	s := fmt.Sprintf("go %s %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	settings := map[string]string{}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, kv := range bi.Settings {
			settings[kv.Key] = kv.Value
		}
	}
	for _, key := range toolchainSettings() {
		v, ok := settings[key]
		if !ok {
			v = "unknown"
		}
		s += " " + key + "=" + v
	}
	return s
}

// toolchainSettings are the build settings that Toolchain names: the microarchitecture level of the
// target, if its architecture has one, and cgo. A build records each that it sets (runtime/debug),
// and the go command sets them all.
func toolchainSettings() []string {
	level := map[string]string{
		"386": "GO386", "amd64": "GOAMD64", "arm": "GOARM", "arm64": "GOARM64",
		"mips": "GOMIPS", "mipsle": "GOMIPS", "mips64": "GOMIPS64", "mips64le": "GOMIPS64",
		"ppc64": "GOPPC64", "ppc64le": "GOPPC64", "riscv64": "GORISCV64", "wasm": "GOWASM",
	}[runtime.GOARCH]
	if level == "" {
		return []string{"CGO_ENABLED"}
	}
	return []string{level, "CGO_ENABLED"}
}

// Report is a parsed report: its header lines, without the "# ", and its metrics.
type Report struct {
	Header  []string
	Metrics []Metric
}

// Comparable returns an error unless the reports have the same toolchain line. Without one on
// either side they are not comparable either.
func Comparable(base, head Report) error {
	b, h := base.toolchain(), head.toolchain()
	switch {
	case b == "" || h == "":
		return fmt.Errorf("a report has no toolchain line (# go <release> <os>/<arch> <settings>)")
	case b != h:
		return fmt.Errorf("the reports come from different toolchains (base %q, head %q); allocation counts and sizes differ between them", b, h)
	}
	return nil
}

func (r Report) toolchain() string {
	for _, h := range r.Header {
		if strings.HasPrefix(h, "go ") {
			return h
		}
	}
	return ""
}

// Parse reads a report, skipping blank lines.
func Parse(r io.Reader) (Report, error) {
	var rep Report
	var ms []Metric
	seen := map[string]bool{}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if h, ok := strings.CutPrefix(line, "#"); ok {
			rep.Header = append(rep.Header, strings.TrimSpace(h))
			continue
		}
		f := strings.Fields(line)
		if len(f) < 3 {
			return Report{}, fmt.Errorf("line %d: want <key> <value> <policy>: %q", n, line)
		}
		v, err := strconv.ParseInt(f[len(f)-2], 10, 64)
		if err != nil {
			return Report{}, fmt.Errorf("line %d: bad value: %v", n, err)
		}
		p, err := ParsePolicy(f[len(f)-1])
		if err != nil {
			return Report{}, fmt.Errorf("line %d: %v", n, err)
		}
		m := Metric{Key: strings.Join(f[:len(f)-2], " "), Value: v, Policy: p}
		if seen[m.Key] {
			return Report{}, fmt.Errorf("line %d: metric %q twice", n, m.Key)
		}
		seen[m.Key] = true
		ms = append(ms, m)
	}
	rep.Metrics = ms
	return rep, sc.Err()
}

// Verdict is the outcome of the comparison of one metric.
type Verdict string

const (
	Same          Verdict = "same"
	Better        Verdict = "better"
	Changed       Verdict = "changed"    // an info metric changed
	Tolerated     Verdict = "within-tol" // a lower metric with a tolerance moved within it, either way
	Added         Verdict = "new"        // in the head only
	Worse         Verdict = "REGRESSED"
	OverMax       Verdict = "OVER-MAX"
	Missing       Verdict = "MISSING" // in the base only
	PolicyChanged Verdict = "POLICY"  // the head changed the policy of the metric
)

// Fails reports whether the verdict is a regression.
func (v Verdict) Fails() bool {
	return v == Worse || v == OverMax || v == Missing || v == PolicyChanged
}

// Row is the comparison of one metric. For an Added metric Base is zero; for a Missing one Head
// is.
type Row struct {
	Key        string
	Base, Head int64
	// Policy is the policy that judged the row: the base's, or the head's for an Added metric.
	// HeadPolicy is the head's, which differs from Policy only in a PolicyChanged row.
	Policy, HeadPolicy Policy
	Verdict            Verdict
}

// Delta is Head - Base.
func (r Row) Delta() int64 { return r.Head - r.Base }

// Percent is the delta relative to the base, in percent; it is 0 for a zero base.
func (r Row) Percent() float64 {
	if r.Base == 0 {
		return 0
	}
	return float64(r.Delta()) * 100 / float64(r.Base)
}

// Compare judges each metric of head against base, in key order, by the base's policy: a head
// cannot loosen the policy that judges it, such as an exact metric given a tolerance that hides
// its own regression. A metric whose policy the head changes is a PolicyChanged regression even
// when its value is judged no worse, so that a change of a policy is a change of its own, made
// and reviewed apart from the code it would judge. A ceiling is absolute: a value above the
// ceiling of either side is OverMax, whatever the base's value, and a metric that only the head
// has is held to the ceiling of its own policy.
func Compare(base, head []Metric) []Row {
	b := map[string]Metric{}
	for _, m := range base {
		b[m.Key] = m
	}
	var rows []Row
	for _, h := range head {
		bm, ok := b[h.Key]
		delete(b, h.Key)
		r := Row{Key: h.Key, Base: bm.Value, Head: h.Value, Policy: bm.Policy, HeadPolicy: h.Policy}
		switch {
		case !ok:
			r.Policy, r.Verdict = h.Policy, Added
		default:
			r.Verdict = judge(bm.Value, h.Value, bm.Policy)
			if r.Verdict != Worse && bm.Policy != h.Policy {
				r.Verdict = PolicyChanged
			}
		}
		if over(h.Value, h.Policy) || ok && over(h.Value, bm.Policy) {
			r.Verdict = OverMax
		}
		rows = append(rows, r)
	}
	for _, m := range b {
		rows = append(rows, Row{Key: m.Key, Base: m.Value, Policy: m.Policy, HeadPolicy: m.Policy, Verdict: Missing})
	}
	slices.SortFunc(rows, func(x, y Row) int { return strings.Compare(x.Key, y.Key) })
	return rows
}

// over reports whether v is above the ceiling of p.
func over(v int64, p Policy) bool { return p.HasMax && v > p.Max }

func judge(base, head int64, p Policy) Verdict {
	if head == base {
		return Same
	}
	switch p.Kind {
	case Info:
		return Changed
	case Equal:
		return Worse
	}
	// A move within the tolerance, either way, is the noise that the tolerance was set for.
	d := head - base
	if d < 0 {
		d = -d
	}
	if p.Tolerance > 0 && float64(d) <= float64(base)*p.Tolerance/100 || d <= p.Slack {
		return Tolerated
	}
	if head < base {
		return Better
	}
	return Worse
}

// WriteRows writes the rows as an aligned table, the changed ones only unless all is set, and
// returns the number of regressions.
func WriteRows(w io.Writer, rows []Row, all bool) (int, error) {
	bw := bufio.NewWriter(w)
	fails := 0
	fmt.Fprintf(bw, "%-10s %14s %14s %12s %9s  %s\n", "verdict", "base", "head", "delta", "delta%", "metric")
	for _, r := range rows {
		if r.Verdict.Fails() {
			fails++
		}
		if !all && r.Verdict == Same {
			continue
		}
		pct := "-"
		if r.Verdict != Added && r.Verdict != Missing && r.Base != 0 {
			pct = fmt.Sprintf("%+.2f", r.Percent())
		}
		policy := r.Policy.String()
		if r.HeadPolicy != r.Policy {
			policy += " -> " + r.HeadPolicy.String()
		}
		fmt.Fprintf(bw, "%-10s %14d %14d %+12d %9s  %s [%s]\n", r.Verdict, r.Base, r.Head, r.Delta(), pct, r.Key, policy)
	}
	return fails, bw.Flush()
}
