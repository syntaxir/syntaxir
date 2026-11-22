// Command tool sets the printer hooks outside the tests, which the rule reports.
package main

import "example.com/pipe/internal/driver/generate"

func main() {
	var h int
	_ = generate.GoWith(&h)
	_ = generate.GoWith(nil)
	t := generate.Target{GoHooks: &h, TSHooks: nil}
	t.TSHooks = &h
	t.GoHooks = nil
	_ = t
}
