package print

// A fake file that breaks the name rule of the printers: its name and the names below have the word
// "flow"; overflow, Overflowed and Workflows only contain the letters.

type tsFlowType struct {
	FlowTarget int
	overflow   int
}

func printFlow(flowArg int) (HTTPFlowResult int, Overflowed bool) {
	var flowVar = flowArg
	flow := flowVar
	for _, rangeFlow := range []int{flow} {
		HTTPFlowResult += rangeFlow
	}
Flow:
	for Workflows := 0; Workflows < 1; Workflows++ {
		break Flow
	}
	return HTTPFlowResult, false
}

const flowConst = 1
