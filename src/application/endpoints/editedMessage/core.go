package editedMessage

import (
	"context"
	"encoding/json"
	"fmt"

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
	OldVersion   *tele.Message
	NewVersion   *tele.Message
	MessageModel *models.Message

	Key []byte

	ReciverID int64

	ActorName string
	ActorID   int64
}

func actorLinkEntity(actorName string, actorID int64) tele.MessageEntity {
	return tele.MessageEntity{
		Type:   tele.EntityTextLink,
		Offset: 13,
		Length: utils.TgLen(actorName),
		URL:    fmt.Sprintf("tg://user?id=%d", actorID),
	}
}

func sendOldAlbumVersion(input *EditedInput, allMediaGroupMessagesApi []*tele.Message, hashe *h.HandlerChainHashe) *e.ErrorInfo {
	mediaGroup, ok := telegram.BuildMediaGroup(allMediaGroupMessagesApi)
	if !ok {
		return e.NewError("failed to build media group", "failed to build media group")
	}

	mediaGroup.Chat = &tele.Chat{ID: input.ReciverID}

	sentMediaGroup, err := hashe.EmitAlbumWait(context.Background(), "telegram.message.send", mediaGroup)
	if e.IsNonNil(err) {
		return err
	}
	if len(sentMediaGroup) == 0 {
		return e.NewError("empty sent album", "old media group was sent without resulting messages").WithSeverity(e.Warning)
	}

	summary, summarySendOpts := telegram.BuildMessageSummary(input.OldVersion)
	prefix := fmt.Sprintf("Пользователь %s изменил медиагруппу!\nСтарая версия:\n", input.ActorName)
	usernameLen := utils.TgLen(input.ActorName)
	prefixLen := utils.TgLen(prefix)

	for i := 0; i < len(summarySendOpts.Entities); i++ {
		summarySendOpts.Entities[i].Offset += prefixLen
	}

	toSend := &tele.Message{
		Chat: &tele.Chat{ID: input.ReciverID},
		Text: prefix + summary,
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

func sendNewAlbumVersion(input *EditedInput, allMediaGroupMessagesApi []*tele.Message, editedMessagePosition int, hashe *h.HandlerChainHashe) *e.ErrorInfo {
	allMediaGroupMessagesApi[editedMessagePosition] = input.NewVersion

	mediaGroup, ok := telegram.BuildMediaGroup(allMediaGroupMessagesApi)
	if !ok {
		return e.NewError("failed to build media group", "failed to build media group")
	}

	mediaGroup.Chat = &tele.Chat{ID: input.ReciverID}

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
		Chat: &tele.Chat{ID: input.ReciverID},
		Text: prefix + summary,
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
		messageApi := &tele.Message{}

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

	err := sendOldAlbumVersion(input, allMediaGroupMessagesApi, hashe)
	if e.IsNonNil(err) {
		return err
	}

	return sendNewAlbumVersion(input, allMediaGroupMessagesApi, editedMessagePosition, hashe)
}

func handleTextMessage(input *EditedInput, hashe *h.HandlerChainHashe) *e.ErrorInfo {
	prefix := fmt.Sprintf("Пользователь %s изменил сообщение!\nСтарая версия:\n", input.ActorName)
	originalTextLen := utils.TgLen(input.OldVersion.Text)
	prefixLen := utils.TgLen(prefix)
	usernameLen := utils.TgLen(input.ActorName)

	var oldDiffEntities, newDiffEntities []tele.MessageEntity
	if highlightTextDiff {
		oldDiffEntities, newDiffEntities = computeDiffBoldEntities(input.OldVersion.Text, input.NewVersion.Text)
	}

	combined := *input.OldVersion

	for i := 0; i < len(combined.Entities); i++ {
		combined.Entities[i].Offset += prefixLen
	}

	postfix := "Новая версия:\n"
	postfixLen := utils.TgLen(postfix)
	newVersionTextLen := utils.TgLen(input.NewVersion.Text)

	for _, entity := range input.NewVersion.Entities {
		entity.Offset += prefixLen + originalTextLen + postfixLen
		combined.Entities = append(combined.Entities, entity)
	}

	if highlightTextDiff {
		for _, ent := range oldDiffEntities {
			ent.Offset += prefixLen
			combined.Entities = append(combined.Entities, ent)
		}
		for _, ent := range newDiffEntities {
			ent.Offset += prefixLen + originalTextLen + postfixLen
			combined.Entities = append(combined.Entities, ent)
		}
	}

	combined.Text = prefix + input.OldVersion.Text + postfix + input.NewVersion.Text
	combined.Entities = append(combined.Entities, tele.MessageEntity{
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
	combined.Chat = &tele.Chat{ID: input.ReciverID}

	sentMessage, err := hashe.EmitWait(context.Background(), "telegram.message.send", &combined)
	if e.IsNonNil(err) {
		return err
	}

	summary, summarySendOpts := telegram.BuildMessageSummary(input.NewVersion)
	if summary == "" {
		return e.Nil()
	}

	toSend := &tele.Message{
		Chat:    &tele.Chat{ID: input.ReciverID},
		Text:    summary,
		ReplyTo: sentMessage,
	}
	toSend = telegram.HideSendOptsIntoMessage(toSend, summarySendOpts)
	return hashe.Emit("telegram.message.send", toSend)
}

func handleOtherMedia(input *EditedInput, hashe *h.HandlerChainHashe) *e.ErrorInfo {
	if highlightTextDiff && input.OldVersion.Text != "" && input.NewVersion.Text != "" {
		oldDiffEntities, newDiffEntities := computeDiffBoldEntities(input.OldVersion.Text, input.NewVersion.Text)
		input.OldVersion.Entities = append(input.OldVersion.Entities, oldDiffEntities...)
		input.NewVersion.Entities = append(input.NewVersion.Entities, newDiffEntities...)
	}

	oldVersion := *input.OldVersion
	oldVersion.Chat = &tele.Chat{ID: input.ReciverID}

	sentOld, err := hashe.EmitWait(context.Background(), "telegram.message.send", &oldVersion)
	if e.IsNonNil(err) {
		return err
	}

	usernameLen := utils.TgLen(input.ActorName)
	err = hashe.Emit("telegram.message.send", &tele.Message{
		Chat: &tele.Chat{ID: input.ReciverID},
		Text: fmt.Sprintf("Пользователь %s изменил сообщение!\nСтарая версия:", input.ActorName),
		ReplyTo: sentOld,
		Entities: tele.Entities{actorLinkEntity(input.ActorName, input.ActorID)},
	})
	if e.IsNonNil(err) {
		return err
	}

	newVersion := *input.NewVersion
	newVersion.Chat = &tele.Chat{ID: input.ReciverID}

	sentNew, err := hashe.EmitWait(context.Background(), "telegram.message.send", &newVersion)
	if e.IsNonNil(err) {
		return err
	}

	summary, summarySendOpts := telegram.BuildMessageSummary(input.NewVersion)
	prefix := fmt.Sprintf("Пользователь %s изменил сообщение!\nНовая версия:", input.ActorName)
	prefixLen := utils.TgLen(prefix)

	for i := 0; i < len(summarySendOpts.Entities); i++ {
		summarySendOpts.Entities[i].Offset += prefixLen
	}

	toSend := &tele.Message{
		Chat:    &tele.Chat{ID: input.ReciverID},
		Text:    prefix + summary,
		ReplyTo: sentNew,
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

	return e.Nil()
}
