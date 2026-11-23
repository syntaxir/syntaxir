package codegentest

// ExaminedChar is a call of the rule atom at position 0 of the grammar "rule main = atom" and
// "rule atom = " Atom, on Input in Bytes or in code points, whose exact examined end
// (spec/runtime/observability.md, Examined; spec/runtime/documents.md, Slots) the case states by hand.
type ExaminedChar struct {
	Name, Atom, Input string
	Bytes             bool
	Matched           bool
	Examined          int
}

// Src is the grammar of the case.
func (c ExaminedChar) Src() string { return "rule main = atom\nrule atom = " + c.Atom }

// ExaminedChars are calls whose bodies compare characters: a literal compares the bytes of each of its
// characters up to the first that differs, a case-insensitive literal too, the guard of a choice
// examines what the alternative it skips would, `$` examines the slot at its position alone, and only a
// class (or U+FFFD) decodes a character. Every backend must report these examined ends. The cases
// marked decode were over-reported when a guard, a case-insensitive literal or `$` decoded the whole
// character.
func ExaminedChars() []ExaminedChar {
	return []ExaminedChar{
		// C2 differs from '-', 'a' and the C3 of é at byte 0: every guard rejects there.
		{"guards reject at the first byte (decode)", `"-x" / "a" / "é"`, "¬", true, false, 1},
		{"guards reject a truncated character (decode)", `"-x" / "a" / "é"`, "\xc2", true, false, 1},
		{"guarded choice in an atomic expression (decode)", `t:("-x" / "a" / "é") #atomic`, "¬", true, false, 1},
		// ì is C3 AC: é's guard compares C3 and rejects at AC.
		{"guard rejects at the second byte", `"-x" / "a" / "é"`, "ì", true, false, 2},
		{"guarded alternative matches", `"-x" / "a" / "é"`, "é", true, true, 2},
		{"lone literal", `"é"`, "¬", true, false, 1},
		{"guards in code points", `"-x" / "a" / "é"`, "¬", false, false, 1},
		// A class decodes the character it tests, so its guard does too.
		{"class guard decodes", `[a-z] / "é"`, "¬", true, false, 2},
		{"case-insensitive literal (decode)", `"éa"i`, "¬", true, false, 1},
		{"case-insensitive literal, digit first (decode)", `"1x"i`, "¬", true, false, 1},
		{"case-insensitive guards (decode)", `"ax"i / "1x"i`, "¬", true, false, 1},
		{"case-insensitive literal folds ASCII", `"ax"i`, "AX", true, true, 2},
		{"case-insensitive literal, second character", `"aé"i`, "Aì", true, false, 3},
		// U+FFFD matches an invalid byte, which only decoding tells.
		{"replacement literal decodes", `"�" / "a"`, "\xff", true, true, 1},
		{"replacement literal, other characters by bytes (decode)", `"é�"`, "¬", true, false, 1},
		// $ at 2 reads the C3 of é alone: it is neither LF nor CR.
		{"line end before a multibyte character (decode)", `"na" $`, "naé", true, false, 3},
		{"line end before a line feed", `"na" $`, "na\n", true, true, 3},
		{"line end at the end", `"na" $`, "na", true, true, 3},
	}
}
