package deletedMessage

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"slices"
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

func NewDeletedMessageEndpoint() h.Endpoint {
	ep := h.Endpoint{}
	ep.Init(
		"deleted_message",
		*h.HandlerChain{}.Init(
			1*time.Minute,
			h.InitChainHandler(run, h.EndOnError),
		),
		h.And(h.BusinessEvent(h.BusEventTypeDeleted), filters.ActorIsNotSelf{}),
	)

	return ep
}

func sendNotification(message *models.Message, hashe *h.HandlerChainHashe, ignoredMessagesIDs []int) (*e.ErrorInfo, []int) {
	if slices.Contains(ignoredMessagesIDs, message.MessageID) {
		return e.Nil(), ignoredMessagesIDs
	}

	botUser, err := shared.GetBotUser(message.BusinessConnectionIDHash)
	if e.IsNonNil(err) {
		return err, ignoredMessagesIDs
	}

	botUserID, err := botUser.GetTgId()
	if e.IsNonNil(err) {
		return err, ignoredMessagesIDs
	}

	key, err := utils.DecryptUserKey(botUser.DataEncryptionKey)
	if e.IsNonNil(err) {
		return e.FromError(err, "failed to decrypt user key"), ignoredMessagesIDs
	}

	decryptedMetadataJson, err := utils.Decrypt(message.Metadata, key)
	if e.IsNonNil(err) {
		return e.FromError(err, "failed to decrypt message metadata"), ignoredMessagesIDs
	}

	var metadata = &tele.Message{}
	eraw := json.Unmarshal(decryptedMetadataJson, metadata)
	if e.IsNonNil(eraw) {
		return e.FromError(eraw, "failed to unmarshal message metadata"), ignoredMessagesIDs
	}

	if message.MediaGroupIDHash != "" {
		db := postgresql.GetDB()

		var allMediagroupMessages []*models.Message
		eraw = db.Model(&allMediagroupMessages).
			Where("media_group_id_hash = ?", message.MediaGroupIDHash).
			Order("created_at ASC").
			Select()
		if e.IsNonNil(eraw) {
			return e.FromError(eraw, "failed to get all media group messages"), ignoredMessagesIDs
		}

		var unmarshalMessageErr *e.ErrorInfo
		var allMediaGroupMessagesApi []*tele.Message

		for _, raw := range allMediagroupMessages {
			ignoredMessagesIDs = append(ignoredMessagesIDs, raw.MessageID)

			var messageApi = &tele.Message{}

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
			return e.FromError(unmarshalMessageErr, "sendNotification").WithSeverity(e.Warning), ignoredMessagesIDs
		}

		mediaGroup, ok := telegram.BuildMediaGroup(allMediaGroupMessagesApi)
		if !ok {
			return e.FromError(err, "failed to build media group"), ignoredMessagesIDs
		}

		mediaGroup.Chat = &tele.Chat{
			ID: botUserID,
		}

		sentMediaGroup, err := hashe.EmitAlbumWait(context.Background(), "telegram.message.send", mediaGroup)
		if e.IsNonNil(err) {
			return err, ignoredMessagesIDs
		}
		if len(sentMediaGroup) == 0 {
			return e.NewError("empty sent album", "deleted media group was sent without resulting messages").WithSeverity(e.Warning), ignoredMessagesIDs
		}

		username := strings.TrimSpace(metadata.Chat.FirstName + " " + metadata.Chat.LastName)
		summary, summarySendOpts := telegram.BuildMessageSummary(metadata)
		prefix := fmt.Sprintf("Пользователь %s удалил медиагруппу!\n", username)
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

		return hashe.Emit("telegram.message.send", toSend), ignoredMessagesIDs
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
			Type:   tele.EntityTextLink,
			Offset: 13,
			Length: usernameLen,
			URL:    fmt.Sprintf("tg://user?id=%d", metadata.Sender.ID),
		}, tele.MessageEntity{
			Type:   tele.EntityEBlockquote,
			Offset: prefixLen,
			Length: originalTextLen,
		})
		metadata.Chat.ID = botUserID

		sentMessage, err := hashe.EmitWait(context.Background(), "telegram.message.send", metadata)
		if e.IsNonNil(err) {
			return err, ignoredMessagesIDs
		}

		summary, sendOpts := telegram.BuildMessageSummary(metadata)
		toSend := &tele.Message{
			Chat: &tele.Chat{
				ID: botUserID,
			},
			Text:    summary,
			ReplyTo: sentMessage,
		}
		toSend = telegram.HideSendOptsIntoMessage(toSend, sendOpts)

		err = hashe.Emit("telegram.message.send", toSend)

		// TODO: Если не включено расширенное сохранение сообщений, удалять из бд

		return err, ignoredMessagesIDs
	}

	metadata.Chat.ID = botUserID
	sentMessage, err := hashe.EmitWait(context.Background(), "telegram.message.send", metadata)

	if e.IsNonNil(err) {
		return err, ignoredMessagesIDs
	}

	username := strings.TrimSpace(metadata.Chat.FirstName + " " + metadata.Chat.LastName)
	summary, summarySendOpts := telegram.BuildMessageSummary(metadata)
	prefix := fmt.Sprintf("Пользователь %s удалил сообщение!\n", username)
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

	return err, ignoredMessagesIDs
}

func run(update tele.Update, hashe *h.HandlerChainHashe) *e.ErrorInfo {
	var ignoredMessagesIDs []int

	for _, deletedMessage := range update.DeletedBusinessMessages.MessageIDs {
		message, err := shared.GetMessageInfo(deletedMessage, update.DeletedBusinessMessages.BusinessConnectionID, update.DeletedBusinessMessages.Chat.ID)
		if e.IsNonNil(err) {
			return err
		}

		err, ignoredMessagesIDs = sendNotification(message, hashe, ignoredMessagesIDs)
		log.Println("ignoredMessagesIDs", ignoredMessagesIDs)
		if e.IsNonNil(err) {
			return err
		}
	}

	return e.Nil()
}
