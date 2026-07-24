package deletedMessage

import (
	"context"
	"encoding/json"
	"slices"

	e "github.com/ChatDetectiveORG/shared/errors"
	h "github.com/ChatDetectiveORG/shared/handlers"
	models "github.com/ChatDetectiveORG/shared/postgresModels"
	"github.com/ChatDetectiveORG/shared/telegram/notification"
	tele "gopkg.in/telebot.v4"

	shared "github.com/ChatDetectiveORG/business-events-edited-handler/src/application/endpoints"
)

type DeletedInput struct {
	TeleMessage  *tele.Message
	Raw          json.RawMessage
	MessageModel *models.Message

	ignoredMessagesIDs []int

	Key []byte

	ReciverID int64

	ActorName string
	ActorID   int64
}

func sendNotification(input *DeletedInput, hashe *h.HandlerChainHashe) *e.ErrorInfo {
	if slices.Contains(input.ignoredMessagesIDs, input.MessageModel.MessageID) {
		return e.Nil()
	}

	groupRaws, _, err := shared.LoadMediaGroupRawMessages(input.MessageModel, input.Key)
	if e.IsNonNil(err) {
		return err
	}

	return notification.DispatchDelete(context.Background(), hashe, notification.DeleteDispatchInput{
		ReceiverID: input.ReciverID,
		Actor:      notification.Actor{Name: input.ActorName, ID: input.ActorID},
		Raw:        input.Raw,
		Message:          input.TeleMessage,
		MediaGroupIDHash: input.MessageModel.MediaGroupIDHash,
		GroupRaws:        groupRaws,
		RoutingKey:       "telegram.message.send",
	})
}
