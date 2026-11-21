package sxirls

import (
	"encoding/json"

	"syntaxir.dev/sxir/internal/lsp/protocol"
)

func (s *Server) codeActions(params json.RawMessage) (any, *protocol.RPCError) {
	return renderRequest(s.requestContext, func(control *renderControl) (any, *protocol.RPCError) {
		return s.codeActionsControlled(params, control)
	})
}
