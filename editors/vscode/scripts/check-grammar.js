// Checks the extension's static files: that the JSON files parse, that every regular expression of
// the TextMate grammar compiles with Oniguruma (the engine VS Code uses), and that the grammar
// tokenizes the example and ready-made parser grammars of the repository and a sample with the expected scopes.
//
// Run with `npm run check` after `npm install`.
'use strict';

const fs = require('fs');
const path = require('path');
const oniguruma = require('vscode-oniguruma');
const vsctm = require('vscode-textmate');

const root = path.join(__dirname, '..');
const repo = path.join(root, '..', '..');
let failures = 0;

function fail(message) {
  console.error('FAIL: ' + message);
  failures++;
}

function readJSON(file) {
  try {
    return JSON.parse(fs.readFileSync(path.join(root, file), 'utf8'));
  } catch (err) {
    fail(`${file}: ${err.message}`);
    return undefined;
  }
}

// collectRegexes returns the regular expressions of a grammar rule and the rules it contains.
function collectRegexes(rule, where, out) {
  if (!rule || typeof rule !== 'object') {
    return out;
  }
  for (const key of ['match', 'begin', 'end', 'while']) {
    if (typeof rule[key] === 'string') {
      out.push({ where: `${where}.${key}`, source: rule[key] });
    }
  }
  for (const [key, value] of Object.entries(rule)) {
    if (Array.isArray(value)) {
      value.forEach((r, i) => collectRegexes(r, `${where}.${key}[${i}]`, out));
    } else if (typeof value === 'object') {
      collectRegexes(value, `${where}.${key}`, out);
    }
  }
  return out;
}

// expectations are substrings of the sample, a scope each must have (or, after "!", must not
// have), and optionally a context after which the substring is looked for.
const sample = [
  'package calc',
  '// A comment',
  '/* outer',
  '  /* inner */ still comment',
  '*/',
  'type Num = { Text string } // trailing',
  'type Op = {',
  '    Left  Num // the left operand',
  '    Right *Num',
  '}',
  'type Node ~ Op | Num | []node[string] | error',
  'rule expr Node = l:term rest:(op:"+" r:term)* => foldl($l, $rest, (acc, i) => Op{Left: acc, Right: i.$r})',
  'rule term = [0-9_\\-]+ #atomic / "\\u{1F600}\\q" @error(message="expected (a term)")  #stream',
  'rule calc = #operators {',
  '    skip " "*',
  '    operand num',
  '    level { infix left "+" => $lhs }',
  '}',
  'rule main = ^^ expr? $$ {n := len($x) > 0 && true} #cut _ _|_ . x #repeat(2,3) !y &z -w',
  'rule string = "s" string',
  'rule old = "a" => $2',
  'rule a = default_value typed_x',
  'rule typed = typed T{}',
  'type U ~ (A',
  '    | B) // the other',
  'rule x² = y',
  'rule idx = xs:a+ => $xs[0][len($xs) - 1]',
  'rule pidx = xs:a+ {$xs[1] == nil} name [q-r]',
  'rule cacc = xs:(y:a)+ => map($xs, (i) => i.$y)',
  'rule cast = x:a => $x.(Num | []Num).startPos',
  'type Top ~ _ | []_|_ & Num',
  'type Tup = {',
  '  F node[Num, (Num,)]',
  '  G (Num, Pair)',
  '  H { Inner Num; Other []{ Deep Num } }',
  '  I Num',
  '}',
  'var depth int = 0',
  'var open = true',
  'rule scoped = {depth := depth + 1} "x"',
].join('\n');

const expectations = [
  ['package', 'keyword.other.package.sxir'],
  ['calc', 'entity.name.namespace.sxir'],
  ['// A comment', 'comment.line.double-slash.sxir'],
  ['outer', 'comment.block.sxir'],
  ['inner', 'comment.block.sxir'],
  ['still comment', 'comment.block.sxir'],
  ['type', 'storage.type.sxir'],
  ['Num', 'entity.name.type.sxir'],
  ['// trailing', 'comment.line.double-slash.sxir'],
  ['=', 'keyword.operator.assignment.sxir', 'type Op'],
  ['Left', 'variable.other.property.sxir'],
  ['// the left operand', 'comment.line.double-slash.sxir'],
  ['Right', 'variable.other.property.sxir'],
  ['*', 'keyword.operator.type.sxir'],
  ['Node', 'entity.name.type.sxir'],
  ['~', 'keyword.operator.alias.sxir'],
  ['|', 'keyword.operator.join.sxir'],
  ['error', 'support.type.builtin.sxir'],
  ['rule', 'storage.type.function.sxir'],
  ['expr', 'entity.name.function.sxir'],
  ['l', 'variable.other.capture.sxir'],
  ['"+"', 'string.quoted.double.sxir'],
  ['=>', 'keyword.operator.arrow.sxir'],
  ['foldl', 'support.function.builtin.sxir'],
  ['$rest', 'variable.parameter.capture.sxir'],
  ['Op', 'entity.name.type.sxir', '=> Op'],
  ['$r', 'variable.parameter.capture.sxir', 'i.$r'],
  ['term', 'entity.name.function.sxir', 'rule term'],
  ['[', 'punctuation.definition.character-class.begin.sxir', '[0-9'],
  ['-', 'keyword.operator.range.sxir', '0-9'],
  ['\\-', 'constant.character.escape.sxir'],
  ['#', 'punctuation.definition.attribute.sxir', '#atomic'],
  ['atomic', 'entity.other.attribute-name.sxir'],
  ['/', 'keyword.operator.choice.sxir'],
  ['\\u{1F600}', 'constant.character.escape.sxir'],
  ['\\q', 'invalid.illegal.escape.sxir'],
  ['error', 'entity.other.attribute-name.sxir'],
  ['message', 'variable.parameter.attribute.sxir'],
  ['"expected (a term)"', 'string.quoted.double.sxir'],
  ['stream', 'entity.other.attribute-name.sxir'],
  ['#operators', 'keyword.control.operators-rule.sxir'],
  ['skip', 'keyword.control.operators-rule.sxir'],
  ['level', 'keyword.control.operators-rule.sxir'],
  ['left', 'keyword.other.associativity.sxir'],
  ['$lhs', 'variable.parameter.capture.sxir'],
  ['^^', 'keyword.operator.anchor.sxir'],
  ['?', 'keyword.operator.quantifier.sxir', 'expr?'],
  ['$$', 'keyword.operator.anchor.sxir'],
  ['n', '!variable.other.capture.sxir', '{n'],
  [':=', 'keyword.operator.assignment.sxir'],
  ['len', 'support.function.builtin.sxir'],
  ['&&', 'keyword.operator.sxir'],
  ['true', 'constant.language.sxir'],
  ['#cut', 'keyword.control.cut.sxir'],
  ['_', 'constant.language.top.sxir', ' _ '],
  ['_|_', 'constant.language.bottom.sxir'],
  ['.', 'constant.language.any.sxir', ' . '],
  ['repeat', 'entity.other.attribute-name.sxir', '#repeat'],
  ['!', 'keyword.operator.lookahead.sxir', '!y'],
  ['-', 'keyword.operator.discard.sxir', '-w'],
  ['string', 'entity.name.function.sxir', 'rule string'],
  ['string', '!support.type.builtin.sxir', '"s" string'],
  ['$2', 'invalid.deprecated.positional-capture.sxir'],
  ['default_value', '!storage.type.function.sxir'],
  ['typed_x', '!storage.type.sxir'],
  ['typed', 'entity.name.function.sxir', 'rule typed'],
  ['typed', '!storage.type.sxir', '= typed'],
  ['T', 'entity.name.type.sxir', 'typed T'],
  ['{}', '!keyword.operator.quantifier.sxir'],
  ['|', 'keyword.operator.join.sxir', '    | B'],
  ['B', 'entity.name.type.sxir', '| B'],
  ['// the other', 'comment.line.double-slash.sxir'],
  ['²', '!entity.name.function.sxir', 'rule x²'],
  ['[', '!punctuation.definition.character-class.begin.sxir', '[0]'],
  ['len', 'support.function.builtin.sxir', 'len($xs) - 1'],
  ['[', '!punctuation.definition.character-class.begin.sxir', '[1]'],
  ['[', 'punctuation.definition.character-class.begin.sxir', '[q-r]'],
  ['$y', 'variable.parameter.capture.sxir', 'i.$y'],
  ['Num', 'entity.name.type.sxir', '(Num |'],
  ['Num', 'entity.name.type.sxir', '[]Num)'],
  ['startPos', 'variable.other.property.sxir', '.startPos'],
  ['_', 'support.type.builtin.sxir', '~ _ '],
  ['_|_', 'support.type.builtin.sxir', '[]_|_'],
  ['&', 'keyword.operator.meet.sxir', '_ & Num'],
  ['node', 'support.type.builtin.sxir', 'F node['],
  ['[', 'punctuation.bracket.sxir', 'node[Num'],
  ['Num', 'entity.name.type.sxir', ', (Num,)'],
  ['G', 'variable.other.property.sxir', '  G ('],
  ['Pair', 'entity.name.type.sxir', 'Num, Pair'],
  ['Inner', 'variable.other.property.sxir', '{ Inner Num'],
  ['Num', 'entity.name.type.sxir', 'Inner Num;'],
  ['Other', 'variable.other.property.sxir', '; Other'],
  ['Deep', 'variable.other.property.sxir', '{ Deep'],
  ['I', 'variable.other.property.sxir', '  I Num'],
  ['var', 'storage.type.sxir', 'var depth'],
  ['depth', 'variable.other.sxir', 'var depth'],
  ['int', 'support.type.builtin.sxir', 'depth int'],
  ['=', 'keyword.operator.assignment.sxir', 'int ='],
  ['var', 'storage.type.sxir', 'var open'],
  ['open', 'variable.other.sxir', 'var open'],
  ['depth', '!variable.other.sxir', 'scoped = {depth'],
];

async function main() {
  for (const file of ['package.json', 'language-configuration.json', '.vscode/launch.json']) {
    readJSON(file);
  }
  const grammarFile = 'syntaxes/sxir.tmLanguage.json';
  const grammarJSON = readJSON(grammarFile);
  if (!grammarJSON) {
    return;
  }

  const wasm = fs.readFileSync(require.resolve('vscode-oniguruma/release/onig.wasm'));
  await oniguruma.loadWASM(wasm.buffer.slice(wasm.byteOffset, wasm.byteOffset + wasm.byteLength));
  const regexes = collectRegexes(grammarJSON, 'grammar', []);
  for (const { where, source } of regexes) {
    try {
      new oniguruma.OnigScanner([source]).dispose();
    } catch (err) {
      fail(`${where}: ${source}: ${err.message}`);
    }
  }
  console.log(`${regexes.length} regular expressions compile`);

  const registry = new vsctm.Registry({
    onigLib: Promise.resolve({
      createOnigScanner: (sources) => new oniguruma.OnigScanner(sources),
      createOnigString: (s) => new oniguruma.OnigString(s),
    }),
    loadGrammar: async (scopeName) =>
      scopeName === 'source.sxir'
        ? vsctm.parseRawGrammar(fs.readFileSync(path.join(root, grammarFile), 'utf8'), grammarFile)
        : null,
  });
  const grammar = await registry.loadGrammar('source.sxir');

  // tokenize returns the tokens of text with their offsets in it.
  function tokenize(text) {
    const tokens = [];
    let state = vsctm.INITIAL;
    let offset = 0;
    for (const line of text.split('\n')) {
      const r = grammar.tokenizeLine(line, state);
      for (const t of r.tokens) {
        tokens.push({ start: offset + t.startIndex, end: offset + t.endIndex, scopes: t.scopes });
      }
      state = r.ruleStack;
      offset += line.length + 1;
    }
    return tokens;
  }

  // The sample: each expectation's text, found after its context if one is given, must be covered
  // by tokens that all have the scope.
  const tokens = tokenize(sample);
  let from = 0;
  for (const [text, scope, context] of expectations) {
    let start;
    if (context) {
      const c = sample.indexOf(context, from);
      start = c < 0 ? -1 : sample.indexOf(text, c);
    } else {
      start = sample.indexOf(text, from);
    }
    if (start < 0) {
      fail(`sample: ${JSON.stringify(text)} not found after offset ${from}`);
      continue;
    }
    const end = start + text.length;
    const covering = tokens.filter((t) => t.start < end && t.end > start);
    const negated = scope.startsWith('!');
    const name = negated ? scope.slice(1) : scope;
    const bad = covering.filter((t) => t.scopes.includes(name) === negated);
    if (bad.length > 0) {
      const want = negated ? `not ${name}` : name;
      fail(`sample: ${JSON.stringify(text)} at ${start} has scopes ${JSON.stringify(bad[0].scopes)}, want ${want}`);
    }
    from = start;
  }
  console.log(`${expectations.length} scopes of the sample are as expected`);

  // The example and ready-made parser grammars are valid, so nothing in them is marked invalid.
  let files = 0;
  for (const top of ['examples', 'parsers']) {
    const base = path.join(repo, top);
    for (const dir of fs.readdirSync(base)) {
      const d = path.join(base, dir);
      if (!fs.statSync(d).isDirectory()) {
        continue;
      }
      for (const name of fs.readdirSync(d).filter((n) => n.endsWith('.sxir'))) {
        const file = path.join(d, name);
        const text = fs.readFileSync(file, 'utf8');
        let state = vsctm.INITIAL;
        text.split('\n').forEach((line, i) => {
          const r = grammar.tokenizeLine(line, state);
          for (const t of r.tokens) {
            const invalid = t.scopes.find((s) => s.startsWith('invalid.'));
            if (invalid) {
              fail(`${path.relative(repo, file)}:${i + 1}: ${invalid} at ${JSON.stringify(line.slice(t.startIndex, t.endIndex))}`);
            }
          }
          state = r.ruleStack;
        });
        files++;
      }
    }
  }
  console.log(`${files} example and parser grammars tokenized`);
}

main()
  .catch((err) => fail(err.stack || String(err)))
  .finally(() => {
    if (failures > 0) {
      console.error(`${failures} failure(s)`);
      process.exit(1);
    }
    console.log('ok');
  });
