package editedMessage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	e "github.com/ChatDetectiveORG/shared/errors"
	h "github.com/ChatDetectiveORG/shared/handlers"
	models "github.com/ChatDetectiveORG/shared/postgresModels"
	"github.com/ChatDetectiveORG/shared/telegram"
	utils "github.com/ChatDetectiveORG/shared/utils"
	tele "gopkg.in/telebot.v4"

	shared "github.com/ChatDetectiveORG/business-events-edited-handler/src/application/endpoints"
	"github.com/ChatDetectiveORG/business-events-edited-handler/src/infrastructure/postgresql"
)

type EditedInput struct {
	OldVersion *tele.Message
	NewVersion *tele.Message
	MessageModel *models.Message

	Key []byte

	ReciverID int64

	ActorName  string
	ActorID    int64
}

func SendOldAlbumnVersion(input *EditedInput, allMediaGroupMessagesApi []*tele.Message, hashe *h.HandlerChainHashe) *e.ErrorInfo {
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

	summary, summarySendOpts := telegram.BuildMessageSummary(input.OldVersion)
	prefix := fmt.Sprintf("Пользователь %s изменил медиагруппу!\nСтарая версия:\n", input.ActorName)
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
	err = hashe.Emit("telegram.message.send", toSend)
	if e.IsNonNil(err) {
		return err
	}

	return e.Nil()
}

func SendNewAlbumnVersion(input *EditedInput, allMediaGroupMessagesApi[]*tele.Message, editedMessagePosition int, hashe *h.HandlerChainHashe) *e.ErrorInfo {
	
	allMediaGroupMessagesApi[editedMessagePosition] = input.NewVersion
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
		return e.NewError("empty sent album", "edited media group was sent without resulting messages").WithSeverity(e.Warning)
	}

	summary, summarySendOpts := telegram.BuildMessageSummary(input.NewVersion)
	prefix := fmt.Sprintf("Пользователь %s изменил медиагруппу!\nНовая версия:\n", input.ActorName)
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
	err = hashe.Emit("telegram.message.send", toSend)

	return e.Nil()
}

func handleMediagroup(input *EditedInput, hashe *h.HandlerChainHashe) *e.ErrorInfo {
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

	var editedMessagePosition int

	for i, raw := range allMediagroupMessages {
		var messageApi = &tele.Message{}

		if raw.MessageID == input.MessageModel.MessageID {
			editedMessagePosition = i
		}

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

	err := SendOldAlbumnVersion(input, allMediaGroupMessagesApi, hashe)
	if e.IsNonNil(err) {
		return err
	}

	err = SendNewAlbumnVersion(input, allMediaGroupMessagesApi, editedMessagePosition, hashe)

	return err
}

func handleTextMessage(input *EditedInput, hashe *h.HandlerChainHashe) *e.ErrorInfo {
	prefix := fmt.Sprintf("Пользователь %s изменил сообщение!\nСтарая версия:\n", input.ActorName)
	originalTextLen := utils.TgLen(input.OldVersion.Text)
	prefixLen := utils.TgLen(prefix)
	usernameLen := utils.TgLen(input.ActorName)

	// Compute diff before metadata.Text is modified so both originals are available.
	var oldDiffEntities, newDiffEntities []tele.MessageEntity
	if highlightTextDiff {
		oldDiffEntities, newDiffEntities = computeDiffBoldEntities(input.OldVersion.Text, input.NewVersion.Text)
	}

	for i := 0; i < len(input.OldVersion.Entities); i++ {
		input.OldVersion.Entities[i].Offset += prefixLen
	}

	postfix := "Новая версия:\n"
	postfixLen := utils.TgLen(postfix)
	newVersionTextLen := utils.TgLen(input.NewVersion.Text)

	for _, entity := range input.NewVersion.Entities {
		entity.Offset += prefixLen + originalTextLen + postfixLen
		input.OldVersion.Entities = append(input.OldVersion.Entities, entity)
	}

	if highlightTextDiff {
		for _, ent := range oldDiffEntities {
			ent.Offset += prefixLen
			input.OldVersion.Entities = append(input.OldVersion.Entities, ent)
		}
		for _, ent := range newDiffEntities {
			ent.Offset += prefixLen + originalTextLen + postfixLen
			input.OldVersion.Entities = append(input.OldVersion.Entities, ent)
		}
	}

	input.OldVersion.Text = prefix + input.OldVersion.Text + postfix + input.NewVersion.Text
	input.OldVersion.Entities = append(input.OldVersion.Entities, tele.MessageEntity{
		Type:   tele.EntityTextLink,
		Offset: 13,
		Length: usernameLen,
		URL:    fmt.Sprintf("tg://user?id=%d", input.ActorID),
	}, tele.MessageEntity{
		Type:   tele.EntityEBlockquote,
		Offset: prefixLen,
		Length: originalTextLen,
	}, tele.MessageEntity{
		Type:   tele.EntityEBlockquote,
		Offset: prefixLen + originalTextLen + postfixLen,
		Length: newVersionTextLen,
	})
	input.OldVersion.Chat.ID = input.ReciverID

	sentMessage, err := hashe.EmitWait(context.Background(), "telegram.message.send", input.OldVersion)
	if e.IsNonNil(err) {
		return err
	}

	summary, summarySendOpts := telegram.BuildMessageSummary(input.OldVersion)
	if summary != "" {
		toSend := &tele.Message{
			Chat: &tele.Chat{
				ID: input.ReciverID,
			},
			Text:    summary,
			ReplyTo: sentMessage,
		}

		toSend = telegram.HideSendOptsIntoMessage(toSend, summarySendOpts)

		err = hashe.Emit("telegram.message.send", toSend)
	}

	return err
}

func handleOtherMedia(input *EditedInput, hashe *h.HandlerChainHashe) *e.ErrorInfo {
	// Apply diff highlighting to the fallback case (separate old/new messages).
	if highlightTextDiff && input.OldVersion.Text != "" && input.NewVersion.Text != "" {
		oldDiffEntities, newDiffEntities := computeDiffBoldEntities(input.OldVersion.Text, input.NewVersion.Text)
		input.OldVersion.Entities = append(input.OldVersion.Entities, oldDiffEntities...)
		input.NewVersion.Entities = append(input.NewVersion.Entities, newDiffEntities...)
	}

	input.OldVersion.Chat.ID = input.ReciverID
	sentMessage, err := hashe.EmitWait(context.Background(), "telegram.message.send", input.OldVersion)

	if e.IsNonNil(err) {
		return err
	}

	username := strings.TrimSpace(input.OldVersion.Chat.FirstName + " " + input.OldVersion.Chat.LastName)
	usernameLen := utils.TgLen(username)

	err = hashe.Emit("telegram.message.send", &tele.Message{
		Chat: &tele.Chat{
			ID: input.ReciverID,
		},
		Text:    fmt.Sprintf("Пользователь %s изменил сообщение!\nСтарая версия:", username),
		ReplyTo: sentMessage,
		Entities: tele.Entities{tele.MessageEntity{
			Type:   tele.EntityTextLink,
			Offset: 13,
			Length: usernameLen,
			URL:    fmt.Sprintf("tg://user?id=%d", input.ActorID),
		}},
	})

	input.NewVersion.Chat.ID = input.ReciverID
	sentMessage, err = hashe.EmitWait(context.Background(), "telegram.message.send", input.NewVersion)

	summary, summarySendOpts := telegram.BuildMessageSummary(input.NewVersion)
	prefix := fmt.Sprintf("Пользователь %s изменил сообщение!\nНовая версия:", username)
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

func sendNotification(input *EditedInput, hashe *h.HandlerChainHashe) *e.ErrorInfo {
	if input.MessageModel.MediaGroupIDHash != "" {
		return handleMediagroup(input, hashe)
	}

	if input.OldVersion.Text != "" && len(input.OldVersion.Text)+len(input.NewVersion.Text) <= 3900 && input.NewVersion.Text != "" {
		return handleTextMessage(input, hashe)
	}

	return handleOtherMedia(input, hashe)
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
