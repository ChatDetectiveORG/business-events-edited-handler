package editedMessage

import (
	"context"
	"encoding/json"

	e "github.com/ChatDetectiveORG/shared/errors"
	h "github.com/ChatDetectiveORG/shared/handlers"
	models "github.com/ChatDetectiveORG/shared/postgresModels"
	"github.com/ChatDetectiveORG/shared/telegram/notification"
	"github.com/ChatDetectiveORG/shared/telegram/rawmessage"
	utils "github.com/ChatDetectiveORG/shared/utils"
	tele "gopkg.in/telebot.v4"

	shared "github.com/ChatDetectiveORG/business-events-edited-handler/src/application/endpoints"
	"github.com/ChatDetectiveORG/business-events-edited-handler/src/infrastructure/postgresql"
)

const highlightTextDiff = true

type EditedInput struct {
	OldVersion   *tele.Message
	NewVersion   *tele.Message
	OldRaw       json.RawMessage
	NewRaw       json.RawMessage
	MessageModel *models.Message

	Key []byte

	ReciverID int64

	ActorName string
	ActorID   int64
}

func sendNotification(input *EditedInput, hashe *h.HandlerChainHashe) *e.ErrorInfo {
	groupRaws, editedIndex, err := shared.LoadMediaGroupRawMessages(input.MessageModel, input.Key)
	if e.IsNonNil(err) {
		return err
	}

	return notification.DispatchEdit(context.Background(), hashe, notification.EditDispatchInput{
		ReceiverID:       input.ReciverID,
		Actor:            notification.Actor{Name: input.ActorName, ID: input.ActorID},
		OldRaw:           input.OldRaw,
		NewRaw:           input.NewRaw,
		OldMessage:       input.OldVersion,
		NewMessage:       input.NewVersion,
		MediaGroupIDHash: input.MessageModel.MediaGroupIDHash,
		GroupRaws:        groupRaws,
		EditedIndex:      editedIndex,
		HighlightDiff:    highlightTextDiff,
		RoutingKey:       "telegram.message.send",
	})
}

func updateMessageInDatabase(message *models.Message, newMessage *tele.Message, key []byte) *e.ErrorInfo {
	db := postgresql.GetDB()

	metadataJson, eraw := rawmessage.MarshalBusinessMessage(newMessage)
	if eraw != nil {
		return e.FromError(eraw, "failed to marshal message metadata")
	}

	encryptedMetadataJson, err := utils.Encrypt(metadataJson, key)
	if e.IsNonNil(err) {
		return e.FromError(err, "failed to encrypt message metadata")
	}

	message.Metadata = encryptedMetadataJson
	message.MetadataFormat = rawmessage.MetadataFormatRawAPIv1
	_, eraw = db.Model(message).WherePK().Column("metadata", "metadata_format").Update()
	if eraw != nil {
		return e.FromError(eraw, "failed to update message in database")
	}

	return e.Nil()
}
