package print_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/testing/codegentest"
)

// Export isolated typed tracing controls, keeping generation, compilation and
// raw measurements outside the timed parse loop and outside the repository.
func TestExportGeneratedTypedTraceBenchmarks(t *testing.T) {
	t.Parallel()
	dir := os.Getenv("SXIR_TYPED_TRACE_BENCH_DIR")
	if dir == "" {
		t.Skip("set SXIR_TYPED_TRACE_BENCH_DIR to export benchmark controls")
	}
	write := func(name string, data []byte) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", []byte(codegentest.GoMod("typedtracebench")))
	manifest := map[string]struct {
		Bytes       int
		DirectStart bool
	}{}
	for _, fixture := range []struct{ name, path, input string }{
		{"json", "testdata/grammars/json/json.sxir", "[" + strings.TrimSuffix(strings.Repeat(`{"name":"éclair","n":123,"ok":true},`, 64), ",") + "]"},
		{"xml", "testdata/grammars/xml/xml.sxir", "<root>" + strings.Repeat(`<item name="éclair">123</item>`, 64) + "</root>"},
		{"calc", "examples/calculator/calc.sxir", strings.TrimSuffix(strings.Repeat("1+2*3+", 32), "+")},
	} {
		data, err := os.ReadFile(filepath.Join("../../../../..", fixture.path))
		if err != nil {
			t.Fatal(err)
		}
		g, err := parseTest(string(data))
		if err != nil {
			t.Fatal(err)
		}
		for _, convert := range []bool{false, true} {
			route := "auto"
			if convert {
				route = "converted"
			}
			for _, enabled := range []bool{false, true} {
				pkg := "ordinary"
				if enabled {
					pkg = "observed"
				}
				code, err := codegentest.GenerateCode(t, g, common.GenOptions{Package: pkg, Start: "main", Types: true, Recognize: true, Observability: enabled, ConvertTypes: convert})
				if err != nil {
					t.Fatal(err)
				}
				path := fixture.name + "/" + route + "/" + pkg
				manifest[path] = struct {
					Bytes       int
					DirectStart bool
				}{len(code), strings.Contains(string(code), "return sxirrt.TParse(trules[")}
				write(path+"/parser.go", code)
				driver := strings.NewReplacer("PACKAGE", pkg, "INPUT", fmt.Sprintf("%q", fixture.input)).Replace(generatedTypedTraceBenchmark)
				if enabled {
					driver += generatedTypedTraceEnabledBenchmark
				}
				write(path+"/bench_test.go", []byte(driver))
			}
		}
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	write("manifest.json", data)
}

const generatedTypedTraceBenchmark = `package PACKAGE
import("testing")
var typedSink any
var errorSink error
const input=INPUT
func BenchmarkValue(b *testing.B){for _,named:=range []bool{false,true}{route:="ParseValue";if named{route="ParseRuleValue"};b.Run(route,func(b *testing.B){for _,unit:=range []PositionUnit{CodePoints,Bytes}{name:="CodePoints";if unit==Bytes{name="Bytes"};b.Run(name,func(b *testing.B){opts:=[]ParseOption{WithUnit(unit)};if _,err:=ParseValue(input,opts...);err!=nil{b.Fatal(err)};b.ReportAllocs();b.SetBytes(int64(len(input)));for b.Loop(){if named{typedSink,errorSink=ParseRuleValue("main",input,opts...)}else{typedSink,errorSink=ParseValue(input,opts...)};if errorSink!=nil{b.Fatal(errorSink)}}})}})}}
`
const generatedTypedTraceEnabledBenchmark = `
var eventSink TraceEvent
func BenchmarkValueTrace(b *testing.B){for _,mode:=range []string{"full","root"}{b.Run(mode,func(b *testing.B){for _,unit:=range []PositionUnit{CodePoints,Bytes}{name:="CodePoints";if unit==Bytes{name="Bytes"};b.Run(name,func(b *testing.B){opts:=[]ParseOption{WithUnit(unit),WithTrace(func(e TraceEvent){eventSink=e})};if mode=="root"{opts=append(opts,WithTraceMaxDepth(1))};if _,err:=ParseValue(input,opts...);err!=nil{b.Fatal(err)};b.ReportAllocs();b.SetBytes(int64(len(input)));for b.Loop(){typedSink,errorSink=ParseValue(input,opts...);if errorSink!=nil{b.Fatal(errorSink)}}})}})}}
`
