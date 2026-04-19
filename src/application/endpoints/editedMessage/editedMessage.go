package editedMessage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

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

// highlightTextDiff controls whether changed words are highlighted with bold in edit notifications.
const highlightTextDiff = true

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

		// Compute diff before metadata.Text is modified so both originals are available.
		var oldDiffEntities, newDiffEntities []tele.MessageEntity
		if highlightTextDiff {
			oldDiffEntities, newDiffEntities = computeDiffBoldEntities(metadata.Text, newMessage.Text)
		}

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

		if highlightTextDiff {
			for _, ent := range oldDiffEntities {
				ent.Offset += prefixLen
				metadata.Entities = append(metadata.Entities, ent)
			}
			for _, ent := range newDiffEntities {
				ent.Offset += prefixLen + originalTextLen + postfixLen
				metadata.Entities = append(metadata.Entities, ent)
			}
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

	// Apply diff highlighting to the fallback case (separate old/new messages).
	if highlightTextDiff && metadata.Text != "" && newMessage.Text != "" {
		oldDiffEntities, newDiffEntities := computeDiffBoldEntities(metadata.Text, newMessage.Text)
		metadata.Entities = append(metadata.Entities, oldDiffEntities...)
		newMessage.Entities = append(newMessage.Entities, newDiffEntities...)
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

// --- text diff helpers ---

// wordToken holds a word extracted from a string together with its UTF-16 offset and length,
// which is what Telegram uses for entity positions.
type wordToken struct {
	text   string
	offset int
	length int
}

func utf16RuneLen(r rune) int {
	if r >= 0x10000 {
		return 2
	}
	return 1
}

// tokenizeWithOffsets splits text into non-whitespace word tokens and records the UTF-16
// offset/length of every token within the original string.
func tokenizeWithOffsets(text string) []wordToken {
	runes := []rune(text)
	var tokens []wordToken
	utf16Off := 0
	i := 0
	for i < len(runes) {
		if unicode.IsSpace(runes[i]) {
			utf16Off += utf16RuneLen(runes[i])
			i++
			continue
		}
		start := i
		startOff := utf16Off
		for i < len(runes) && !unicode.IsSpace(runes[i]) {
			utf16Off += utf16RuneLen(runes[i])
			i++
		}
		tokens = append(tokens, wordToken{
			text:   string(runes[start:i]),
			offset: startOff,
			length: utf16Off - startOff,
		})
	}
	return tokens
}

// lcsTable computes the standard LCS dynamic-programming table for two token slices.
func lcsTable(a, b []wordToken) [][]int {
	m, n := len(a), len(b)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1].text == b[j-1].text {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	return dp
}

// findChangedWords backtracks through the LCS table and marks tokens that are not part of the
// longest common subsequence (i.e. tokens that changed).
func findChangedWords(a, b []wordToken) (changedA, changedB []bool) {
	dp := lcsTable(a, b)
	changedA = make([]bool, len(a))
	changedB = make([]bool, len(b))
	i, j := len(a), len(b)
	for i > 0 && j > 0 {
		if a[i-1].text == b[j-1].text {
			i--
			j--
		} else if dp[i-1][j] >= dp[i][j-1] {
			changedA[i-1] = true
			i--
		} else {
			changedB[j-1] = true
			j--
		}
	}
	for i > 0 {
		changedA[i-1] = true
		i--
	}
	for j > 0 {
		changedB[j-1] = true
		j--
	}
	return
}

// boldEntitiesForChanged converts a boolean mask of changed tokens into merged EntityBold
// entities. Consecutive changed tokens (including the whitespace gap between them) are
// collapsed into a single entity for a cleaner visual result.
func boldEntitiesForChanged(tokens []wordToken, changed []bool) []tele.MessageEntity {
	var entities []tele.MessageEntity
	i := 0
	for i < len(tokens) {
		if !changed[i] {
			i++
			continue
		}
		startOff := tokens[i].offset
		endOff := tokens[i].offset + tokens[i].length
		i++
		for i < len(tokens) && changed[i] {
			endOff = tokens[i].offset + tokens[i].length
			i++
		}
		entities = append(entities, tele.MessageEntity{
			Type:   tele.EntityBold,
			Offset: startOff,
			Length: endOff - startOff,
		})
	}
	return entities
}

// computeDiffBoldEntities returns two slices of EntityBold entities — one for the old text
// and one for the new text — marking the words that differ between them.
// Offsets are relative to the start of each respective text.
func computeDiffBoldEntities(oldText, newText string) (oldEntities, newEntities []tele.MessageEntity) {
	oldTokens := tokenizeWithOffsets(oldText)
	newTokens := tokenizeWithOffsets(newText)
	changedOld, changedNew := findChangedWords(oldTokens, newTokens)
	oldEntities = boldEntitiesForChanged(oldTokens, changedOld)
	newEntities = boldEntitiesForChanged(newTokens, changedNew)
	return
}

// --- end text diff helpers ---

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
