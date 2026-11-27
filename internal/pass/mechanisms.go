package pass

// The declarations of the optimization catalog (design record 080): the mechanisms that are classified
// as optimizations, the reference controls, the names of removed optimizations, and the mechanisms not
// classified yet. go generate ./internal/optcatalog writes the catalog's tables from them.

var mechanisms = []Mechanism{
	{ID: "A070", Title: "Independent initial typed allocations before slab growth", Page: "A070-typed-output-bootstrap.md",
		Scope: "Typed Go output", Full: On(BackendTypedGo)},
	{ID: "A250", Title: "Typed object/terminal slab growth", Page: "A250-typed-object-slabs.md",
		Scope: "Typed Go output", Full: On(BackendTypedGo)},
	{ID: "A260", Title: "Typed list slab growth", Page: "A260-typed-list-slabs.md",
		Scope: "Typed Go output", Full: On(BackendTypedGo)},
	{ID: "A280", Title: "Typed slab and match chunk growth past the fixed limit", Page: "A280-typed-chunk-growth.md",
		Scope: "Typed Go output", Full: On(BackendTypedGo)},
	{ID: "G010", Title: "Share exact generated helper bodies", Page: "G010-generated-helper-sharing.md",
		Scope: "Generated parsers with exactly equal bodies", Partial: Generated},
	{ID: "G070", Title: "Pure literal-text disjunction lowering", Page: "G070-literal-text-predicates.md",
		Scope: "Predicates that compare a capture's text with literals, without limit controls", Partial: Generated},
	{ID: "G130", Title: "Share generated literal tables", Page: "G130-generated-literal-sharing.md",
		Scope: "Generated parsers with equal literals; TypeScript as an opt-in", Partial: Generated},
	{ID: "G220", Title: "Outlined TypeScript nodes", Page: "G220-outlined-typescript-nodes.md",
		Scope: "Large and repeated nodes of TypeScript Node and recognizer tables", Partial: On(BackendTypeScript)},
	{ID: "G240", Title: "Compact TypeScript Exec forms", Page: "G240-typescript-exec-forms.md",
		Scope: "Expectation merges and tested calls in TypeScript", Partial: On(BackendTypeScript)},
	{ID: "G250", Title: "Library of the used features", Page: "G250-used-library-features.md",
		Scope: "Generated parsers with an embedded library", Partial: Generated},
	{ID: "M010", Title: "First-character guards for ordered choices", Page: "M010-first-character-dispatch.md",
		Scope: "Choices whose alternatives have known first characters", Partial: Engines | Generated},
	{ID: "M040", Title: "Value-free character repetition scan loops", Page: "M040-character-scan-loops.md",
		Scope: "Value-free repetitions of a class or of any character", Partial: Engines | Generated},
	{ID: "M130", Title: "In-place short literal comparison", Page: "M130-short-literal-comparisons.md",
		Scope: "Short literals of generated Go", Partial: GeneratedGo},
	{ID: "M140", Title: "Run large value-free keyword choices as one table-driven program", Page: "M140-terminal-programs.md",
		Scope: "Lean typed Go; value-free choices of at least 16 literals and classes", Partial: On(BackendTypedGo)},
	{ID: "M150", Title: "Match consecutive value-free classes as one run", Page: "M150-class-runs.md",
		Scope: "Lean typed Go; two or more consecutive classes", Partial: On(BackendTypedGo)},
	{ID: "M160", Title: "Match large classes through a shared membership table", Page: "M160-class-tables.md",
		Scope: "Lean typed Go; classes of at least the configured number of ranges", Partial: On(BackendTypedGo)},
}

var referenceControls = []ReferenceControl{}

var removedFlags = []Removed{
	{Name: "canonicalize", Record: 142},
	{Name: "drop-after-exit", Record: 142},
	{Name: "drop-unused-labels", Record: 142},
	{Name: "fold-branches", Record: 142},
	{Name: "fold-constants", Record: 142},
	{Name: "splice-blocks", Record: 142},
	{Name: "ts-spend-runs", Record: 145},
}

var unclassified = []string{
	"A010", "A020", "A030", "A040", "A050", "A060", "A080", "A090", "A100", "A110", "A120", "A140", "A150",
	"A160", "A170", "A180", "A190", "A200", "A210", "A220", "A230", "A240", "A270",
	"E010", "E020", "E030", "E040", "E050", "E060",
	"G020", "G030", "G040", "G050", "G060", "G080", "G085", "G090", "G100", "G110", "G120", "G125", "G140",
	"G150", "G160", "G170", "G180", "G190", "G200", "G210",
	"M020", "M030", "M050", "M060", "M070", "M080", "M090", "M100", "M110", "M120",
	"R010", "R020", "R030", "R040", "R050", "R060", "R070", "R080", "R090", "R100", "R110", "R120", "R130",
	"R140", "R150", "R160", "R170", "R180",
}
