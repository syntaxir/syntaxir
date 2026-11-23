package driver

import (
	"slices"
	"strings"
	"testing"

	gopass "syntaxir.dev/sxir/internal/backend/codegen/golang/pass"
	tspass "syntaxir.dev/sxir/internal/backend/codegen/typescript/pass"
	"syntaxir.dev/sxir/internal/middleend/codepass"
	"syntaxir.dev/sxir/internal/middleend/corepass"
	"syntaxir.dev/sxir/internal/middleend/flowpass"
	"syntaxir.dev/sxir/internal/pass"
)

// The flags of the passes that the tests of the driver add to the pipelines. The registry of this
// test binary holds them and the flags of the parts the driver links (the Core, Flow and Exec passes and
// the targets' Exec passes), and not those of the printers.
func init() {
	pass.Register(
		pass.Flag{Name: testFlowPass, Kind: pass.KindFlowPass, Default: 1, Doc: "adds a comment"},
		pass.Flag{Name: testExecPass, Kind: pass.KindExecPass, Default: 1, Doc: "documents"},
		pass.Flag{Name: testExecBreak, Kind: pass.KindExecPass, Doc: "breaks"},
	)
}

// Every pass of the Core pipeline is an optimization that can be turned off, under its own name.
func TestEveryCorePassIsAFlag(t *testing.T) {
	for _, name := range corepass.Names() {
		if f, ok := pass.Lookup(name); !ok || f.Kind != pass.KindCorePass {
			t.Errorf("pass %s is not a registered optimization of kind pass", name)
		}
	}
}

// Every pass of the Flow pipeline is an optimization that can be turned off, under its own name, and
// no Flow pass has the name of a Core pass. A pass of Code IR that runs at both stages is of kind code
// pass and is also an Exec pass.
func TestEveryFlowPassIsAFlag(t *testing.T) {
	for _, name := range flowpass.Names() {
		f, ok := pass.Lookup(name)
		if !ok || f.Kind != pass.KindFlowPass && (f.Kind != pass.KindCodePass || !slices.Contains(codepass.Names(), name)) {
			t.Errorf("pass %s is not a registered optimization of kind flow pass, or of kind code pass at both stages", name)
		}
		if slices.Contains(corepass.Names(), name) {
			t.Errorf("pass %s is both a Core and a Flow pass", name)
		}
	}
	// The manager accepts the pipeline (names, After, features) and every pass states its obligation.
	if _, err := runFlowTable(t, testRun{}, "rules", planned(framed()), flowpass.Target{}); err != nil {
		t.Error(err)
	}
	for _, p := range flowpass.Passes {
		if p.Decl().Obligation == "" {
			t.Errorf("pass %s states no obligation", p.Name())
		}
	}
}

// Every pass of the Exec pipeline is an optimization that can be turned off, under its own name, and
// no Exec pass has the name of a Core or Flow pass, except a pass of Code IR of kind code pass, which
// runs at both stages.
func TestEveryExecPassIsAFlag(t *testing.T) {
	for _, name := range codepass.Names() {
		f, ok := pass.Lookup(name)
		if !ok || f.Kind != pass.KindExecPass && f.Kind != pass.KindCodePass {
			t.Errorf("pass %s is not a registered optimization of kind exec pass or code pass", name)
		}
		if slices.Contains(corepass.Names(), name) || f.Kind != pass.KindCodePass && slices.Contains(flowpass.Names(), name) {
			t.Errorf("pass %s is also a Core or Flow pass", name)
		}
	}
	if _, err := runExecTable(t, testRun{}, "rules", planned(framed()), noTarget); err != nil {
		t.Error(err)
	}
	for _, p := range codepass.Passes {
		if p.Decl().Obligation == "" {
			t.Errorf("pass %s states no obligation", p.Name())
		}
	}
}

func TestCheckStepNames(t *testing.T) {
	if err := checkStepNames(pass.Flags()); err != nil {
		t.Error(err)
	}
	for _, name := range append(requiredSteps(), PrintAll) {
		if err := checkStepNames([]pass.Flag{{Name: name, Kind: pass.KindFlowPass, Doc: "x"}}); err == nil || !strings.Contains(err.Error(), "name of a step") {
			t.Errorf("a flag named %s: %v", name, err)
		}
	}
}

// A required step cannot be turned on or off: CheckOptimizable names it, and passes the
// optimizations and the unknown names, which the settings report themselves.
func TestCheckOptimizable(t *testing.T) {
	for _, name := range requiredSteps() {
		if err := CheckOptimizable("guards", " "+name+" "); err == nil || err.Error() != name+" is a required step and always runs" {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := CheckOptimizable("guards", "nosuch", "all"); err != nil {
		t.Error(err)
	}
}

// The speed profile is the defaults of the registered flags. A profile that sets a flag that no
// linked part registered is an error that names the flag: the memory and size profiles set flags of
// the generators, which this test binary does not link, and they must not quietly become speed.
func TestProfile(t *testing.T) {
	for _, name := range []string{"", "speed"} {
		speed, err := Profile(name)
		if err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		for _, f := range pass.Flags() {
			if speed.Value(f.Name) != f.Default {
				t.Errorf("%s is %d in speed, want %d", f.Name, speed.Value(f.Name), f.Default)
			}
		}
	}
	if _, err := Profile("nosuch"); err == nil || !strings.Contains(err.Error(), "valid: speed, memory, size") {
		t.Errorf("unknown profile: %v", err)
	}
	// The driver links the Go generator, which registers the flags that the memory profile sets.
	mem, err := Profile("memory")
	if err != nil || mem.Value("typed-list-slabs") != 0 || mem.Value("typed-object-slabs") != 0 {
		t.Errorf("memory: %v, %v", mem, err)
	}
	if err := CheckRegistry(); err != nil {
		t.Errorf("CheckRegistry: %v", err)
	}
	// A profile that sets a flag that no linked part registers is an error that says so.
	var without []pass.Flag
	for _, f := range pass.Flags() {
		if f.Name != "typed-list-slabs" {
			without = append(without, f)
		}
	}
	err = checkProfiles(profileDefs, without)
	for _, want := range []string{"profile memory", `"typed-list-slabs"`, "not a registered flag", "not linked"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("a profile without its flag: %v, want an error with %q", err, want)
		}
	}
}

func TestParsePrintAfter(t *testing.T) {
	if got, err := ParsePrintAfter(""); err != nil || got != nil {
		t.Errorf("empty: %v, %v", got, err)
	}
	if got, err := ParsePrintAfter("elaborate, guards,all"); err != nil || !slices.Equal(got, []string{"elaborate", "guards", "all"}) {
		t.Errorf("list: %v, %v", got, err)
	}
	if _, err := ParsePrintAfter("guards,nonsense"); err == nil || !strings.Contains(err.Error(), "unknown pass") {
		t.Errorf("unknown: %v", err)
	}
	// The names follow the order the steps run in: the elaboration and the Core passes, the Action
	// lowering, passes and finish, dispatch, outline and the plan of the program, then the Flow lowering, passes and finish, then the Exec lowering and passes and the Exec
	// passes of the targets.
	want := append([]string{"elaborate"}, corepass.Names()...)
	want = append(want, "actionlower", "text-dispatch", "field-compare", "go-struct-call", "go-in-place", "action-finish")
	want = append(append(want, "dispatch", "outline", "plan", "flowlower"), flowpass.Names()...)
	want = append(append(want, "flow-finish", "execlower"), codepass.Names()...)
	for _, p := range append(slices.Clone(gopass.Passes), tspass.Passes...) {
		want = append(want, p.Name())
	}
	// A pass at both stages is named once, at its first place.
	var once []string
	for _, n := range want {
		if !slices.Contains(once, n) {
			once = append(once, n)
		}
	}
	want = append(once, "all")
	if got := PrintNames(); !slices.Equal(got, want) {
		t.Errorf("names %v, want %v", got, want)
	}
	if got, err := ParsePrintAfter("flowlower"); err != nil || !slices.Equal(got, []string{"flowlower"}) {
		t.Errorf("flowlower: %v, %v", got, err)
	}
	// The stages are no names of steps; the error names the step that makes the stage's IR.
	for stage, step := range map[string]string{"core": "elaborate", "flow": "flowlower", "exec": "execlower"} {
		if _, err := ParsePrintAfter(stage); err == nil || !strings.Contains(err.Error(), "the step that makes its IR is "+step) {
			t.Errorf("the stage %s: %v", stage, err)
		}
	}
	if _, err := ParsePrintAfter("nope"); err == nil || !strings.HasPrefix(err.Error(), `unknown pass or step "nope"`) {
		t.Errorf("an unknown name: %v", err)
	}
}

func TestParseValues(t *testing.T) {
	got, err := ParseValues(" ts-literal-sharing , class-tables=3,inline-literals=0 ")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"ts-literal-sharing": 1, "class-tables": 3, "inline-literals": 0}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %d, want %d", k, got[k], v)
		}
	}
	if got, err := ParseValues("  "); got != nil || err != nil {
		t.Errorf("an empty list is %v, %v", got, err)
	}
	for list, want := range map[string]string{
		"a,,b":                        "empty optimization name",
		"=3":                          "empty optimization name",
		"class-tables=x":              `"x" is not a count`,
		"class-tables=,scans":         `"" is not a count`,
		"class-tables=3,class-tables": "twice",
	} {
		if _, err := ParseValues(list); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseValues(%q) = %v, want an error with %q", list, err, want)
		}
	}
}

// testFlags is the registry, which has the flags that the profiles set: the driver links every part
// that registers them.
func testFlags() []pass.Flag { return pass.Flags() }

func TestProfileDefinitionsAreValid(t *testing.T) {
	flags := testFlags()
	if err := checkProfiles(profileDefs, flags); err != nil {
		t.Error(err)
	}
	for _, c := range []struct {
		name string
		defs []profileDef
		want string
	}{
		{"undocumented", []profileDef{{Name: "a"}}, "no description"},
		{"repeated", []profileDef{{Name: "a", Doc: "x"}, {Name: "a", Doc: "y"}}, "twice"},
		{"unknown flag", []profileDef{{Name: "a", Doc: "x", Values: map[string]int{"nosuch": 1}}}, "not a registered flag"},
		{"bad value", []profileDef{{Name: "a", Doc: "x", Values: map[string]int{"guards": 2}}}, "value 2"},
	} {
		if err := checkProfiles(c.defs, flags); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want an error with %q", c.name, err, c.want)
		}
	}
	if err := checkProfiles([]profileDef{{Name: "a", Doc: "x", Values: map[string]int{"o": 1}}}, []pass.Flag{{Name: "o", Doc: "x", ChangesOutput: true}}); err == nil || !strings.Contains(err.Error(), "changes the output") {
		t.Errorf("a profile that sets an output flag: %v", err)
	}
	if got := Profiles(); !slices.Equal(got, []string{"speed", "memory", "size"}) {
		t.Fatalf("profiles %v", got)
	}
	for _, name := range Profiles() {
		if ProfileDoc(name) == "" {
			t.Errorf("profile %s has no description", name)
		}
	}
	if ProfileDoc("nosuch") != "" || ProfileValues("nosuch") != nil {
		t.Errorf("an unknown profile has a description or values")
	}
	if v := ProfileValues("size"); v["class-tables"] != 5 || len(v) != 2 {
		t.Errorf("size values %v", v)
	}
	// The flags of size are the Flow passes', which the driver links, so it resolves here.
	size, err := Profile("size")
	if err != nil {
		t.Fatalf("size: %v", err)
	}
	if size.Value("class-tables") != 5 || size.Value("inline-literals") != 0 {
		t.Errorf("size settings %v", size)
	}
}

// A registry with an optimization that changes the output, which no flag of the real one does yet,
// shows the checks on their positive path: it is valid as an opt-in, and neither a nonzero default nor
// a profile that sets it is.
func TestOutputChangingFlagRegistry(t *testing.T) {
	fake := pass.Flag{Name: "fake-output", Kind: pass.KindPrinter, Doc: "changes the generated API", ChangesOutput: true}
	flags := append(testFlags(), fake)
	if err := pass.CheckFlags(flags); err != nil {
		t.Errorf("an opt-in that changes the output: %v", err)
	}
	on := fake
	on.Default = 1
	if err := pass.CheckFlags(append(testFlags(), on)); err == nil || !strings.Contains(err.Error(), "must be an opt-in") {
		t.Errorf("a nonzero default for a flag that changes the output: %v, want the opt-in error", err)
	}
	for _, v := range []int{0, 1} {
		defs := []profileDef{{Name: "p", Doc: "x", Values: map[string]int{"fake-output": v}}}
		if err := checkProfiles(defs, flags); err == nil || !strings.Contains(err.Error(), "changes the output") {
			t.Errorf("a profile that gives the flag %d: %v, want an error", v, err)
		}
	}
	if err := checkProfiles([]profileDef{{Name: "p", Doc: "x", Values: map[string]int{"guards": 0}}}, flags); err != nil {
		t.Errorf("a profile of flags that keep behavior, in a registry that also has the flag: %v", err)
	}
}
