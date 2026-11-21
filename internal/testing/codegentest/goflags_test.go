package codegentest

import "testing"

func TestWithWitnessTag(t *testing.T) {
	for in, want := range map[string]string{
		"":                       "-tags=sxir_feature_witness",
		"-trimpath":              "-trimpath -tags=sxir_feature_witness",
		"-tags=sxir_x -trimpath": "-tags=sxir_x,sxir_feature_witness -trimpath",
		"-trimpath --tags=a,b":   "-trimpath --tags=a,b,sxir_feature_witness",
		"-tags= -mod=mod":        "-tags=sxir_feature_witness -mod=mod",
	} {
		if got := WithWitnessTag(in); got != want {
			t.Errorf("WithWitnessTag(%q) = %q, want %q", in, got, want)
		}
	}
}
