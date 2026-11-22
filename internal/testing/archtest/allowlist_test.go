package archtest

// allowlist holds the import edges that break a rule today, in the form of violation.key. The
// test fails when a build has an edge that the list does not hold, and when the list holds an
// edge that no build has: an edge leaves the list in the change that removes it. An entry is
// added only for a dependence that already exists, when a move renames or splits it, and it names
// the stage that removes it.
var allowlist []string
