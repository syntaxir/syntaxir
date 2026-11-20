package print

import "zeroparse.dev/zp/internal/backend/codegen/common"

// typedFrameComment returns the comment of the matcher of the body of the typed rule r that runs in
// place under the runtime-owned frame (flowplan.TypedFrameOK): a left-recursion leader or a rule that
// recovers.
func typedFrameComment(r *common.Rule) string {
	if r.Leader {
		return "typed LR body inlined"
	}
	return "typed recovery body inlined"
}
