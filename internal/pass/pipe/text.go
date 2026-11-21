package pipe

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"syntaxir.dev/sxir/internal/pass"
)

// The text door: a pipeline prints as text, and a text reads back to a pipeline. Describe prints
// the step tree with the instances that the manager names, Parse reads a text without the
// registries, and Resolve checks it against the registries and against the pipeline that the
// driver assembled for the same build, and builds the step tree. The text is line-oriented:
//
//	text     = "sxir-pipeline" SP version NL { header } "pipeline" SP kind SP "->" SP kind SP "{" NL { item } "}" NL .
//	version  = digits "." digits .
//	header   = word [ SP label ] { SP key "=" value } NL .
//	item     = step NL | block .
//	step     = pass [ "#" digits ] [ "!" ] [ "(" param { "," param } ")" ] [ "[" pattern { "," pattern } "]" ] .
//	param    = flag "=" integer .
//	block    = opener SP "{" NL { item } "}" { SP "for" SP label SP "{" NL { item } "}" } NL .
//	opener   = "fork" | "branch" SP label | "each" SP split | "func" SP view | "within" SP lens
//	         | "shared" SP key | "with" SP ( provider | feature "=" value ) | "slot" SP slot .
//
// Indentation (two spaces per level) and blank lines mean nothing to the reader. Labels and values
// are escaped (escape), so a module path or a library specifier round-trips.

// Format is the version of the pipeline text that this package writes. It reads the texts of the
// same major version and of this minor version or an older one.
const Format = "1.0"

// Description is a pipeline as text: its headers, its kinds and its step tree as the text names
// them, before Resolve checks it against the registries. Its String is the canonical text, and
// Parse(d.String()) describes the same pipeline.
type Description struct {
	Version string // the version of the format that the text is written in
	Header  []Line // what a build needs besides the pipeline; the reader of the build interprets them
	In, Out string // the kinds that the pipeline takes and returns
	body    []item
}

// Line is a header line: a word, an optional label (the version after sxir, the backend after
// target) and fields, in order.
type Line struct {
	Word   string
	Label  string
	Fields []Field
}

// Field is a key and its value on a header line.
type Field struct{ Key, Value string }

// Value returns the value of the field key and whether the line has it.
func (l Line) Value(key string) (string, bool) {
	for _, f := range l.Fields {
		if f.Key == key {
			return f.Value, true
		}
	}
	return "", false
}

// item is one element of a block: a pass (op "") or a block.
type item struct {
	line     int    // the line of a parsed text, for errors; 0 in a described one
	op       string // "" for a pass; fork, branch, func, each, within, shared, with, slot
	name     string // the pass, the label of a branch, or the registered name the block opens
	value    string // with: the value of a constant feature
	constant bool   // with: name is a feature and value its value, not a provider
	required bool
	instance int // the k of name#k as a parsed text gives it, which only String writes back
	params   []param
	patterns []string
	body     []item
	parts    []part // slot: the bodies for the parts of the enclosing each
}

type param struct {
	flag  string
	value int
}

type part struct {
	line  int
	label string
	body  []item
}

// Describe returns the description of p with the header lines h. The passes are named by their
// instances (name#k for the k-th occurrence on a path of execution), as dumps and errors name them.
func Describe[I, O any](p Pipeline[I, O], h ...Line) *Description {
	root, ins := instantiate(p.n)
	in, out := root.kinds()
	return &Description{Version: Format, Header: slices.Clone(h), In: in.name, Out: out.name, body: describe(root, ins)}
}

func describe(n node, ins map[*passNode]string) []item {
	switch x := n.(type) {
	case *passNode:
		it := item{name: x.p.Name, required: x.p.Required, patterns: slices.Clone(x.p.Patterns)}
		if _, k, ok := strings.Cut(ins[x], "#"); ok {
			it.instance, _ = strconv.Atoi(k)
		}
		for _, f := range x.p.Params { // in declaration order
			if v, ok := x.params[f]; ok {
				it.params = append(it.params, param{f, v})
			}
		}
		return []item{it}
	case *seqNode, *chainNode:
		var out []item
		children(n, func(c node) { out = append(out, describe(c, ins)...) })
		return out
	case *perFuncNode:
		return []item{{op: "func", name: x.view.name, body: describe(x.body, ins)}}
	case *eachNode:
		return []item{{op: "each", name: x.split.name, body: describe(x.body, ins)}}
	case *withinNode:
		return []item{{op: "within", name: x.lens.name, body: describe(x.body, ins)}}
	case *sharedNode:
		return []item{{op: "shared", name: x.key.name, body: describe(x.body, ins)}}
	case *withNode:
		if x.prov.constant {
			return []item{{op: "with", name: string(x.prov.feature), value: x.prov.text, constant: true, body: describe(x.body, ins)}}
		}
		return []item{{op: "with", name: x.prov.name, body: describe(x.body, ins)}}
	case *slotNode:
		it := item{op: "slot", name: x.slot.name, body: describe(x.def, ins)}
		for _, l := range x.order {
			it.parts = append(it.parts, part{label: l, body: describe(x.parts[l], ins)})
		}
		return []item{it}
	case *forkNode:
		it := item{op: "fork"}
		for i, b := range x.branches {
			it.body = append(it.body, item{op: "branch", name: x.names[i], body: describe(b, ins)})
		}
		return []item{it}
	}
	panic(fmt.Sprintf("pipe: no text form for %T", n))
}

// String returns the canonical text of d.
func (d *Description) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "sxir-pipeline %s\n", d.Version)
	for _, l := range d.Header {
		b.WriteString(l.Word)
		if l.Label != "" {
			b.WriteString(" " + escape(l.Label, true))
		}
		for _, f := range l.Fields {
			b.WriteString(" " + f.Key + "=" + escape(f.Value, false))
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "pipeline %s -> %s {\n", d.In, d.Out)
	writeItems(&b, d.body, 1)
	b.WriteString("}\n")
	return b.String()
}

func writeItems(b *strings.Builder, its []item, depth int) {
	ind := strings.Repeat("  ", depth)
	for _, it := range its {
		if it.op == "" {
			b.WriteString(ind + it.name)
			if it.instance > 0 {
				b.WriteString("#" + strconv.Itoa(it.instance))
			}
			if it.required {
				b.WriteString("!")
			}
			if len(it.params) > 0 {
				ps := make([]string, len(it.params))
				for i, p := range it.params {
					ps[i] = p.flag + "=" + strconv.Itoa(p.value)
				}
				b.WriteString("(" + strings.Join(ps, ",") + ")")
			}
			if len(it.patterns) > 0 {
				b.WriteString("[" + strings.Join(it.patterns, ",") + "]")
			}
			b.WriteString("\n")
			continue
		}
		switch {
		case it.op == "fork":
			b.WriteString(ind + "fork {\n")
		case it.constant:
			fmt.Fprintf(b, "%swith %s=%s {\n", ind, it.name, escape(it.value, false))
		default:
			fmt.Fprintf(b, "%s%s %s {\n", ind, it.op, escape(it.name, true))
		}
		writeItems(b, it.body, depth+1)
		for _, p := range it.parts {
			fmt.Fprintf(b, "%s} for %s {\n", ind, escape(p.label, true))
			writeItems(b, p.body, depth+1)
		}
		b.WriteString(ind + "}\n")
	}
}

// escape writes the bytes that would end a token of the text as %XX: the percent sign, spaces,
// control bytes, and in a label or a name also "=", which would make it a field, and the braces.
func escape(s string, label bool) string {
	var b strings.Builder
	for i := range len(s) {
		c := s[i]
		if c == '%' || c <= ' ' || c == 0x7f || label && (c == '=' || c == '{' || c == '}') {
			fmt.Fprintf(&b, "%%%02X", c)
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

func unescape(s string) (string, error) {
	if !strings.Contains(s, "%") {
		return s, nil
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			b.WriteByte(s[i])
			continue
		}
		if i+2 >= len(s) {
			return "", fmt.Errorf("%q ends inside an escape", s)
		}
		v, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
		if err != nil {
			return "", fmt.Errorf("%q has the invalid escape %q", s, s[i:i+3])
		}
		b.WriteByte(byte(v))
		i += 2
	}
	return b.String(), nil
}

var (
	wordRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	stepRE = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_.-]*)(?:#([0-9]+))?(!)?(?:\(([^()]*)\))?(?:\[([^\[\]]*)\])?$`)
)

// Parse reads a pipeline text. It rejects another major version and a newer minor version, and
// a text that does not follow the grammar; it does not look at the registries (Resolve does).
func Parse(text string) (*Description, error) {
	p := &parser{lines: strings.Split(text, "\n")}
	first, ok := p.next()
	v, found := strings.CutPrefix(first, "sxir-pipeline ")
	if !ok || !found {
		return nil, p.errorf("want sxir-pipeline <version>")
	}
	if err := checkVersion(v); err != nil {
		return nil, p.errorf("%v", err)
	}
	d := &Description{Version: v}
	for {
		ln, ok := p.next()
		if !ok {
			return nil, p.errorf("the text has no pipeline line")
		}
		if strings.HasPrefix(ln, "pipeline ") {
			f := strings.Fields(ln)
			if len(f) != 5 || f[2] != "->" || f[4] != "{" {
				return nil, p.errorf("want pipeline <kind> -> <kind> {")
			}
			d.In, d.Out = f[1], f[3]
			break
		}
		l, err := parseHeader(ln)
		if err != nil {
			return nil, p.errorf("%v", err)
		}
		d.Header = append(d.Header, l)
	}
	body, end, err := p.block()
	if err != nil {
		return nil, err
	}
	if end != "}" {
		return nil, p.errorf("the pipeline block ends with %q, want }", end)
	}
	if _, ok := p.next(); ok {
		return nil, p.errorf("text after the end of the pipeline")
	}
	d.body = body
	return d, nil
}

func checkVersion(v string) error {
	maj, min, ok := strings.Cut(v, ".")
	wmaj, wmin, _ := strings.Cut(Format, ".")
	a, err1 := strconv.Atoi(maj)
	b, err2 := strconv.Atoi(min)
	if !ok || err1 != nil || err2 != nil {
		return fmt.Errorf("version %q is not <major>.<minor>", v)
	}
	wa, _ := strconv.Atoi(wmaj)
	wb, _ := strconv.Atoi(wmin)
	if a != wa || b > wb {
		return fmt.Errorf("pipeline text version %s: this reader reads %d.0 to %s", v, wa, Format)
	}
	return nil
}

func parseHeader(ln string) (Line, error) {
	f := strings.Fields(ln)
	l := Line{Word: f[0]}
	if !wordRE.MatchString(l.Word) {
		return l, fmt.Errorf("header word %q is not a lower-case word", l.Word)
	}
	f = f[1:]
	if len(f) > 0 && !strings.Contains(f[0], "=") {
		label, err := unescape(f[0])
		if err != nil {
			return l, err
		}
		l.Label, f = label, f[1:]
	}
	for _, kv := range f {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || !wordRE.MatchString(k) {
			return l, fmt.Errorf("header %s: %q is not key=value", l.Word, kv)
		}
		if _, dup := l.Value(k); dup {
			return l, fmt.Errorf("header %s: %s is given twice", l.Word, k)
		}
		v, err := unescape(v)
		if err != nil {
			return l, fmt.Errorf("header %s: %s: %v", l.Word, k, err)
		}
		l.Fields = append(l.Fields, Field{k, v})
	}
	return l, nil
}

type parser struct {
	lines []string
	i     int // the number of the line that next returned, from 1
}

// next returns the next line that is not blank, without its indentation.
func (p *parser) next() (string, bool) {
	for p.i < len(p.lines) {
		ln := strings.TrimSpace(p.lines[p.i])
		p.i++
		if ln != "" {
			return ln, true
		}
	}
	return "", false
}

func (p *parser) errorf(format string, a ...any) error {
	return fmt.Errorf("pipeline text: line %d: %s", p.i, fmt.Sprintf(format, a...))
}

// openers maps the word of a block to whether it names something.
var openers = map[string]bool{"fork": false, "branch": true, "func": true, "each": true, "within": true, "shared": true, "with": true, "slot": true}

// block reads items up to the line that closes the block, which it returns: "}" or "} for <label> {".
func (p *parser) block() ([]item, string, error) {
	var out []item
	for {
		ln, ok := p.next()
		if !ok {
			return nil, "", p.errorf("a block is not closed")
		}
		if strings.HasPrefix(ln, "}") {
			return out, ln, nil
		}
		line := p.i
		head, isBlock := strings.CutSuffix(ln, " {")
		if !isBlock {
			it, err := parseStep(ln)
			if err != nil {
				return nil, "", p.errorf("%v", err)
			}
			it.line = line
			out = append(out, it)
			continue
		}
		f := strings.Fields(head)
		named, known := openers[f[0]]
		if !known || named != (len(f) == 2) || len(f) > 2 {
			return nil, "", p.errorf("unknown block %q", ln)
		}
		it := item{line: line, op: f[0]}
		if named {
			name := f[1]
			if it.op == "with" {
				var value string
				if name, value, it.constant = strings.Cut(name, "="); it.constant {
					v, err := unescape(value)
					if err != nil {
						return nil, "", p.errorf("%v", err)
					}
					it.value = v
				}
			}
			n, err := unescape(name)
			if err != nil {
				return nil, "", p.errorf("%v", err)
			}
			it.name = n
		}
		body, end, err := p.block()
		if err != nil {
			return nil, "", err
		}
		it.body = body
		for end != "}" {
			label, ok := strings.CutPrefix(end, "} for ")
			if label, ok = strings.CutSuffix(label, " {"); !ok || strings.Contains(label, " ") {
				return nil, "", p.errorf("want } or } for <label> {")
			}
			if it.op != "slot" {
				return nil, "", p.errorf("%s %s has a body for the part %s; only a slot has bodies for parts", it.op, it.name, label)
			}
			l, err := unescape(label)
			if err != nil {
				return nil, "", p.errorf("%v", err)
			}
			pt := part{line: p.i, label: l}
			if pt.body, end, err = p.block(); err != nil {
				return nil, "", err
			}
			it.parts = append(it.parts, pt)
		}
		out = append(out, it)
	}
}

func parseStep(tok string) (item, error) {
	m := stepRE.FindStringSubmatch(tok)
	if m == nil {
		return item{}, fmt.Errorf("%q is no pass step", tok)
	}
	it := item{name: m[1], required: m[3] == "!"}
	// The instance number is informational: the resolver numbers the instances itself, and only
	// String writes it back, so that a parsed text prints as it was written.
	if m[2] != "" {
		it.instance, _ = strconv.Atoi(m[2])
	}
	if m[4] != "" {
		for _, kv := range strings.Split(m[4], ",") {
			k, v, ok := strings.Cut(kv, "=")
			n, err := strconv.Atoi(v)
			if !ok || err != nil {
				return item{}, fmt.Errorf("pass %s: parameter %q is not flag=integer", it.name, kv)
			}
			it.params = append(it.params, param{k, n})
		}
	}
	if m[5] != "" {
		it.patterns = strings.Split(m[5], ",")
	}
	return it, nil
}

// Resolve checks d against the registries and returns the pipeline typed at its edges. It checks by
// kinds what the compiler checks for a typed pipeline, what Execute checks before any step runs,
// and that the required skeleton of d is that of ref, the pipeline assembled for the same build:
// its required steps, the blocks that hold them, in order, and every constant feature with its
// value. So a text can reorder, repeat, insert or remove optional passes, and set their parameters,
// only. An unknown name is an error that names it.
func Resolve[I, O any](d *Description, ref Pipeline[I, O]) (Pipeline[I, O], error) {
	var zero Pipeline[I, O]
	in, out := registeredKind(reflect.TypeFor[I]()), registeredKind(reflect.TypeFor[O]())
	if in == nil || out == nil {
		return zero, fmt.Errorf("%w: the kinds of the pipeline are not registered", errCheck)
	}
	if d.In != in.name || d.Out != out.name {
		return zero, fmt.Errorf("%w: the text takes %s to %s, and the pipeline %s to %s", errCheck, d.In, d.Out, in.name, out.name)
	}
	n, err := resolveBlock(d.body, in, out, "pipeline", 0)
	if err != nil {
		return zero, err
	}
	if err := check(n, map[FeatureID]bool{}, map[string]bool{}); err != nil {
		return zero, err
	}
	if err := checkShape(n, nil); err != nil {
		return zero, err
	}
	if got, want := skeleton(n), skeleton(ref.n); got != want {
		return zero, fmt.Errorf("%w: the required steps of the text differ from those of the assembled pipeline: %s", errCheck, firstDifference(got, want))
	}
	return Pipeline[I, O]{n}, nil
}

func registeredKind(t reflect.Type) *kind {
	listMu.Lock()
	defer listMu.Unlock()
	return kindsByType[t]
}

// firstDifference names where two skeletons part: the shared context and what each has there.
func firstDifference(got, want string) string {
	i := 0
	for i < len(got) && i < len(want) && got[i] == want[i] {
		i++
	}
	// Back up to the start of the token, a block opener or a step.
	j := strings.LastIndexAny(got[:i], "{};") + 1
	clip := func(s string) string {
		s = s[j:]
		if k := strings.IndexAny(s, "{};"); k >= 0 {
			s = s[:k+1]
		}
		if s == "" {
			return "nothing"
		}
		return strconv.Quote(s)
	}
	return fmt.Sprintf("after %q the text has %s where the pipeline has %s", got[:j], clip(got), clip(want))
}

func resolveErr(line int, format string, a ...any) error {
	if line > 0 {
		return fmt.Errorf("%w: line %d: %s", errCheck, line, fmt.Sprintf(format, a...))
	}
	return fmt.Errorf("%w: %s", errCheck, fmt.Sprintf(format, a...))
}

// resolveBlock resolves the items of a block that takes units of in; out, if not nil, is the kind
// it must return. A block is a chain even of one step, as in the normal form.
func resolveBlock(its []item, in, out *kind, where string, line int) (node, error) {
	cur := in
	c := &chainNode{}
	for _, it := range its {
		n, err := resolveItem(it, cur)
		if err != nil {
			return nil, err
		}
		a, b := n.kinds()
		if a != cur {
			return nil, resolveErr(it.line, "%s takes %s, but it gets %s", describeItem(it), a.name, cur.name)
		}
		c.steps = append(c.steps, n)
		cur = b
	}
	if out != nil && cur != out {
		return nil, resolveErr(line, "%s returns %s, want %s", where, cur.name, out.name)
	}
	if len(c.steps) == 0 {
		return &seqNode{k: in}, nil
	}
	return c, nil
}

func describeItem(it item) string {
	if it.op == "" {
		return "pass " + it.name
	}
	return strings.TrimSpace(it.op + " " + it.name)
}

func resolveItem(it item, in *kind) (node, error) {
	what := describeItem(it)
	switch it.op {
	case "":
		return resolvePass(it)
	case "func":
		v := views[it.name]
		if v == nil {
			return nil, resolveErr(it.line, "no view %s", it.name)
		}
		b, err := resolveBlock(it.body, v.part, v.part, what, it.line)
		return &perFuncNode{view: v, body: b}, err
	case "each":
		s := splits[it.name]
		if s == nil {
			return nil, resolveErr(it.line, "no split %s", it.name)
		}
		b, err := resolveBlock(it.body, s.part, s.done, what, it.line)
		return &eachNode{split: s, body: b}, err
	case "within":
		l := lenses[it.name]
		if l == nil {
			return nil, resolveErr(it.line, "no lens %s", it.name)
		}
		b, err := resolveBlock(it.body, l.part, l.partOut, what, it.line)
		return &withinNode{lens: l, body: b}, err
	case "shared":
		k := keys[it.name]
		if k == nil {
			return nil, resolveErr(it.line, "no shared key %s", it.name)
		}
		if k.k != in {
			return nil, resolveErr(it.line, "shared key %s reads %s, but its block starts on %s", it.name, k.k.name, in.name)
		}
		b, err := resolveBlock(it.body, in, nil, what, it.line)
		return &sharedNode{key: k, body: b}, err
	case "with":
		pr, err := readProvider(it, in)
		if err != nil {
			return nil, err
		}
		b, err := resolveBlock(it.body, in, nil, what, it.line)
		return &withNode{prov: pr, body: b}, err
	case "slot":
		s := slots[it.name]
		if s == nil {
			return nil, resolveErr(it.line, "no slot %s", it.name)
		}
		def, err := resolveBlock(it.body, s.k, s.k, what, it.line)
		if err != nil {
			return nil, err
		}
		sn := &slotNode{slot: s, skel: skeleton(def), def: def, parts: map[string]node{}}
		for _, p := range it.parts {
			if sn.parts[p.label] != nil {
				return nil, resolveErr(p.line, "slot %s has two bodies for the part %s", it.name, p.label)
			}
			b, err := resolveBlock(p.body, s.k, s.k, what+" for "+p.label, p.line)
			if err != nil {
				return nil, err
			}
			sn.parts[p.label], sn.order = b, append(sn.order, p.label)
		}
		return sn, nil
	case "fork":
		f := &forkNode{in: in}
		var out *kind
		for _, br := range it.body {
			if br.op != "branch" {
				return nil, resolveErr(br.line, "a fork holds branches only, not %s", describeItem(br))
			}
			b, err := resolveBlock(br.body, in, out, "branch "+br.name, br.line)
			if err != nil {
				return nil, err
			}
			_, out = b.kinds()
			f.names, f.branches = append(f.names, br.name), append(f.branches, b)
		}
		if out == nil {
			return nil, resolveErr(it.line, "a fork without branches")
		}
		f.out = listKind(out)
		return f, nil
	case "branch":
		return nil, resolveErr(it.line, "branch %s is outside a fork", it.name)
	}
	return nil, resolveErr(it.line, "unknown block %s", it.op)
}

func resolvePass(it item) (node, error) {
	p := passes[it.name]
	if p == nil {
		return nil, resolveErr(it.line, "no pass %s", it.name)
	}
	if it.required != p.Required {
		if p.Required {
			return nil, resolveErr(it.line, "pass %s is required: write %s!", it.name, it.name)
		}
		return nil, resolveErr(it.line, "pass %s is optional: write %s without !", it.name, it.name)
	}
	if !slices.Equal(it.patterns, p.Patterns) {
		return nil, resolveErr(it.line, "pass %s lists the patterns [%s], but declares [%s]", it.name, strings.Join(it.patterns, ","), strings.Join(p.Patterns, ","))
	}
	n := &passNode{p: p}
	for _, pr := range it.params {
		if !contains(p.Params, pr.flag) {
			return nil, resolveErr(it.line, "pass %s has no parameter %s", it.name, pr.flag)
		}
		if _, dup := n.params[pr.flag]; dup {
			return nil, resolveErr(it.line, "pass %s: parameter %s is given twice", it.name, pr.flag)
		}
		if _, err := (pass.Settings{}).With(pr.flag, pr.value); err != nil {
			return nil, resolveErr(it.line, "pass %s: %v", it.name, err)
		}
		if n.params == nil {
			n.params = map[string]int{}
		}
		n.params[pr.flag] = pr.value
	}
	return n, nil
}

func readProvider(it item, in *kind) (*provDesc, error) {
	if !it.constant {
		pr := providers[it.name]
		if pr == nil {
			return nil, resolveErr(it.line, "no provider %s", it.name)
		}
		if pr.in != in {
			return nil, resolveErr(it.line, "provider %s reads %s, but its block starts on %s", it.name, pr.in.name, in.name)
		}
		return pr, nil
	}
	f := features[FeatureID(it.name)]
	if f == nil {
		return nil, resolveErr(it.line, "no feature %s", it.name)
	}
	if f.parse == nil {
		return nil, resolveErr(it.line, "feature %s has no text form, so a text cannot provide it", it.name)
	}
	v, err := f.parse(it.value)
	if err != nil {
		return nil, resolveErr(it.line, "feature %s: %v", it.name, err)
	}
	return &provDesc{feature: FeatureID(it.name), text: f.format(v), constant: true, value: func(any) any { return v }, format: f.format}, nil
}
