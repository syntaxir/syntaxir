package compile

import (
	"syntaxir.dev/sxir/pkg/grammar/ast"
)

// SetSites sets the site numbers of the program, which Check leaves unset.
func (prog *Program) SetSites(s Sites) {
	prog.sites = s
	prog.NumSkipSites, prog.NumRunSites, prog.NumScanSites = s.NumSkip, s.NumRun, s.NumScan
}

// SkipSite returns the number of the skip-run site e (from 1), or 0 if e is not one.
func (prog *Program) SkipSite(e *ast.Repeat) int { return prog.sites.Skip[e] }

// RunSite returns the number of the run site e, or 0 if e is not one. Numbers follow those of the
// skip-run sites, so a site's records share the parser's tables with them.
func (prog *Program) RunSite(e *ast.Repeat) int { return prog.sites.Run[e] }

// ScanSite returns the number of the scan site e, or 0 if e is not one; the walks that build a
// value use the next number.
func (prog *Program) ScanSite(e *ast.Repeat) int { return prog.sites.Scan[e] }

// EnvFree reports whether the skip-run or run site is free of the environment: its records are
// kept by position alone, and a walk that reuses one keeps its own environment. Otherwise the
// records are keyed by the environment too, and a walk that reuses one takes its end environment.
func (prog *Program) EnvFree(site int) bool {
	return site < len(prog.sites.EnvFree) && prog.sites.EnvFree[site]
}

// RunKey returns the variables that key the records of the run site (RunKey); a site free of the
// environment, or no run site, has none.
func (prog *Program) RunKey(site int) RunKey { return prog.sites.RunKeys[site] }

// RunKeys returns the keys of the run sites that are not free of the environment, by site.
func (prog *Program) RunKeys() map[int]RunKey { return prog.sites.RunKeys }
