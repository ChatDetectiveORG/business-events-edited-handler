package endpoints

import (
	"app/src/infrastructure/postgresql"
	"context"
	"fmt"
	"strings"
	"time"

	e "github.com/ChatDetectiveORG/shared/errors"
	h "github.com/ChatDetectiveORG/shared/handlers"
	models "github.com/ChatDetectiveORG/shared/postgresModels"
	"github.com/ChatDetectiveORG/shared/telegram"
	utils "github.com/ChatDetectiveORG/shared/utils"
	tele "gopkg.in/telebot.v4"
)

func NewDeletedMessageEndpoint() h.Endpoint {
	ep := h.Endpoint{}
	ep.Init(
		"save",
		*h.HandlerChain{}.Init(
			10 * time.Second,
			h.InitChainHandler(run, h.EndOnError),
		),
		h.BusinessEvent(h.BusEventTypeNew),
	)

	return ep
}

func getDeletedMessageInfo(mid int, update tele.Update, hashe *h.HandlerChainHashe) (*models.Message, *e.ErrorInfo) {
	message := &models.Message{
		ChatID: utils.ToHash(update.DeletedBusinessMessages.Chat.ID),
		MessageID: mid,
		BusinessConnectionID: update.DeletedBusinessMessages.BusinessConnectionID,
	}

	db := postgresql.GetDB()
	err := db.Model(message).
		Where("chat_id = ? AND message_id = ? AND business_connection_id = ?", message.ChatID, message.MessageID, utils.ToHash(message.BusinessConnectionID)).
		Select()
	if e.IsNonNil(err) {
		return nil,e.FromError(err, "failed to select deleted message")
	}

	hashe.Add("db_message", message)

	return message, e.Nil()
}

func getBotUserID(businessConnectionID string) (int64, *e.ErrorInfo) {
	user := &models.Telegramuser{
		BusinessConnectionID: businessConnectionID,
	}

	db := postgresql.GetDB()
	err := db.Model(user).
		Where("business_connection_id = ?", utils.ToHash(businessConnectionID)).
		Select()
	if e.IsNonNil(err) {
		return 0, e.FromError(err, "failed to select bot user")
	}

	id, err := user.GetTgId()
	if e.IsNonNil(err) {
		return 0, e.FromError(err, "failed to get bot user id")
	}

	return id, e.Nil()
}

func sendNotification(message *models.Message, hashe *h.HandlerChainHashe) *e.ErrorInfo  {
	if message.Metadata.Text != "" && len(message.Metadata.Text) <= 3900 {
		botUserID, err := getBotUserID(message.BusinessConnectionID)
		if e.IsNonNil(err) {
			return err
		}

		// username := message.Metadata.Sender.FirstName + " " + message.Metadata.Sender.LastName
		username := strings.TrimSpace(message.Metadata.Chat.FirstName + " " + message.Metadata.Chat.LastName)

		prefix := fmt.Sprintf("Пользователь %s удалил сообщение!\n", username)
		originalTextLen := utils.TgLen(message.Metadata.Text)
		prefixLen := utils.TgLen(prefix)
		usernameLen := utils.TgLen(username)

		for i := 0; i < len(message.Metadata.Entities); i++ {
			message.Metadata.Entities[i].Offset += prefixLen
		}

		message.Metadata.Text = prefix + message.Metadata.Text
		message.Metadata.Entities = append(message.Metadata.Entities, tele.MessageEntity{
			Type: tele.EntityTextLink,
			Offset: 13,
			Length: usernameLen,
			URL: fmt.Sprintf("tg://user?id=%d", message.Metadata.Sender.ID),
		}, tele.MessageEntity{
			Type: tele.EntityEBlockquote,
			Offset: prefixLen,
			Length: originalTextLen,
		})
		message.Metadata.Chat.ID = botUserID

		sentMessage, err := hashe.EmitWait(context.Background(), "telegram.message.send", message.Metadata)
		if e.IsNonNil(err) {
			return err
		}

		summary := telegram.BuildMessageSummary(message.Metadata).String()

		err = hashe.Emit("telegram.message.send", &tele.Message{
			Chat: &tele.Chat{
				ID: botUserID,
			},
			Text: summary,
			Entities: message.Metadata.Entities,
			ReplyTo: sentMessage,
		})

		return err
	}

	botUserID, err := getBotUserID(message.BusinessConnectionID)
	message.Metadata.Chat.ID = botUserID
	sentMessage, err := hashe.EmitWait(context.Background(), "telegram.message.send", message.Metadata)

	if e.IsNonNil(err) {
		return err
	}

	username := strings.TrimSpace(message.Metadata.Chat.FirstName + " " + message.Metadata.Chat.LastName)
	summary := telegram.BuildMessageSummary(message.Metadata).String()
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
			URL: fmt.Sprintf("tg://user?id=%d", message.Metadata.Sender.ID),
		}},
	})

	return err
}


func run(update tele.Update, hashe *h.HandlerChainHashe) *e.ErrorInfo {
	for _, deletedMessage := range update.DeletedBusinessMessages.MessageIDs {
		message, err := getDeletedMessageInfo(deletedMessage, update, hashe)
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
