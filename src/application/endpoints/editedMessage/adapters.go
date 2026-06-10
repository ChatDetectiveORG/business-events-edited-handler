package editedMessage

import (
	e "github.com/ChatDetectiveORG/shared/errors"
	h "github.com/ChatDetectiveORG/shared/handlers"
	utils "github.com/ChatDetectiveORG/shared/utils"
	tele "gopkg.in/telebot.v4"

	shared "github.com/ChatDetectiveORG/business-events-edited-handler/src/application/endpoints"
	"github.com/ChatDetectiveORG/business-events-edited-handler/src/application/filters"
)

func run(update tele.Update, hashe *h.HandlerChainHashe) *e.ErrorInfo {
	message, err := shared.GetMessageInfo(update.EditedBusinessMessage.ID, update.EditedBusinessMessage.BusinessConnectionID, update.EditedBusinessMessage.Chat.ID)
	if e.IsNonNil(err) {
		return err
	}

	oldVersion, err := shared.GetMetadata(message)
	if e.IsNonNil(err) {
		return err
	}

	input := &EditedInput{
		OldVersion:   oldVersion,
		NewVersion:   update.EditedBusinessMessage,
		MessageModel: message,

		Key:       []byte{},
		ReciverID: update.EditedBusinessMessage.Chat.ID,
		ActorName: update.EditedBusinessMessage.Chat.FirstName + " " + update.EditedBusinessMessage.Chat.LastName,
		ActorID:   update.EditedBusinessMessage.Chat.ID,
	}

	botUser, err := shared.ResolveBotUser(update.EditedBusinessMessage.BusinessConnectionID, update.EditedBusinessMessage)
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

	actorIsInterlocutor := (&filters.ActorIsNotSelf{}).Filter(update)

	if actorIsInterlocutor {
		input.ReciverID = botUserID
		input.ActorName = update.EditedBusinessMessage.Chat.FirstName + " " + update.EditedBusinessMessage.Chat.LastName
	} else {
		return e.Nil()
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

	return updateMessageInDatabase(message, update.EditedBusinessMessage)
}
