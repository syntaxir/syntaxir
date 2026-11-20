//go:build !zp_charge_log

package state

func logCharge(*BudgetControl, uint64) {}
