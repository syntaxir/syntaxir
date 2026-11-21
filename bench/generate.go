// Package bench benchmarks the SyntaxIR backends against each other and against Go standard library parsers.
//
//	go test ./bench -bench . -benchmem
//
// docs/benchmarks.md is generated from a run of these benchmarks: go run ./bench/report measures them
// and rewrites it (see package report).
//
// The generated parsers that the benchmarks run are in internal/bench/parsers, written by sxir gen;
// regenerate them with go generate ./bench. Document (minilangdoc, minilangtdoc, csvdoc) and stream (csvstream)
// parsers are separate packages, so the batch parsers keep their generated form. The inputs come from internal/bench/inputs.
package bench

//go:generate go run ../cmd/sxir gen -g ../testdata/grammars/json/json.sxir -module syntaxir.dev/sxir/internal/bench/parsers/json -lib import -recognize -types -limits -o ../internal/bench/parsers/json
//go:generate go run ../cmd/sxir gen -g ../testdata/grammars/csv/csv.sxir -module syntaxir.dev/sxir/internal/bench/parsers/csv -lib import -recognize -types -limits -o ../internal/bench/parsers/csv
//go:generate go run ../cmd/sxir gen -g ../testdata/grammars/xml/xml.sxir -module syntaxir.dev/sxir/internal/bench/parsers/xml -lib import -recognize -types -limits -o ../internal/bench/parsers/xml
//go:generate go run ../cmd/sxir gen -g ../examples/calculator/calc.sxir -module syntaxir.dev/sxir/internal/bench/parsers/calc -lib import -recognize -types -limits -o ../internal/bench/parsers/calc
//go:generate go run ../cmd/sxir gen -g ../examples/calculator/calc_lr.sxir -module syntaxir.dev/sxir/internal/bench/parsers/calclr -lib import -recognize -types -limits -o ../internal/bench/parsers/calclr
//go:generate go run ../cmd/sxir gen -g ../examples/minilang/minilang.sxir -module syntaxir.dev/sxir/internal/bench/parsers/minilang -lib import -recognize -types -limits -o ../internal/bench/parsers/minilang
//go:generate go run ../cmd/sxir gen -g ../examples/outline/outline.sxir -module syntaxir.dev/sxir/internal/bench/parsers/outline -lib import -recognize -types -limits -o ../internal/bench/parsers/outline
//go:generate go run ../cmd/sxir gen -g grammars/terms/pred.sxir -module syntaxir.dev/sxir/internal/bench/parsers/termspred -lib import -recognize -types -limits -o ../internal/bench/parsers/termspred
//go:generate go run ../cmd/sxir gen -g grammars/terms/heavy.sxir -module syntaxir.dev/sxir/internal/bench/parsers/termsheavy -lib import -recognize -types -limits -o ../internal/bench/parsers/termsheavy
//go:generate go run ../cmd/sxir gen -g ../examples/minilang/minilang.sxir -module syntaxir.dev/sxir/internal/bench/parsers/minilangdoc -lib import -limits -document -o ../internal/bench/parsers/minilangdoc
//go:generate go run ../cmd/sxir gen -g ../examples/minilang/minilang.sxir -module syntaxir.dev/sxir/internal/bench/parsers/minilangtdoc -lib import -types -limits -document -o ../internal/bench/parsers/minilangtdoc
//go:generate go run ../cmd/sxir gen -g ../testdata/grammars/csv/csv.sxir -module syntaxir.dev/sxir/internal/bench/parsers/csvdoc -lib import -limits -document -o ../internal/bench/parsers/csvdoc
//go:generate go run ../cmd/sxir gen -g ../testdata/grammars/csv/csv.sxir -module syntaxir.dev/sxir/internal/bench/parsers/csvstream -lib import -limits -stream -o ../internal/bench/parsers/csvstream
