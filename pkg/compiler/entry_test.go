package compiler

import (
	"reflect"
	"strings"
	"testing"

	"syntaxir.dev/sxir/internal/backend/codegen/common"
	"syntaxir.dev/sxir/internal/driver/generate"
)

// The options of the compile entry carry the optimization setting as the settings that a generation
// of the same setting runs with, beside the options of the check, and an invalid setting is an
// error of the conversion.
func TestEntryCarriesTheOptimization(t *testing.T) {
	o := newOptions([]Option{WithSourceInfo(), WithOptimize(Optimize{Profile: "size", Off: []string{"guards"}})})
	e, err := o.entry()
	if err != nil {
		t.Fatal(err)
	}
	want, err := generate.Settings(common.GenOptions{Optimize: "size", NoOpt: []string{"guards"}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(e.Settings, want) || !e.Compile.SourceInfo || e.Compile.DefaultValue {
		t.Errorf("entry options %+v, want the settings %+v with source information", e, want)
	}
	speed, err := generate.Settings(common.GenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if none, err := newOptions(nil).entry(); err != nil || !reflect.DeepEqual(none.Settings, speed) {
		t.Errorf("no setting: %+v, %v, want the settings of the speed profile %+v", none, err, speed)
	}
	if _, err := newOptions([]Option{WithOptimize(Optimize{Off: []string{"nonsense"}})}).entry(); err == nil || !strings.Contains(err.Error(), "nonsense") {
		t.Errorf("an unknown optimization: %v", err)
	}
}
