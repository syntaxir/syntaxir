// Package generate forwards the printer hooks of its callers to the target, which it may.
package generate

// Target is a fake target of the driver.
type Target struct{ GoHooks, TSHooks *int }

// GoWith sets the hooks h on the target.
func GoWith(h *int) Target { return Target{GoHooks: h} }

// Go passes no hooks.
func Go() Target { return GoWith(nil) }
