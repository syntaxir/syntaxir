package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/backend/codegen/golang/libsrc"
	tslib "syntaxir.dev/sxir/internal/backend/codegen/typescript/lib"
	"syntaxir.dev/sxir/internal/bench/metrics"
	"syntaxir.dev/sxir/internal/bench/probe"
	"syntaxir.dev/sxir/internal/cancel"
	"syntaxir.dev/sxir/internal/driver"
	gensrc "syntaxir.dev/sxir/internal/driver/generate"
	"syntaxir.dev/sxir/internal/driver/result"
	"syntaxir.dev/sxir/internal/pass"
	"syntaxir.dev/sxir/pkg/grammar"
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// The settings part measures the effect of each registered optimization flag in each profile
// (design record 080, "Re-Measurement"): it generates the grammars of probe.Grammars with the flag set
// against the profile, and records what that changes against the profile's own parsers. Every value
// is the variant's minus the profile's, with policy info, so that compare lists a landing that changes
// an optimization's effect without judging it:
//
//	setting-identical-grammars <profile> <flag>=<value> go|ts   grammars whose parser the flag leaves byte-identical
//	setting <profile> <flag>=<value> compile-steps <grammar>     steps of the generation of both parsers
//	setting <profile> <flag>=<value> go-source <grammar>         bytes of the generated Go files
//	setting <profile> <flag>=<value> go-binary <grammar>         bytes of a stripped program that measures the parser
//	setting <profile> <flag>=<value> allocs|alloc-bytes <grammar> <api>  objects and bytes of a call, summed over the inputs
//	setting <profile> <flag>=<value> retained-bytes|retained-objects <grammar>  live heap of the values of ParseValue
//	setting <profile> <flag>=<value> ts-source <grammar>         bytes of the generated TypeScript files
//
// The Go metrics are recorded for a flag that acts on a generated Go backend, ts-source for one that
// acts on TypeScript; compile-steps and the identical counts for every flag. The value of the flag is 0
// if the profile turns it on, and otherwise the value that turns it on: 1, or for a flag with a count
// the largest count that the speed values or a profile give it. The generation options are those of
// testdata/grammars/generate.txt, with the Go library embedded (-lib embed) so that the flags that
// narrow the embedded library show their effect; the TypeScript options are those of
// probe.GenTSArgs. The calls are measured on the inputs of the language-grammar matrix
// (probe.MatrixInputs) by the program internal/bench/settingsprobe, built for each parser; the
// generated parsers' units are not recorded, since their equality under every setting is a parity
// test. A flag that changes the output (pass.Flag.ChangesOutput) changes the API that the program
// calls and gets the source, steps and identity metrics only.

// settingsModule is the import path of the generated package that internal/bench/settingsprobe
// imports; the build supplies it by an overlay.
const settingsModule = "syntaxir.dev/sxir/internal/bench/settingsprobe/gen"

// A settingsSubject is a grammar of probe.Grammars with its generation options and inputs.
type settingsSubject struct {
	name         string
	grammar      *ast.Grammar
	goOpts       common.GenOptions
	tsOpts       common.GenOptions
	apis         []string // the APIs of the Go parser: parse, parse-value, recognize
	inputs       []string // the paths of the inputs
	generations  map[string]*settingsGeneration
	measurements map[string]*settingsMeasurement
}

// A settingsGeneration is the output of one generation of both parsers of a subject.
type settingsGeneration struct {
	goFiles, tsFiles []result.File
	steps            uint64
}

// A settingsMeasurement is what the program measures for one Go parser.
type settingsMeasurement struct {
	binary          int64
	apis            map[string]probeAPI
	retainedBytes   int64
	retainedObjects int64
}

type probeAPI struct {
	Objects uint64 `json:"objects"`
	Bytes   uint64 `json:"bytes"`
	Errors  int    `json:"errors"`
}

// genFlags maps the options of testdata/grammars/generate.txt to the generation options.
func genFlags(args []string) (common.GenOptions, error) {
	o := common.GenOptions{Module: settingsModule, Lib: common.LibEmbed}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-types":
			o.Types = true
		case "-typed-only":
			o.TypedOnly = true
		case "-no-limits":
			o.NoLimitControls = true
		case "-nodoc":
			o.OmitPackageDoc = true
		case "-recognize":
			o.Recognize = true
		case "-expectations":
			o.Expectations = true
		case "-lib":
			// The library is embedded whatever the line says (see the part's comment).
			i++
		default:
			return o, fmt.Errorf("the settings part does not know the generation option %s", args[i])
		}
	}
	return o, nil
}

// tsFlags maps the TypeScript options of probe.GenTSArgs, which start with -g and its grammar.
func tsFlags(args []string) (common.GenOptions, error) {
	o := common.GenOptions{Lib: common.LibEmbed}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-g":
			i++
		case "-types":
			o.Types = true
		case "-no-limits":
			o.NoLimitControls = true
		case "-recognize":
			o.Recognize = true
		case "-expectations":
			o.Expectations = true
		default:
			return o, fmt.Errorf("the settings part does not know the TypeScript option %s", args[i])
		}
	}
	return o, nil
}

func loadSettingsSubject(root, work, name string) (*settingsSubject, error) {
	path := filepath.Join(root, "testdata", "grammars", name, name+".sxir")
	g, err := grammar.Load(grammar.FromFS(os.DirFS(filepath.Dir(path)), filepath.Base(path)))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	args, err := probe.GenOptions(root, name)
	if err != nil {
		return nil, err
	}
	goOpts, err := genFlags(args)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	targs, err := probe.GenTSArgs(root, name)
	if err != nil {
		return nil, err
	}
	tsOpts, err := tsFlags(targs)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if !goOpts.Types {
		return nil, fmt.Errorf("%s: the settings part measures ParseValue, which -types generates", name)
	}
	apis := []string{"parse-value"}
	if !goOpts.TypedOnly {
		apis = append(apis, "parse")
	}
	if goOpts.Recognize {
		apis = append(apis, "recognize")
	}
	in, err := probe.MatrixInputs(root, name)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(work, "settings-inputs", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var inputs []string
	for _, f := range in {
		p := filepath.Join(dir, f.Name)
		if err := os.WriteFile(p, []byte(f.Text), 0o644); err != nil {
			return nil, err
		}
		inputs = append(inputs, p)
	}
	return &settingsSubject{name: name, grammar: g, goOpts: goOpts, tsOpts: tsOpts, apis: apis, inputs: inputs,
		generations: map[string]*settingsGeneration{}, measurements: map[string]*settingsMeasurement{}}, nil
}

// generate generates both parsers of s in profile with the flags opt and noOpt set, and counts the
// steps of the generation.
func (s *settingsSubject) generate(profile string, opt map[string]int, noOpt []string) (*settingsGeneration, error) {
	targets := []driver.Target{{Backend: result.Go, Gen: s.goOpts}, {Backend: result.TypeScript, Gen: s.tsOpts}}
	for i := range targets {
		t := &targets[i].Gen
		t.LibGo, t.TSLib = libsrc.FS, tslib.FS
		t.Optimize, t.Opt, t.NoOpt = profile, opt, noOpt
	}
	meter := new(cancel.Meter)
	files, err := gensrc.TargetsContext(cancel.WithMeter(context.Background(), meter), driver.CheckForGenerator, s.grammar, driver.Options{}, targets...)
	if err != nil {
		return nil, fmt.Errorf("%s, profile %s: %w", s.name, profile, err)
	}
	return &settingsGeneration{goFiles: files[0], tsFiles: files[1], steps: meter.Steps()}, nil
}

// apisSource is the file apis.go of internal/bench/settingsprobe for a parser with the APIs apis.
func apisSource(apis []string) string {
	var b strings.Builder
	b.WriteString("//go:build sxir_settingsprobe\n\npackage main\n\nimport gen \"" + settingsModule + "\"\n\n")
	b.WriteString("func parseValue(in string) (any, error) { return gen.ParseValue(in) }\n\n")
	b.WriteString("var apis = map[string]func(string) error{\n")
	for _, a := range apis {
		switch a {
		case "parse-value":
			b.WriteString("\t\"parse-value\": func(in string) error { _, err := gen.ParseValue(in); return err },\n")
		case "parse":
			b.WriteString("\t\"parse\": func(in string) error { _, err := gen.Parse(in); return err },\n")
		case "recognize":
			b.WriteString("\t\"recognize\": func(in string) error { return gen.Recognize(in) },\n")
		}
	}
	b.WriteString("}\n")
	return b.String()
}

// measure builds the program of internal/bench/settingsprobe for the Go files of a generation of s in
// dir and runs it on the inputs of s, each measurement in a process of its own.
func (s *settingsSubject) measure(root, dir string, files []result.File) (*settingsMeasurement, error) {
	pkg := filepath.Join(root, "internal", "bench", "settingsprobe")
	replace := map[string]string{}
	for _, f := range files {
		p := filepath.Join(dir, "gen", filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, f.Content, 0o644); err != nil {
			return nil, err
		}
		replace[filepath.Join(pkg, "gen", filepath.FromSlash(f.Path))] = p
	}
	apis := filepath.Join(dir, "apis.go")
	if err := os.WriteFile(apis, []byte(apisSource(s.apis)), 0o644); err != nil {
		return nil, err
	}
	replace[filepath.Join(pkg, "apis.go")] = apis
	overlay, err := json.Marshal(map[string]any{"Replace": replace})
	if err != nil {
		return nil, err
	}
	ovPath := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(ovPath, overlay, 0o644); err != nil {
		return nil, err
	}
	bin := filepath.Join(dir, "settingsprobe")
	if _, err := probe.ExecOut(root, nil, "go", "build", "-tags", "sxir_settingsprobe", "-trimpath", "-ldflags=-s -w",
		"-overlay", ovPath, "-o", bin, "./internal/bench/settingsprobe"); err != nil {
		return nil, err
	}
	st, err := os.Stat(bin)
	if err != nil {
		return nil, err
	}
	m := &settingsMeasurement{binary: st.Size()}
	var out struct {
		APIs            map[string]probeAPI `json:"apis"`
		RetainedBytes   int64               `json:"retained_bytes"`
		RetainedObjects int64               `json:"retained_objects"`
	}
	// One P, as TestMetrics measures: the per-P arrays of a sync.Pool are as long as GOMAXPROCS.
	text, err := probe.ExecOut(root, []string{"GOMAXPROCS=1"}, bin, append([]string{"-what", "allocs"}, s.inputs...)...)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return nil, fmt.Errorf("settingsprobe of %s: %v", s.name, err)
	}
	m.apis = out.APIs
	text, err = probe.ExecOut(root, []string{"GOGC=off", "GOMAXPROCS=1"}, bin, append([]string{"-what", "retained"}, s.inputs...)...)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return nil, fmt.Errorf("settingsprobe of %s: %v", s.name, err)
	}
	m.retainedBytes, m.retainedObjects = out.RetainedBytes, out.RetainedObjects
	return m, nil
}

func filesSize(files []result.File) int64 {
	var n int64
	for _, f := range files {
		n += int64(len(f.Content))
	}
	return n
}

func sameFiles(a, b []result.File) bool {
	return slices.EqualFunc(a, b, func(x, y result.File) bool { return x.Path == y.Path && bytes.Equal(x.Content, y.Content) })
}

// parallel runs work(i) for i in [0, n) on at most workers goroutines and returns the first error.
func parallel(n, workers int, work func(i int) error) error {
	var wg sync.WaitGroup
	errs := make([]error, n)
	sem := make(chan struct{}, workers)
	for i := range n {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			errs[i] = work(i)
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

// collectSettings is the settings part.
func collectSettings(root, work string) ([]metrics.Metric, error) {
	variants, err := probe.SettingVariants()
	if err != nil {
		return nil, err
	}
	subjects := make([]*settingsSubject, len(probe.Grammars))
	for i, name := range probe.Grammars {
		if subjects[i], err = loadSettingsSubject(root, work, name); err != nil {
			return nil, err
		}
	}
	workers := max(runtime.NumCPU()/2, 1)
	info := metrics.Policy{Kind: metrics.Info}
	var ms []metrics.Metric
	for len(variants) > 0 {
		profile := variants[0].Profile
		n := 1
		for n < len(variants) && variants[n].Profile == profile {
			n++
		}
		group := variants[:n]
		variants = variants[n:]
		// The profile's own parsers, and their measurements once a flag changes the Go parser.
		if err := parallel(len(subjects), workers, func(i int) error {
			g, err := subjects[i].generate(profile, nil, nil)
			subjects[i].generations[profile] = g
			return err
		}); err != nil {
			return nil, err
		}
		once := make([]sync.Once, len(subjects))
		baseErr := make([]error, len(subjects))
		baseMeasure := func(i int) (*settingsMeasurement, error) {
			once[i].Do(func() {
				s := subjects[i]
				s.measurements[profile], baseErr[i] = s.measure(root, filepath.Join(work, "settings", profile, "base", s.name), s.generations[profile].goFiles)
			})
			return subjects[i].measurements[profile], baseErr[i]
		}
		for _, v := range group {
			f := v.Flag
			goReach := f.Backends&pass.GeneratedGo != 0
			tsReach := f.Backends.Has(pass.BackendTypeScript)
			rows := make([][]metrics.Metric, len(subjects))
			identical := make([][2]bool, len(subjects))
			if err := parallel(len(subjects), workers, func(i int) error {
				s := subjects[i]
				b := s.generations[profile]
				g, err := s.generate(profile, v.Opt(), v.NoOpt())
				if err != nil {
					return err
				}
				identical[i] = [2]bool{sameFiles(g.goFiles, b.goFiles), sameFiles(g.tsFiles, b.tsFiles)}
				row := func(value int64, fields ...string) {
					rows[i] = append(rows[i], metrics.New(value, info, append([]string{"setting", profile, v.Name()}, fields...)...))
				}
				row(int64(g.steps)-int64(b.steps), "compile-steps", s.name)
				if tsReach {
					row(filesSize(g.tsFiles)-filesSize(b.tsFiles), "ts-source", s.name)
				}
				if !goReach {
					return nil
				}
				row(filesSize(g.goFiles)-filesSize(b.goFiles), "go-source", s.name)
				if f.ChangesOutput {
					return nil
				}
				var bm, vm settingsMeasurement
				if !identical[i][0] {
					p, err := baseMeasure(i)
					if err != nil {
						return err
					}
					m, err := s.measure(root, filepath.Join(work, "settings", profile, v.Name(), s.name), g.goFiles)
					if err != nil {
						return err
					}
					for _, api := range s.apis {
						if m.apis[api].Errors != p.apis[api].Errors {
							return fmt.Errorf("%s, profile %s, %s: %s fails on %d inputs, but on %d in the profile's parser", s.name, profile, v.Name(), api, m.apis[api].Errors, p.apis[api].Errors)
						}
					}
					bm, vm = *p, *m
				}
				row(vm.binary-bm.binary, "go-binary", s.name)
				for _, api := range s.apis {
					row(int64(vm.apis[api].Objects)-int64(bm.apis[api].Objects), "allocs", s.name, api)
					row(int64(vm.apis[api].Bytes)-int64(bm.apis[api].Bytes), "alloc-bytes", s.name, api)
				}
				row(vm.retainedBytes-bm.retainedBytes, "retained-bytes", s.name)
				row(vm.retainedObjects-bm.retainedObjects, "retained-objects", s.name)
				return nil
			}); err != nil {
				return nil, err
			}
			var sameGo, sameTS int64
			for i := range subjects {
				ms = append(ms, rows[i]...)
				if identical[i][0] {
					sameGo++
				}
				if identical[i][1] {
					sameTS++
				}
			}
			ms = append(ms,
				metrics.New(sameGo, info, "setting-identical-grammars", profile, v.Name(), "go"),
				metrics.New(sameTS, info, "setting-identical-grammars", profile, v.Name(), "ts"))
		}
	}
	return ms, nil
}
