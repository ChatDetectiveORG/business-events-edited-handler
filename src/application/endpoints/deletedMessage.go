package endpoints

import (
	"app/src/infrastructure/postgresql"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
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
	botUser, err := getBotUser(update.DeletedBusinessMessages.BusinessConnectionID)
	if e.IsNonNil(err) {
		return nil, err
	}

	key, err := utils.DecryptUserKey(botUser.DataEncryptionKey)
	if e.IsNonNil(err) {
		return nil, e.FromError(err, "failed to decrypt user key")
	}

	encryptedChatId, err := utils.Encrypt([]byte(strconv.FormatInt(update.DeletedBusinessMessages.Chat.ID, 10)), key)
	if e.IsNonNil(err) {
		return nil, e.FromError(err, "failed to encrypt chat id")
	}
	
	message := &models.Message{
		ChatID: encryptedChatId,
		MessageID: mid,
		BusinessConnectionIDHash: utils.ToHash(update.DeletedBusinessMessages.BusinessConnectionID),
	}

	db := postgresql.GetDB()
	eraw := db.Model(message).
		Where("chat_id = ? AND message_id = ? AND business_connection_id_hash = ?", message.ChatID, message.MessageID, message.BusinessConnectionIDHash).
		Select()
	if e.IsNonNil(eraw) {
		return nil,e.FromError(eraw, "failed to select deleted message")
	}

	hashe.Add("db_message", message)

	return message, e.Nil()
}

func getBotUser(businessConnectionID string) (*models.Telegramuser, *e.ErrorInfo) {
	user := &models.Telegramuser{
		BusinessConnectionIDHash: utils.ToHash(businessConnectionID),
	}

	db := postgresql.GetDB()
	err := db.Model(user).
		Where("business_connection_id = ?", utils.ToHash(businessConnectionID)).
		Select()
	if e.IsNonNil(err) {
		return nil, e.FromError(err, "failed to select bot user")
	}

	return user, e.Nil()
}

func sendNotification(message *models.Message, hashe *h.HandlerChainHashe) *e.ErrorInfo  {
	botUser, err := getBotUser(message.BusinessConnectionIDHash)
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
			Entities: metadata.Entities,
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
