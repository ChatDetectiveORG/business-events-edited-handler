package deletedMessage

import (
	"time"

	h "github.com/ChatDetectiveORG/shared/handlers"
)

func NewDeletedMessageEndpoint() h.Endpoint {
	ep := h.Endpoint{}
	ep.Init(
		"deleted_message",
		*h.HandlerChain{}.Init(
			10*time.Second,
			h.InitChainHandler(run, h.EndOnError),
		),
		h.BusinessEvent(h.BusEventTypeDeleted),
	)

	return ep
}
