//go:build !sxir_charge_log

package state

func logCharge(*BudgetControl, uint64) {}
