package editedMessage

import (
	e "github.com/ChatDetectiveORG/shared/errors"
	h "github.com/ChatDetectiveORG/shared/handlers"
	utils "github.com/ChatDetectiveORG/shared/utils"
	tele "gopkg.in/telebot.v4"

	shared "github.com/ChatDetectiveORG/business-events-edited-handler/src/application/endpoints"
	"github.com/ChatDetectiveORG/business-events-edited-handler/src/application/filters"
	"github.com/ChatDetectiveORG/business-events-edited-handler/src/infrastructure/postgresql"
	models "github.com/ChatDetectiveORG/shared/postgresModels"
)

func run(update tele.Update, hashe *h.HandlerChainHashe) *e.ErrorInfo {
	message, err := shared.GetMessageInfo(update.EditedBusinessMessage.ID, update.EditedBusinessMessage.BusinessConnectionID, update.EditedBusinessMessage.Chat.ID)
	if e.IsNonNil(err) {
		return err
	}

	input := &EditedInput{
		OldVersion:   update.EditedBusinessMessage,
		NewVersion:   update.EditedBusinessMessage,
		MessageModel: message,

		Key:       []byte{},
		ReciverID: update.EditedBusinessMessage.Chat.ID,
		ActorName: update.EditedBusinessMessage.Chat.FirstName + " " + update.EditedBusinessMessage.Chat.LastName,
		ActorID:   update.EditedBusinessMessage.Chat.ID,
	}

	businessConnectionIDHash, err := utils.ToSecureHash(update.EditedBusinessMessage.BusinessConnectionID)
	if e.IsNonNil(err) {
		return err
	}

	botUser, err := shared.GetBotUser(businessConnectionIDHash)
	if e.IsNonNil(err) {
		return err
	}

	botUserID, err := botUser.GetTgId()
	if e.IsNonNil(err) {
		return err
	}

	key, err := utils.DecryptUserKey(botUser.DataEncryptionKey)
	if e.IsNonNil(err) {
		return err
	}

	input.Key = key

	actorIsBotUser := (&filters.ActorIsNotSelf{}).Filter(update)

	if actorIsBotUser {
		input.ReciverID = botUserID
		input.ActorName = update.EditedBusinessMessage.Chat.FirstName + " " + update.EditedBusinessMessage.Chat.LastName
	} else {
		interlocutorIDHash, err := utils.ToSecureHash(update.EditedBusinessMessage.Chat.ID)
		if e.IsNonNil(err) {
			return err
		}

		interlocutor := &models.Telegramuser{
			IDHash: interlocutorIDHash,
		}

		db := postgresql.GetDB()

		err = interlocutor.GetByTelegramID(db, update.EditedBusinessMessage.Chat.ID)
		if e.IsNonNil(err) {
			return e.Nil()
		}

		if interlocutor.BusinessConnectionIDHash == "" {
			interlocutorID, err := interlocutor.GetTgId()
			if e.IsNonNil(err) {
				return e.Nil()
			}

			input.ReciverID = interlocutorID
			input.ActorName, err = botUser.GetFullName()
			if e.IsNonNil(err) {
				return e.Nil()
			}
			input.ActorID = botUserID
		}
	}

	canReceive, err := shared.CanReceiveByHierarchy(input.ReciverID, input.ActorID)
	if e.IsNonNil(err) {
		return err
	}
	if !canReceive {
		return e.Nil()
	}

	err = sendNotification(input, hashe)
	if e.IsNonNil(err) {
		return err
	}

	err = updateMessageInDatabase(message, update.EditedBusinessMessage)
	if e.IsNonNil(err) {
		return err
	}

	return e.Nil()
}
