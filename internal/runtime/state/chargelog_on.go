//go:build zp_charge_log

package state

import (
	"fmt"
	"runtime"
	"strings"
)

// ChargeLog, when not nil, receives each reservation with the function chain that made it.
var ChargeLog func(n uint64, where string)

func logCharge(n uint64) {
	if ChargeLog == nil {
		return
	}
	pcs := make([]uintptr, 12)
	k := runtime.Callers(3, pcs)
	fr := runtime.CallersFrames(pcs[:k])
	var b []string
	for {
		f, more := fr.Next()
		name := f.Function[strings.LastIndex(f.Function, "/")+1:]
		if strings.HasPrefix(name, "state.(*BudgetControl)") {
			if !more {
				break
			}
			continue
		}
		b = append(b, fmt.Sprintf("%s:%d", name, f.Line))
		if len(b) == 4 || !more {
			break
		}
	}
	ChargeLog(n, strings.Join(b, " < "))
}
