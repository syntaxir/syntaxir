//go:build !sxir_reference_expectation_filter

package libgo

// expectationFilter lets the bits of the expectations of the current record skip the scan for an
// expectation that is not there yet. The reference build (sxir_reference_expectation_filter) never
// consults them and always scans the record in order.
const expectationFilter = true
