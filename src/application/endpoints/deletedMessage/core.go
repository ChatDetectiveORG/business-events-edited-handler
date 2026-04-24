package deletedMessage

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	e "github.com/ChatDetectiveORG/shared/errors"
	h "github.com/ChatDetectiveORG/shared/handlers"
	models "github.com/ChatDetectiveORG/shared/postgresModels"
	"github.com/ChatDetectiveORG/shared/telegram"
	utils "github.com/ChatDetectiveORG/shared/utils"
	tele "gopkg.in/telebot.v4"

	"github.com/ChatDetectiveORG/business-events-edited-handler/src/infrastructure/postgresql"
)

type DeletedInput struct {
	TeleMessage *tele.Message
	MessageModel *models.Message

	ignoredMessagesIDs []int

	Key []byte

	ReciverID int64

	ActorName  string
	ActorID    int64
}

func handleMediaGroup(input *DeletedInput, hashe *h.HandlerChainHashe) *e.ErrorInfo {
	db := postgresql.GetDB()

	var allMediagroupMessages []*models.Message
	eRaw := db.Model(&allMediagroupMessages).
		Where("media_group_id_hash = ?", input.MessageModel.MediaGroupIDHash).
		Order("created_at ASC").
		Select()
	if e.IsNonNil(eRaw) {
		return e.FromError(eRaw, "failed to get all media group messages")
	}

	var unmarshalMessageErr *e.ErrorInfo
	var allMediaGroupMessagesApi []*tele.Message

	for _, raw := range allMediagroupMessages {
		input.ignoredMessagesIDs = append(input.ignoredMessagesIDs, raw.MessageID)

		var messageApi = &tele.Message{}

		decryptedJson, err := utils.Decrypt(raw.Metadata, input.Key)
		if e.IsNonNil(err) {
			unmarshalMessageErr = err
		}

		eRaw = json.Unmarshal(decryptedJson, messageApi)
		if e.IsNonNil(eRaw) {
			unmarshalMessageErr = e.FromError(eRaw, "failed to unmarshal message metadata")
		}

		allMediaGroupMessagesApi = append(allMediaGroupMessagesApi, messageApi)
	}

	if len(allMediaGroupMessagesApi) == 0 {
		return e.FromError(unmarshalMessageErr, "sendNotification").WithSeverity(e.Warning)
	}

	mediaGroup, ok := telegram.BuildMediaGroup(allMediaGroupMessagesApi)
	if !ok {
		return e.NewError("failed to build media group", "failed to build media group")
	}

	mediaGroup.Chat = &tele.Chat{
		ID: input.ReciverID,
	}

	sentMediaGroup, err := hashe.EmitAlbumWait(context.Background(), "telegram.message.send", mediaGroup)
	if e.IsNonNil(err) {
		return err
	}
	if len(sentMediaGroup) == 0 {
		return e.NewError("empty sent album", "deleted media group was sent without resulting messages").WithSeverity(e.Warning)
	}

	summary, summarySendOpts := telegram.BuildMessageSummary(input.TeleMessage)
	prefix := fmt.Sprintf("Пользователь %s удалил медиагруппу!\n", input.ActorName)
	usernameLen := utils.TgLen(input.ActorName)
	prefixLen := utils.TgLen(prefix)

	for i := 0; i < len(summarySendOpts.Entities); i++ {
		summarySendOpts.Entities[i].Offset += prefixLen
	}

	toSend := &tele.Message{
		Chat: &tele.Chat{
			ID: input.ReciverID,
		},
		Text:    prefix + summary,
		ReplyTo: sentMediaGroup[0],
		Entities: tele.Entities{tele.MessageEntity{
			Type:   tele.EntityTextLink,
			Offset: 13,
			Length: usernameLen,
			URL:    fmt.Sprintf("tg://user?id=%d", input.ActorID),
		}},
	}
	toSend = telegram.HideSendOptsIntoMessage(toSend, summarySendOpts)

	return hashe.Emit("telegram.message.send", toSend)
}

func handleTextMessage(input *DeletedInput, hashe *h.HandlerChainHashe) *e.ErrorInfo {
	prefix := fmt.Sprintf("Пользователь %s удалил сообщение!\n", input.ActorName)
	originalTextLen := utils.TgLen(input.TeleMessage.Text)
	prefixLen := utils.TgLen(prefix)
	usernameLen := utils.TgLen(input.ActorName)

	for i := 0; i < len(input.TeleMessage.Entities); i++ {
		input.TeleMessage.Entities[i].Offset += prefixLen
	}

	input.TeleMessage.Text = prefix + input.TeleMessage.Text
	input.TeleMessage.Entities = append(input.TeleMessage.Entities, tele.MessageEntity{
		Type:   tele.EntityTextLink,
		Offset: 13,
		Length: usernameLen,
		URL:    fmt.Sprintf("tg://user?id=%d", input.ActorID),
	}, tele.MessageEntity{
		Type:   tele.EntityEBlockquote,
		Offset: prefixLen,
		Length: originalTextLen,
	})
	input.TeleMessage.Chat.ID = input.ReciverID

	sentMessage, err := hashe.EmitWait(context.Background(), "telegram.message.send", input.TeleMessage)
	if e.IsNonNil(err) {
		return err
	}

	summary, _ := telegram.BuildMessageSummary(input.TeleMessage)
	if summary != "" {
		toSend := &tele.Message{
			Chat: &tele.Chat{
				ID: input.ReciverID,
			},
			Text:    summary,
			ReplyTo: sentMessage,
		}
		// toSend = telegram.HideSendOptsIntoMessage(toSend, sendOpts)
	
		err = hashe.Emit("telegram.message.send", toSend)
	}
	// TODO: Если не включено расширенное сохранение сообщений, удалять из бд

	return err
}

func handleOtherMessage(input *DeletedInput, hashe *h.HandlerChainHashe) *e.ErrorInfo {
	input.TeleMessage.Chat.ID = input.ReciverID
	sentMessage, err := hashe.EmitWait(context.Background(), "telegram.message.send", input.TeleMessage)

	if e.IsNonNil(err) {
		return err
	}

	summary, summarySendOpts := telegram.BuildMessageSummary(input.TeleMessage)
	prefix := fmt.Sprintf("Пользователь %s удалил сообщение!\n", input.ActorName)
	usernameLen := utils.TgLen(input.ActorName)
	prefixLen := utils.TgLen(prefix)

	for i := 0; i < len(summarySendOpts.Entities); i++ {
		summarySendOpts.Entities[i].Offset += prefixLen
	}

	toSend := &tele.Message{
		Chat: &tele.Chat{
			ID: input.ReciverID,
		},
		Text:    prefix + summary,
		ReplyTo: sentMessage,
		Entities: tele.Entities{tele.MessageEntity{
			Type:   tele.EntityTextLink,
			Offset: 13,
			Length: usernameLen,
			URL:    fmt.Sprintf("tg://user?id=%d", input.ActorID),
		}},
	}

	toSend = telegram.HideSendOptsIntoMessage(toSend, summarySendOpts)

	err = hashe.Emit("telegram.message.send", toSend)

	return err
}

func sendNotification(input *DeletedInput, hashe *h.HandlerChainHashe) *e.ErrorInfo {
	if slices.Contains(input.ignoredMessagesIDs, input.MessageModel.MessageID) {
		return e.Nil()
	}

	if input.MessageModel.MediaGroupIDHash != "" {
		return handleMediaGroup(input, hashe)
	}

	if input.TeleMessage.Text != "" && len(input.TeleMessage.Text) <= 3900 {
		return handleTextMessage(input, hashe)
	}

	return handleOtherMessage(input, hashe)
}
