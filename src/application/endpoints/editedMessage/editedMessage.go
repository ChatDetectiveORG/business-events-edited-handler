package editedMessage

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

	shared "github.com/ChatDetectiveORG/business-events-edited-handler/src/application/endpoints"
	"github.com/ChatDetectiveORG/business-events-edited-handler/src/application/filters"
	"github.com/ChatDetectiveORG/business-events-edited-handler/src/infrastructure/postgresql"
)

func NewEditedMessageEndpoint() h.Endpoint {
	ep := h.Endpoint{}
	ep.Init(
		"edited_message",
		*h.HandlerChain{}.Init(
			1*time.Minute,
			h.InitChainHandler(run, h.EndOnError),
		),
		h.And(h.BusinessEvent(h.BusEventTypeEdited), filters.ActorIsNotSelf{}),
	)

	return ep
}

func sendNotification(message *models.Message, newMessage *tele.Message, hashe *h.HandlerChainHashe) *e.ErrorInfo {
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

	if message.MediaGroupIDHash != "" {
		db := postgresql.GetDB()

		var allMediagroupMessages []*models.Message
		eraw = db.Model(&allMediagroupMessages).
			Where("media_group_id_hash = ?", message.MediaGroupIDHash).
			Order("created_at ASC").
			Select()
		if e.IsNonNil(eraw) {
			return e.FromError(eraw, "failed to get all media group messages")
		}

		var unmarshalMessageErr *e.ErrorInfo
		var allMediaGroupMessagesApi []*tele.Message

		var editedMessagePosition int

		for i, raw := range allMediagroupMessages {
			var messageApi = &tele.Message{}

			if raw.MessageID == message.MessageID {
				editedMessagePosition = i
			}

			decryptedJson, err := utils.Decrypt(raw.Metadata, key)
			if e.IsNonNil(err) {
				unmarshalMessageErr = err
			}

			eraw = json.Unmarshal(decryptedJson, messageApi)
			if e.IsNonNil(eraw) {
				unmarshalMessageErr = e.FromError(eraw, "failed to unmarshal message metadata")
			}

			allMediaGroupMessagesApi = append(allMediaGroupMessagesApi, messageApi)
		}

		if len(allMediaGroupMessagesApi) == 0 {
			return e.FromError(unmarshalMessageErr, "sendNotification").WithSeverity(e.Warning)
		}

		mediaGroup, ok := telegram.BuildMediaGroup(allMediaGroupMessagesApi)
		if !ok {
			return e.FromError(err, "failed to build media group")
		}

		mediaGroup.Chat = &tele.Chat{
			ID: botUserID,
		}

		sentMediaGroup, err := hashe.EmitAlbumWait(context.Background(), "telegram.message.send", mediaGroup)
		if e.IsNonNil(err) {
			return err
		}
		if len(sentMediaGroup) == 0 {
			return e.NewError("empty sent album", "deleted media group was sent without resulting messages").WithSeverity(e.Warning)
		}

		username := strings.TrimSpace(metadata.Chat.FirstName + " " + metadata.Chat.LastName)
		summary, summarySendOpts := telegram.BuildMessageSummary(metadata)
		prefix := fmt.Sprintf("Пользователь %s изменил медиагруппу!\nСтарая версия:\n", username)
		usernameLen := utils.TgLen(username)
		prefixLen := utils.TgLen(prefix)

		for i := 0; i < len(summarySendOpts.Entities); i++ {
			summarySendOpts.Entities[i].Offset += prefixLen
		}

		toSend := &tele.Message{
			Chat: &tele.Chat{
				ID: botUserID,
			},
			Text:    prefix + summary,
			ReplyTo: sentMediaGroup[0],
			Entities: tele.Entities{tele.MessageEntity{
				Type:   tele.EntityTextLink,
				Offset: 13,
				Length: usernameLen,
				URL:    fmt.Sprintf("tg://user?id=%d", metadata.Sender.ID),
			}},
		}
		toSend = telegram.HideSendOptsIntoMessage(toSend, summarySendOpts)
		err = hashe.Emit("telegram.message.send", toSend)
		if e.IsNonNil(err) {
			return err
		}

		allMediaGroupMessagesApi[editedMessagePosition] = newMessage
		mediaGroup, ok = telegram.BuildMediaGroup(allMediaGroupMessagesApi)
		if !ok {
			return e.FromError(err, "failed to build media group")
		}
		mediaGroup.Chat = &tele.Chat{
			ID: botUserID,
		}
		sentMediaGroup, err = hashe.EmitAlbumWait(context.Background(), "telegram.message.send", mediaGroup)
		if e.IsNonNil(err) {
			return err
		}
		if len(sentMediaGroup) == 0 {
			return e.NewError("empty sent album", "edited media group was sent without resulting messages").WithSeverity(e.Warning)
		}

		summary, summarySendOpts = telegram.BuildMessageSummary(metadata)
		prefix = fmt.Sprintf("Пользователь %s изменил медиагруппу!\nНовая версия:\n", username)
		prefixLen = utils.TgLen(prefix)

		for i := 0; i < len(summarySendOpts.Entities); i++ {
			summarySendOpts.Entities[i].Offset += prefixLen
		}

		toSend = &tele.Message{
			Chat: &tele.Chat{
				ID: botUserID,
			},
			Text:    prefix + summary,
			ReplyTo: sentMediaGroup[0],
			Entities: tele.Entities{tele.MessageEntity{
				Type:   tele.EntityTextLink,
				Offset: 13,
				Length: usernameLen,
				URL:    fmt.Sprintf("tg://user?id=%d", metadata.Sender.ID),
			}},
		}
		toSend = telegram.HideSendOptsIntoMessage(toSend, summarySendOpts)
		err = hashe.Emit("telegram.message.send", toSend)

		return e.Nil()
	}

	if metadata.Text != "" && len(metadata.Text)+len(newMessage.Text) <= 3900 && newMessage.Text != "" {
		username := strings.TrimSpace(metadata.Chat.FirstName + " " + metadata.Chat.LastName)

		prefix := fmt.Sprintf("Пользователь %s изменил сообщение!\nСтарая версия:\n", username)
		originalTextLen := utils.TgLen(metadata.Text)
		prefixLen := utils.TgLen(prefix)
		usernameLen := utils.TgLen(username)

		for i := 0; i < len(metadata.Entities); i++ {
			metadata.Entities[i].Offset += prefixLen
		}

		postfix := "Новая версия:\n"
		postfixLen := utils.TgLen(postfix)
		newVersionTextLen := utils.TgLen(newMessage.Text)

		for _, entity := range newMessage.Entities {
			entity.Offset += prefixLen + originalTextLen + postfixLen
			metadata.Entities = append(metadata.Entities, entity)
		}

		metadata.Text = prefix + metadata.Text + postfix + newMessage.Text
		metadata.Entities = append(metadata.Entities, tele.MessageEntity{
			Type:   tele.EntityTextLink,
			Offset: 13,
			Length: usernameLen,
			URL:    fmt.Sprintf("tg://user?id=%d", metadata.Sender.ID),
		}, tele.MessageEntity{
			Type:   tele.EntityEBlockquote,
			Offset: prefixLen,
			Length: originalTextLen,
		}, tele.MessageEntity{
			Type:   tele.EntityEBlockquote,
			Offset: prefixLen + originalTextLen + postfixLen,
			Length: newVersionTextLen,
		})
		metadata.Chat.ID = botUserID

		sentMessage, err := hashe.EmitWait(context.Background(), "telegram.message.send", metadata)
		if e.IsNonNil(err) {
			return err
		}

		summary, summarySendOpts := telegram.BuildMessageSummary(metadata)
		toSend := &tele.Message{
			Chat: &tele.Chat{
				ID: botUserID,
			},
			Text:    summary,
			ReplyTo: sentMessage,
		}

		toSend = telegram.HideSendOptsIntoMessage(toSend, summarySendOpts)

		err = hashe.Emit("telegram.message.send", toSend)

		return err
	}

	metadata.Chat.ID = botUserID
	sentMessage, err := hashe.EmitWait(context.Background(), "telegram.message.send", metadata)

	if e.IsNonNil(err) {
		return err
	}

	username := strings.TrimSpace(metadata.Chat.FirstName + " " + metadata.Chat.LastName)
	usernameLen := utils.TgLen(username)

	err = hashe.Emit("telegram.message.send", &tele.Message{
		Chat: &tele.Chat{
			ID: botUserID,
		},
		Text:    fmt.Sprintf("Пользователь %s изменил сообщение!\nСтарая версия:", username),
		ReplyTo: sentMessage,
		Entities: tele.Entities{tele.MessageEntity{
			Type:   tele.EntityTextLink,
			Offset: 13,
			Length: usernameLen,
			URL:    fmt.Sprintf("tg://user?id=%d", metadata.Sender.ID),
		}},
	})

	newMessage.Chat.ID = botUserID
	sentMessage, err = hashe.EmitWait(context.Background(), "telegram.message.send", newMessage)

	summary, summarySendOpts := telegram.BuildMessageSummary(metadata)
	prefix := fmt.Sprintf("Пользователь %s изменил сообщение!\nНовая версия:", username)
	prefixLen := utils.TgLen(prefix)

	for i := 0; i < len(summarySendOpts.Entities); i++ {
		summarySendOpts.Entities[i].Offset += prefixLen
	}

	toSend := &tele.Message{
		Chat: &tele.Chat{
			ID: botUserID,
		},
		Text:    prefix + summary,
		ReplyTo: sentMessage,
		Entities: tele.Entities{tele.MessageEntity{
			Type:   tele.EntityTextLink,
			Offset: 13,
			Length: usernameLen,
			URL:    fmt.Sprintf("tg://user?id=%d", metadata.Sender.ID),
		}},
	}

	toSend = telegram.HideSendOptsIntoMessage(toSend, summarySendOpts)

	err = hashe.Emit("telegram.message.send", toSend)

	return err
}

func updateMessageInDatabase(message *models.Message, newMessage *tele.Message) *e.ErrorInfo {
	db := postgresql.GetDB()

	user, err := shared.GetBotUser(message.BusinessConnectionIDHash)
	if e.IsNonNil(err) {
		return err
	}

	key, err := utils.DecryptUserKey(user.DataEncryptionKey)
	if e.IsNonNil(err) {
		return e.FromError(err, "failed to decrypt user key")
	}

	metadataJson, eraw := json.Marshal(newMessage)
	if e.IsNonNil(eraw) {
		return e.FromError(eraw, "failed to encrypt message text")
	}

	encryptedMetadataJson, err := utils.Encrypt(metadataJson, key)
	if e.IsNonNil(err) {
		return e.FromError(err, "failed to encrypt message metadata")
	}

	message.Metadata = encryptedMetadataJson
	_, eraw = db.Model(message).WherePK().Column("metadata").Update()
	if e.IsNonNil(eraw) {
		return e.FromError(eraw, "failed to update message in database")
	}

	// TODO: Если включено расширенное сохранение сообщений, вносить изменения в бд
	return e.Nil()
}

func run(update tele.Update, hashe *h.HandlerChainHashe) *e.ErrorInfo {
	message, err := shared.GetMessageInfo(update.EditedBusinessMessage.ID, update.EditedBusinessMessage.BusinessConnectionID, update.EditedBusinessMessage.Chat.ID)
	if e.IsNonNil(err) {
		return err
	}

	err = sendNotification(message, update.EditedBusinessMessage, hashe)
	if e.IsNonNil(err) {
		return err
	}

	err = updateMessageInDatabase(message, update.EditedBusinessMessage)
	if e.IsNonNil(err) {
		return err
	}

	return e.Nil()
}
