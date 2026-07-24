package editedMessage

import (
	"time"

	h "github.com/ChatDetectiveORG/shared/handlers"
)

func NewEditedMessageEndpoint() h.Endpoint {
	ep := h.Endpoint{}
	ep.Init(
		"edited_message",
		*h.HandlerChain{}.Init(
			1*time.Minute,
			h.InitChainHandler(run, h.EndOnError),
		),
		h.And(h.BusinessEvent(h.BusEventTypeEdited)),
	)

	return ep
}
