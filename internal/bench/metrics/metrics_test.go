package metrics

import (
	"bytes"
	"strings"
	"testing"
)

func TestPolicyRoundTrip(t *testing.T) {
	for _, s := range []string{"lower", "lower~0.5%", "lower~2%,max=2219", "lower,max=2219", "lower~600", "equal", "info"} {
		p, err := ParsePolicy(s)
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		if got := p.String(); got != s {
			t.Errorf("%s: printed as %s", s, got)
		}
	}
	for _, s := range []string{"", "higher", "equal~1%", "info~1%", "lower~1.5", "lower~-1%", "lower~0", "lower~0%", "lower~-3", "equal~2", "lower,max=x"} {
		if _, err := ParsePolicy(s); err == nil {
			t.Errorf("%q: no error", s)
		}
	}
}

func TestWriteParse(t *testing.T) {
	ms := []Metric{
		New(7, Policy{Kind: Lower}, "units", "JSON", "engine-closure"),
		New(1, Policy{Kind: Equal}, "determinism", "json", "go"),
		New(2001, Policy{Kind: Lower, HasMax: true, Max: 2219}, "linearity-units", "JSON"),
	}
	var b bytes.Buffer
	if err := Write(&b, []string{"commit abc"}, ms); err != nil {
		t.Fatal(err)
	}
	want := "# commit abc\ndeterminism json go 1 equal\nlinearity-units JSON 2001 lower,max=2219\nunits JSON engine-closure 7 lower\n"
	if b.String() != want {
		t.Fatalf("got\n%s\nwant\n%s", b.String(), want)
	}
	got, err := Parse(&b)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Header) != 1 || got.Header[0] != "commit abc" {
		t.Fatalf("parsed header %q", got.Header)
	}
	if got := got.Metrics; len(got) != 3 || got[2].Key != "units JSON engine-closure" || got[2].Value != 7 {
		t.Fatalf("parsed %v", got)
	}
	if err := Write(&b, nil, append(ms, ms[0])); err == nil {
		t.Error("duplicate key written")
	}
	if _, err := Parse(strings.NewReader("a 1 lower\na 2 lower\n")); err == nil {
		t.Error("duplicate key parsed")
	}
	if _, err := Parse(strings.NewReader("a lower\n")); err == nil {
		t.Error("line without a value parsed")
	}
}

func TestComparable(t *testing.T) {
	linux := Report{Header: []string{"commit a clean", "go go1.27.1 linux/amd64"}}
	if err := Comparable(linux, linux); err != nil {
		t.Errorf("same toolchain: %v", err)
	}
	for _, other := range []Report{
		{Header: []string{"go go1.27.1 darwin/arm64"}},
		{Header: []string{"go go1.27.2 linux/amd64"}},
		{Header: []string{"commit a clean"}},
		{Header: []string{"go go1.27.1 linux/amd64 GOAMD64=v3 CGO_ENABLED=1"}},
	} {
		if err := Comparable(linux, other); err == nil {
			t.Errorf("%q: comparable", other.Header)
		}
	}
	tc := Toolchain()
	if !strings.HasPrefix(tc, "go go") || !strings.Contains(tc, " CGO_ENABLED=") || strings.Contains(tc, "unknown") {
		t.Errorf("Toolchain() = %q, want the release, the target and its build settings", tc)
	}
	for _, s := range toolchainSettings() {
		if !strings.Contains(tc, " "+s+"=") {
			t.Errorf("Toolchain() = %q lacks %s", tc, s)
		}
	}
}

func TestCompare(t *testing.T) {
	lower := Policy{Kind: Lower}
	tol := Policy{Kind: Lower, Tolerance: 1}
	capped := Policy{Kind: Lower, HasMax: true, Max: 2219}
	slack := Policy{Kind: Lower, Slack: 2}
	base := []Metric{
		{"a same", 10, lower}, {"b better", 10, lower}, {"c worse", 10, lower},
		{"d tolerated", 1000, tol}, {"e beyond", 1000, tol}, {"f equal", 1, Policy{Kind: Equal}},
		{"g info", 5, Policy{Kind: Info}}, {"h gone", 3, lower}, {"k slack", 100, slack}, {"l beyond slack", 100, slack}, {"m slack down", 100, slack}, {"n beyond slack down", 100, slack}, {"i max", 2000, Policy{Kind: Lower, HasMax: true, Max: 2100}},
		{"o loosened", 100, lower}, {"p tightened", 100, tol}, {"q loosened and worse", 100, lower}, {"r base max", 2000, capped},
		{"s head max", 0, lower},
	}
	head := []Metric{
		{"a same", 10, lower}, {"b better", 9, lower}, {"c worse", 11, lower},
		{"d tolerated", 1010, tol}, {"e beyond", 1011, tol}, {"f equal", 0, Policy{Kind: Equal}},
		{"g info", 50, Policy{Kind: Info}}, {"i max", 1900, Policy{Kind: Lower, HasMax: true, Max: 1800}}, {"j new", 1, lower},
		{"k slack", 102, slack}, {"l beyond slack", 103, slack}, {"m slack down", 98, slack}, {"n beyond slack down", 97, slack},
		// A head that loosens an exact metric to hide its own increase is judged by the base's policy.
		{"o loosened", 100, Policy{Kind: Lower, Tolerance: 5}}, {"p tightened", 100, lower}, {"q loosened and worse", 104, Policy{Kind: Lower, Tolerance: 5}},
		// A ceiling holds whatever the base's value: the base's, a head that drops it, or a new metric's.
		{"r base max", 2300, lower}, {"s head max", 1, Policy{Kind: Lower, HasMax: true}}, {"t new max", 1, Policy{Kind: Lower, HasMax: true}},
	}
	want := map[string]Verdict{
		"a same": Same, "b better": Better, "c worse": Worse, "d tolerated": Tolerated, "e beyond": Worse,
		"f equal": Worse, "g info": Changed, "h gone": Missing, "i max": OverMax, "j new": Added, "k slack": Tolerated, "l beyond slack": Worse, "m slack down": Tolerated, "n beyond slack down": Better,
		"o loosened": PolicyChanged, "p tightened": PolicyChanged, "q loosened and worse": Worse, "r base max": OverMax,
		"s head max": OverMax, "t new max": OverMax,
	}
	rows := Compare(base, head)
	if len(rows) != len(want) {
		t.Fatalf("%d rows, want %d", len(rows), len(want))
	}
	for _, r := range rows {
		if r.Verdict != want[r.Key] {
			t.Errorf("%s: %s, want %s", r.Key, r.Verdict, want[r.Key])
		}
	}
	var b bytes.Buffer
	fails, err := WriteRows(&b, rows, false)
	if err != nil {
		t.Fatal(err)
	}
	if fails != 12 {
		t.Errorf("%d regressions, want 12\n%s", fails, b.String())
	}
	if strings.Contains(b.String(), "a same") {
		t.Error("unchanged metric listed without all")
	}
	if !strings.Contains(b.String(), "c worse [lower]") || !strings.Contains(b.String(), "+10.00") {
		t.Errorf("delta not printed:\n%s", b.String())
	}
	if !strings.Contains(b.String(), "o loosened [lower -> lower~5%]") {
		t.Errorf("policy change not printed:\n%s", b.String())
	}
}
