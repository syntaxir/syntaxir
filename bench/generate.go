// Package bench benchmarks the SyntaxIR backends against each other and against Go standard library parsers.
//
//	go test ./bench -bench . -benchmem
//
// docs/benchmarks.md is generated from a run of these benchmarks: go run ./bench/report measures them
// and rewrites it (see package report).
//
// The generated parsers that the benchmarks run are in internal/bench/parsers, written by sxir gen;
// regenerate them with go generate ./bench. Document (minilangdoc, minilangtdoc, csvdoc) and stream (csvstream)
// parsers are separate packages, so the batch parsers keep their generated form. A parser with limit controls
// counts logical compute units (design record 076); the references of their
// variants (-charge-units) are separate packages: *logical is the default form, *event counts each call where it
// happens, *keepguards keeps every guard without the effects of the calls it skips, and *plain is the same grammar
// without limit controls. The inputs come from internal/bench/inputs.
package bench

//go:generate go run ../cmd/sxir gen -g ../testdata/grammars/json/json.sxir -module syntaxir.dev/sxir/internal/bench/parsers/json -lib import -recognize -types -o ../internal/bench/parsers/json
//go:generate go run ../cmd/sxir gen -g ../testdata/grammars/csv/csv.sxir -module syntaxir.dev/sxir/internal/bench/parsers/csv -lib import -recognize -types -o ../internal/bench/parsers/csv
//go:generate go run ../cmd/sxir gen -g ../testdata/grammars/xml/xml.sxir -module syntaxir.dev/sxir/internal/bench/parsers/xml -lib import -recognize -types -o ../internal/bench/parsers/xml
//go:generate go run ../cmd/sxir gen -g ../examples/calculator/calc.sxir -module syntaxir.dev/sxir/internal/bench/parsers/calc -lib import -recognize -types -o ../internal/bench/parsers/calc
//go:generate go run ../cmd/sxir gen -g ../examples/calculator/calc_lr.sxir -module syntaxir.dev/sxir/internal/bench/parsers/calclr -lib import -recognize -types -o ../internal/bench/parsers/calclr
//go:generate go run ../cmd/sxir gen -g ../examples/minilang/minilang.sxir -module syntaxir.dev/sxir/internal/bench/parsers/minilang -lib import -recognize -types -o ../internal/bench/parsers/minilang
//go:generate go run ../cmd/sxir gen -g ../examples/outline/outline.sxir -module syntaxir.dev/sxir/internal/bench/parsers/outline -lib import -recognize -types -o ../internal/bench/parsers/outline
//go:generate go run ../cmd/sxir gen -g grammars/actions/pred.sxir -module syntaxir.dev/sxir/internal/bench/parsers/actionspred -lib import -recognize -types -o ../internal/bench/parsers/actionspred
//go:generate go run ../cmd/sxir gen -g grammars/actions/heavy.sxir -module syntaxir.dev/sxir/internal/bench/parsers/actionsheavy -lib import -recognize -types -o ../internal/bench/parsers/actionsheavy
//go:generate go run ../cmd/sxir gen -g ../examples/minilang/minilang.sxir -module syntaxir.dev/sxir/internal/bench/parsers/minilangdoc -lib import -document -o ../internal/bench/parsers/minilangdoc
//go:generate go run ../cmd/sxir gen -g ../examples/minilang/minilang.sxir -module syntaxir.dev/sxir/internal/bench/parsers/minilangtdoc -lib import -types -document -o ../internal/bench/parsers/minilangtdoc
//go:generate go run ../cmd/sxir gen -g ../testdata/grammars/csv/csv.sxir -module syntaxir.dev/sxir/internal/bench/parsers/csvdoc -lib import -document -o ../internal/bench/parsers/csvdoc
//go:generate go run ../cmd/sxir gen -g ../testdata/grammars/csv/csv.sxir -module syntaxir.dev/sxir/internal/bench/parsers/csvstream -lib import -stream -o ../internal/bench/parsers/csvstream
//go:generate go run ../cmd/sxir gen -g ../testdata/grammars/json/json.sxir -module syntaxir.dev/sxir/internal/bench/parsers/jsonplain -lib import -types -no-limits -o ../internal/bench/parsers/jsonplain
//go:generate go run ../cmd/sxir gen -g ../testdata/grammars/json/json.sxir -module syntaxir.dev/sxir/internal/bench/parsers/jsonlogical -lib import -types -o ../internal/bench/parsers/jsonlogical
//go:generate go run ../cmd/sxir gen -g ../testdata/grammars/json/json.sxir -module syntaxir.dev/sxir/internal/bench/parsers/jsonevent -lib import -types -charge-units=event -o ../internal/bench/parsers/jsonevent
//go:generate go run ../cmd/sxir gen -g ../testdata/grammars/json/json.sxir -module syntaxir.dev/sxir/internal/bench/parsers/jsonkeepguards -lib import -types -charge-units=keep-guards -o ../internal/bench/parsers/jsonkeepguards
//go:generate go run ../cmd/sxir gen -g ../testdata/grammars/xml/xml.sxir -module syntaxir.dev/sxir/internal/bench/parsers/xmlplain -lib import -types -no-limits -o ../internal/bench/parsers/xmlplain
//go:generate go run ../cmd/sxir gen -g ../testdata/grammars/xml/xml.sxir -module syntaxir.dev/sxir/internal/bench/parsers/xmllogical -lib import -types -o ../internal/bench/parsers/xmllogical
//go:generate go run ../cmd/sxir gen -g ../testdata/grammars/xml/xml.sxir -module syntaxir.dev/sxir/internal/bench/parsers/xmlevent -lib import -types -charge-units=event -o ../internal/bench/parsers/xmlevent
//go:generate go run ../cmd/sxir gen -g ../testdata/grammars/xml/xml.sxir -module syntaxir.dev/sxir/internal/bench/parsers/xmlkeepguards -lib import -types -charge-units=keep-guards -o ../internal/bench/parsers/xmlkeepguards
//go:generate go run ../cmd/sxir gen -g grammars/logical_policy.sxir -module syntaxir.dev/sxir/internal/bench/parsers/policylogical -lib import -types -o ../internal/bench/parsers/policylogical
//go:generate go run ../cmd/sxir gen -g grammars/logical_policy.sxir -module syntaxir.dev/sxir/internal/bench/parsers/policyevent -lib import -types -charge-units=event -o ../internal/bench/parsers/policyevent
//go:generate go run ../cmd/sxir gen -g grammars/budget_recover.sxir -module syntaxir.dev/sxir/internal/bench/parsers/recoverlogical -lib import -types -o ../internal/bench/parsers/recoverlogical
//go:generate go run ../cmd/sxir gen -g grammars/budget_recover.sxir -module syntaxir.dev/sxir/internal/bench/parsers/recoverevent -lib import -types -charge-units=event -o ../internal/bench/parsers/recoverevent
//go:generate go run ../cmd/sxir gen -g grammars/logical_chain.sxir -module syntaxir.dev/sxir/internal/bench/parsers/chainlogical -lib import -types -o ../internal/bench/parsers/chainlogical
//go:generate go run ../cmd/sxir gen -g grammars/logical_chain.sxir -module syntaxir.dev/sxir/internal/bench/parsers/chainevent -lib import -types -charge-units=event -o ../internal/bench/parsers/chainevent
//go:generate go run ../cmd/sxir gen -g grammars/logical_keyed.sxir -module syntaxir.dev/sxir/internal/bench/parsers/keyedlogical -lib import -types -o ../internal/bench/parsers/keyedlogical
//go:generate go run ../cmd/sxir gen -g grammars/logical_keyed.sxir -module syntaxir.dev/sxir/internal/bench/parsers/keyedevent -lib import -types -charge-units=event -o ../internal/bench/parsers/keyedevent
//go:generate go run ../cmd/sxir gen -g grammars/logical_hand.sxir -module syntaxir.dev/sxir/internal/bench/parsers/handlogical -lib import -o ../internal/bench/parsers/handlogical
