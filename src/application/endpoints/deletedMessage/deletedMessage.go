package deletedMessage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	e "github.com/ChatDetectiveORG/shared/errors"
	h "github.com/ChatDetectiveORG/shared/handlers"
	models "github.com/ChatDetectiveORG/shared/postgresModels"
	"github.com/ChatDetectiveORG/shared/telegram"
	utils "github.com/ChatDetectiveORG/shared/utils"
	tele "gopkg.in/telebot.v4"

	shared "app/src/application/endpoints"
)

func NewDeletedMessageEndpoint() h.Endpoint {
	ep := h.Endpoint{}
	ep.Init(
		"deleted_message",
		*h.HandlerChain{}.Init(
			1 * time.Minute,
			h.InitChainHandler(run, h.EndOnError),
		),
		h.BusinessEvent(h.BusEventTypeDeleted),
	)

	return ep
}

func sendNotification(message *models.Message, hashe *h.HandlerChainHashe) *e.ErrorInfo  {
	botUser, err := shared.GetBotUser(message.BusinessConnectionIDHash)
	if e.IsNonNil(err) {
		return err
	}

	botUserID, err := botUser.GetTgId()
	if e.IsNonNil(err) {
		return err
	}

	key, err := utils.DecryptUserKey(botUser.DataEncryptionKey)
	if e.IsNonNil(err) {
		return e.FromError(err, "failed to decrypt user key")
	}

	decryptedMetadataJson, err := utils.Decrypt(message.Metadata, key)
	if e.IsNonNil(err) {
		return e.FromError(err, "failed to decrypt message metadata")
	}

	var metadata = &tele.Message{}
	eraw := json.Unmarshal(decryptedMetadataJson, metadata)
	if e.IsNonNil(eraw) {
		return e.FromError(eraw, "failed to unmarshal message metadata")
	}

	if metadata.Text != "" && len(metadata.Text) <= 3900 {
		// username := message.Metadata.Sender.FirstName + " " + message.Metadata.Sender.LastName
		username := strings.TrimSpace(metadata.Chat.FirstName + " " + metadata.Chat.LastName)

		prefix := fmt.Sprintf("Пользователь %s удалил сообщение!\n", username)
		originalTextLen := utils.TgLen(metadata.Text)
		prefixLen := utils.TgLen(prefix)
		usernameLen := utils.TgLen(username)

		for i := 0; i < len(metadata.Entities); i++ {
			metadata.Entities[i].Offset += prefixLen
		}

		metadata.Text = prefix + metadata.Text
		metadata.Entities = append(metadata.Entities, tele.MessageEntity{
			Type: tele.EntityTextLink,
			Offset: 13,
			Length: usernameLen,
			URL: fmt.Sprintf("tg://user?id=%d", metadata.Sender.ID),
		}, tele.MessageEntity{
			Type: tele.EntityEBlockquote,
			Offset: prefixLen,
			Length: originalTextLen,
		})
		metadata.Chat.ID = botUserID

		sentMessage, err := hashe.EmitWait(context.Background(), "telegram.message.send", metadata)
		if e.IsNonNil(err) {
			return err
		}

		summary := telegram.BuildMessageSummary(metadata).String()

		err = hashe.Emit("telegram.message.send", &tele.Message{
			Chat: &tele.Chat{
				ID: botUserID,
			},
			Text: summary,
			ReplyTo: sentMessage,
		})

		// TODO: Если не включено расширенное сохранение сообщений, удалять из бд

		return err
	}

	metadata.Chat.ID = botUserID
	sentMessage, err := hashe.EmitWait(context.Background(), "telegram.message.send", metadata)

	if e.IsNonNil(err) {
		return err
	}

	username := strings.TrimSpace(metadata.Chat.FirstName + " " + metadata.Chat.LastName)
	summary := telegram.BuildMessageSummary(metadata).String()
	prefix := fmt.Sprintf("Пользователь %s удалил сообщение!\n", username)
	usernameLen := utils.TgLen(username)

	err = hashe.Emit("telegram.message.send", &tele.Message{
		Chat: &tele.Chat{
			ID: botUserID,
		},
		Text: prefix + summary,
		ReplyTo: sentMessage,
		Entities: tele.Entities{tele.MessageEntity{
			Type: tele.EntityTextLink,
			Offset: 13,
			Length: usernameLen,
			URL: fmt.Sprintf("tg://user?id=%d", metadata.Sender.ID),
		}},
	})

	return err
}


func run(update tele.Update, hashe *h.HandlerChainHashe) *e.ErrorInfo {
	for _, deletedMessage := range update.DeletedBusinessMessages.MessageIDs {
		message, err := shared.GetMessageInfo(deletedMessage, update.DeletedBusinessMessages.BusinessConnectionID, update.DeletedBusinessMessages.Chat.ID)
		if e.IsNonNil(err) {
			return err
		}

		err = sendNotification(message, hashe)
		if e.IsNonNil(err) {
			return err
		}
	}

	return e.Nil()
}
