package pipe

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// Header is the scheme by which the header lines of a pipeline text carry the options of type T,
// a struct: each header word writes some of its fields, and the fields that no word writes are
// omitted, for the reader to supply (a writer, a file system). A field of a nested struct is named by
// its dotted path from T ("Debug.Stats"); a struct field that no place names is the sum of its
// fields. Check holds a scheme to the type, so a field added to T or to a struct inside it is a
// failure until the scheme gives it one place.
type Header[T any] struct {
	Words []HeaderWord
	Omit  []string // the fields that no word writes, by path
}

// HeaderWord is a header word and the fields of the options that its lines write, in order.
type HeaderWord struct {
	Word   string
	Fields []HeaderField
}

// HeaderField is one field of the options on a header line.
type HeaderField struct {
	Field string // the path of the Go field: its name, or the names from T joined by "."
	Key   string // the key in the text; empty: the last name of the path in lower case, words joined by "-"
	// Codec writes and reads a field whose type has no text form of its own; nil for a bool, an
	// integer, a string, a list of strings, a map from strings to integers, or a pointer to a bool,
	// an integer or a string, whose key a line leaves out for nil.
	Codec *Codec
}

// Codec is the text form of the values of one field.
type Codec struct {
	Format func(v reflect.Value) string
	Parse  func(s string, v reflect.Value) error // sets v, which is addressable
}

// Check reports every exported field of T, nested structs walked, that no word writes and that is
// not omitted, or that two places name, a struct and a field inside it included; every path that is
// no field of T; and every field without a text form. It returns nil for a scheme that writes each
// field exactly once or omits it.
func (h *Header[T]) Check() error {
	t := reflect.TypeFor[T]()
	if t.Kind() != reflect.Struct {
		return fmt.Errorf("pipe: header options %v is not a struct", t)
	}
	where := map[string][]string{}
	var errs []string
	for _, n := range h.Omit {
		where[n] = append(where[n], "omitted")
	}
	words := map[string]bool{}
	for _, w := range h.Words {
		if !wordRE.MatchString(w.Word) || w.Word == "pipeline" || words[w.Word] {
			errs = append(errs, fmt.Sprintf("header word %q is not a lower-case word, is pipeline or is given twice", w.Word))
		}
		words[w.Word] = true
		keys := map[string]bool{}
		for _, f := range w.Fields {
			where[f.Field] = append(where[f.Field], w.Word)
			k := f.key()
			if !wordRE.MatchString(k) || keys[k] {
				errs = append(errs, fmt.Sprintf("%s: key %q of %s is not a lower-case word or is given twice", w.Word, k, f.Field))
			}
			keys[k] = true
			if ft, ok := fieldType(t, f.Field); ok && f.Codec == nil && !plainKind(ft) {
				errs = append(errs, fmt.Sprintf("%s: field %s of type %v has no text form: give it a codec", w.Word, f.Field, ft))
			}
		}
	}
	fields := map[string]bool{}
	var walk func(t reflect.Type, prefix string, placed string)
	walk = func(t reflect.Type, prefix string, placed string) {
		for _, sf := range reflect.VisibleFields(t) {
			if !sf.IsExported() || sf.Anonymous {
				continue
			}
			path := prefix + sf.Name
			fields[path] = true
			at := where[path]
			if placed != "" && len(at) > 0 {
				errs = append(errs, fmt.Sprintf("field %s has a place (%s) inside %s, which has one", path, strings.Join(at, ", "), placed))
			}
			if len(at) == 0 && placed == "" && sf.Type.Kind() == reflect.Struct {
				walk(sf.Type, path+".", "")
				continue
			}
			if sf.Type.Kind() == reflect.Struct {
				inner := placed
				if inner == "" {
					inner = path
				}
				walk(sf.Type, path+".", inner)
			}
			if placed != "" {
				continue
			}
			switch len(at) {
			case 0:
				errs = append(errs, fmt.Sprintf("field %s is written by no header word and is not omitted", path))
			case 1:
			default:
				errs = append(errs, fmt.Sprintf("field %s has %d places (%s); it needs exactly one", path, len(at), strings.Join(at, ", ")))
			}
		}
	}
	walk(t, "", "")
	for _, n := range slices.Sorted(maps.Keys(where)) {
		if !fields[n] {
			errs = append(errs, fmt.Sprintf("%s is no exported field of %v", n, t))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("pipe: the header of %v: %s", t, strings.Join(errs, "; "))
	}
	return nil
}

// fieldType returns the type of the field at path in the struct type t.
func fieldType(t reflect.Type, path string) (reflect.Type, bool) {
	for _, n := range strings.Split(path, ".") {
		if t.Kind() != reflect.Struct {
			return nil, false
		}
		sf, ok := t.FieldByName(n)
		if !ok || !sf.IsExported() {
			return nil, false
		}
		t = sf.Type
	}
	return t, true
}

// fieldValue returns the field at path in the struct v, which Check has found.
func fieldValue(v reflect.Value, path string) reflect.Value {
	for _, n := range strings.Split(path, ".") {
		v = v.FieldByName(n)
	}
	return v
}

func (f HeaderField) key() string {
	if f.Key != "" {
		return f.Key
	}
	var b strings.Builder
	name := f.Field
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		name = name[i+1:]
	}
	rs := []rune(name)
	for i, r := range rs {
		// A word starts at an upper-case letter after a lower-case one or a digit, or at the last
		// upper-case letter of a run that a lower-case one follows (TSLib: ts-lib).
		if i > 0 && unicode.IsUpper(r) && (!unicode.IsUpper(rs[i-1]) || i+1 < len(rs) && unicode.IsLower(rs[i+1])) {
			b.WriteByte('-')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

func plainKind(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Bool, reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return true
	case reflect.Slice:
		return t.Elem().Kind() == reflect.String
	case reflect.Map:
		return t.Key().Kind() == reflect.String && t.Elem().Kind() == reflect.Int
	case reflect.Pointer:
		switch t.Elem().Kind() {
		case reflect.Bool, reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return true
		}
	}
	return false
}

func (h *Header[T]) word(w string) *HeaderWord {
	for i := range h.Words {
		if h.Words[i].Word == w {
			return &h.Words[i]
		}
	}
	return nil
}

// Line returns the header line of the word w with the label label, which writes the fields of v
// that w writes, but not the key of a nil pointer. It panics for a word that the scheme does not
// have.
func (h *Header[T]) Line(w, label string, v T) Line {
	hw := h.word(w)
	if hw == nil {
		panic(fmt.Sprintf("pipe: the header has no word %q", w))
	}
	l := Line{Word: w, Label: label}
	rv := reflect.ValueOf(v)
	for _, f := range hw.Fields {
		fv := fieldValue(rv, f.Field)
		if f.Codec == nil && fv.Kind() == reflect.Pointer {
			if fv.IsNil() {
				continue
			}
			fv = fv.Elem()
		}
		var s string
		if f.Codec != nil {
			s = f.Codec.Format(fv)
		} else {
			s = formatPlain(fv)
		}
		l.Fields = append(l.Fields, Field{f.key(), s})
	}
	return l
}

// Read sets the fields of v that the word of l writes from l. The line must give every key of its
// word once and no other key, so a text names every option of its scope and a misspelt key is an
// error; only the key of a pointer may be left out, which sets it to nil.
func (h *Header[T]) Read(l Line, v *T) error {
	hw := h.word(l.Word)
	if hw == nil {
		return fmt.Errorf("header: unknown header word %q", l.Word)
	}
	rv := reflect.ValueOf(v).Elem()
	var want []string
	for _, f := range hw.Fields {
		k := f.key()
		want = append(want, k)
		s, ok := l.Value(k)
		fv := fieldValue(rv, f.Field)
		if f.Codec == nil && fv.Kind() == reflect.Pointer {
			fv.Set(reflect.Zero(fv.Type()))
			if !ok {
				continue
			}
			fv.Set(reflect.New(fv.Type().Elem()))
			fv = fv.Elem()
		}
		if !ok {
			return fmt.Errorf("header %s: %s is missing", l.Word, k)
		}
		var err error
		if f.Codec != nil {
			err = f.Codec.Parse(s, fv)
		} else {
			err = parsePlain(s, fv)
		}
		if err != nil {
			return fmt.Errorf("header %s: %s: %v", l.Word, k, err)
		}
	}
	for _, f := range l.Fields {
		if !slices.Contains(want, f.Key) {
			return fmt.Errorf("header %s: unknown key %s", l.Word, f.Key)
		}
	}
	return nil
}

// The elements of a list and the entries of a map are separated by commas; an element writes a
// comma or a percent sign in it as %XX.
var elemEscaper = strings.NewReplacer("%", "%25", ",", "%2C", "=", "%3D")

func formatPlain(v reflect.Value) string {
	switch v.Kind() {
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.String:
		return v.String()
	case reflect.Slice:
		es := make([]string, v.Len())
		for i := range es {
			es[i] = elemEscaper.Replace(v.Index(i).String())
		}
		return strings.Join(es, ",")
	case reflect.Map:
		ks := v.MapKeys()
		slices.SortFunc(ks, func(a, b reflect.Value) int { return strings.Compare(a.String(), b.String()) })
		es := make([]string, len(ks))
		for i, k := range ks {
			es[i] = elemEscaper.Replace(k.String()) + "=" + strconv.FormatInt(v.MapIndex(k).Int(), 10)
		}
		return strings.Join(es, ",")
	}
	return strconv.FormatInt(v.Int(), 10)
}

func parsePlain(s string, v reflect.Value) error {
	switch v.Kind() {
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		v.SetBool(b)
		return err
	case reflect.String:
		v.SetString(s)
		return nil
	case reflect.Slice:
		v.Set(reflect.Zero(v.Type()))
		if s == "" {
			return nil
		}
		for _, e := range strings.Split(s, ",") {
			u, err := unescape(e)
			if err != nil {
				return err
			}
			v.Set(reflect.Append(v, reflect.ValueOf(u).Convert(v.Type().Elem())))
		}
		return nil
	case reflect.Map:
		v.Set(reflect.Zero(v.Type()))
		if s == "" {
			return nil
		}
		v.Set(reflect.MakeMap(v.Type()))
		for _, e := range strings.Split(s, ",") {
			k, n, ok := strings.Cut(e, "=")
			i, err := strconv.ParseInt(n, 10, 64)
			if !ok || err != nil {
				return fmt.Errorf("%q is not name=integer", e)
			}
			if k, err = unescape(k); err != nil {
				return err
			}
			kv := reflect.ValueOf(k).Convert(v.Type().Key())
			if v.MapIndex(kv).IsValid() {
				return fmt.Errorf("%s is given twice", k)
			}
			v.SetMapIndex(kv, reflect.ValueOf(i).Convert(v.Type().Elem()))
		}
		return nil
	}
	i, err := strconv.ParseInt(s, 10, v.Type().Bits())
	v.SetInt(i)
	return err
}
