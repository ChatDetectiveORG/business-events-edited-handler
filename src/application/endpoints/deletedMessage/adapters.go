package deletedMessage

import (
	"encoding/json"

	e "github.com/ChatDetectiveORG/shared/errors"
	h "github.com/ChatDetectiveORG/shared/handlers"
	models "github.com/ChatDetectiveORG/shared/postgresModels"
	utils "github.com/ChatDetectiveORG/shared/utils"
	tele "gopkg.in/telebot.v4"

	shared "github.com/ChatDetectiveORG/business-events-edited-handler/src/application/endpoints"
	"github.com/ChatDetectiveORG/business-events-edited-handler/src/infrastructure/postgresql"
)

func run(update tele.Update, hashe *h.HandlerChainHashe) *e.ErrorInfo {
	input := &DeletedInput{
		TeleMessage:        nil,
		MessageModel:       nil,
		Key:                []byte{},
		ReciverID:          0,
		ActorName:          "",
		ActorID:            0,
		ignoredMessagesIDs: []int{},
	}

	for _, deletedMessage := range update.DeletedBusinessMessages.MessageIDs {
		message, err := shared.GetMessageInfo(deletedMessage, update.DeletedBusinessMessages.BusinessConnectionID, update.DeletedBusinessMessages.Chat.ID)
		if e.IsNonNil(err) {
			return err
		}

		input.MessageModel = message

		botUser, err := shared.ResolveBotUser(update.DeletedBusinessMessages.BusinessConnectionID, nil)
		if e.IsNonNil(err) {
			return err
		}

		botUserID, err := botUser.GetTgId()
		if e.IsNonNil(err) {
			return err
		}

		input.ReciverID = botUserID
		input.ActorName = update.DeletedBusinessMessages.Chat.FirstName + " " + update.DeletedBusinessMessages.Chat.LastName
		input.ActorID = update.DeletedBusinessMessages.Chat.ID

		key, err := utils.DecryptUserKey(botUser.DataEncryptionKey)
		if e.IsNonNil(err) {
			return err
		}

		input.Key = key

		metadataJson, err := utils.Decrypt(message.Metadata, key)
		if e.IsNonNil(err) {
			return err
		}

		var metadata = &tele.Message{}
		eRaw := json.Unmarshal(metadataJson, metadata)
		if e.IsNonNil(eRaw) {
			return e.FromError(eRaw, "failed to unmarshal message metadata")
		}

		input.TeleMessage = metadata

		if message.SenderIDHash == botUser.IDHash {
			db := postgresql.GetDB()

			var interlocutor = &models.Telegramuser{}
			err = interlocutor.GetByTelegramID(db, update.DeletedBusinessMessages.Chat.ID)
			if e.IsNonNil(err) {
				continue
			}

			if !interlocutor.IsConnected {
				input.ReciverID = update.DeletedBusinessMessages.Chat.ID
				input.ActorName, err = botUser.GetFullName()
				if e.IsNonNil(err) {
					continue
				}
				input.ActorID = botUserID
			}
		}

		canReceive, err := shared.CanReceiveByHierarchy(input.ReciverID, input.ActorID)
		if e.IsNonNil(err) {
			return err
		}
		if !canReceive {
			continue
		}

		err = sendNotification(input, hashe)
		if e.IsNonNil(err) {
			return err
		}
	}

	return e.Nil()
}
