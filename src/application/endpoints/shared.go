package endpoints

import (
	"encoding/json"
	"time"

	postgresql "github.com/ChatDetectiveORG/business-events-edited-handler/src/infrastructure/postgresql"
	e "github.com/ChatDetectiveORG/shared/errors"
	models "github.com/ChatDetectiveORG/shared/postgresModels"
	"github.com/ChatDetectiveORG/shared/telegram/rawmessage"
	utils "github.com/ChatDetectiveORG/shared/utils"
	"github.com/go-pg/pg/v10"
	tele "gopkg.in/telebot.v4"
)

func GetMessageInfo(mid int, businessConnectionID string, chatID int64) (*models.Message, *e.ErrorInfo) {
	chatIDHash, err := utils.ToSecureHash(chatID)
	if e.IsNonNil(err) {
		return nil, e.FromError(err, "failed to encrypt chat id")
	}

	businessConnectionIDHash, err := utils.ToSecureHash(businessConnectionID)
	if e.IsNonNil(err) {
		return nil, e.FromError(err, "failed to get secure hash")
	}

	message := &models.Message{
		ChatIDHash:               chatIDHash,
		MessageID:                mid,
		BusinessConnectionIDHash: businessConnectionIDHash,
	}

	db := postgresql.GetDB()
	eraw := db.Model(message).
		Where("chat_id_hash = ? AND message_id = ? AND business_connection_id_hash = ?", message.ChatIDHash, message.MessageID, message.BusinessConnectionIDHash).
		Select()
	if e.IsNonNil(eraw) {
		return nil, e.FromError(eraw, "failed to select deleted message").WithData(map[string]any{
			"chat_id_hash":                message.ChatIDHash,
			"message_id":                  message.MessageID,
			"business_connection_id_hash": message.BusinessConnectionIDHash,
		})
	}

	return message, e.Nil()
}

func GetBotUser(businessConnectionIDHash string) (*models.Telegramuser, *e.ErrorInfo) {
	user := &models.Telegramuser{
		BusinessConnectionIDHash: businessConnectionIDHash,
	}

	db := postgresql.GetDB()
	eraw := db.Model(user).
		Where("business_connection_id_hash = ?", user.BusinessConnectionIDHash).
		Select()
	if e.IsNonNil(eraw) {
		return nil, e.FromError(eraw, "failed to select bot user").WithData(map[string]any{
			"business_connection_id_hash": user.BusinessConnectionIDHash,
		})
	}

	return user, e.Nil()
}

func ResolveBotUser(businessConnectionID string, msg *tele.Message) (*models.Telegramuser, *e.ErrorInfo) {
	db := postgresql.GetDB()
	user, err := models.ResolveBotUserByBusinessConnection(db, businessConnectionID, msg)
	if e.IsNonNil(err) {
		return nil, err
	}
	return user, e.Nil()
}

// MessageMetadataLoad holds decrypted metadata loaded in a single pass.
type MessageMetadataLoad struct {
	Stored rawmessage.StoredMessage
	Legacy *tele.Message
	Parsed *tele.Message
}

func LoadMessageMetadata(message *models.Message, key []byte) (MessageMetadataLoad, *e.ErrorInfo) {
	stored, legacy, loadErr := rawmessage.LoadStoredMessage(int(message.MetadataFormat), message.Metadata, key)
	if loadErr != nil {
		return MessageMetadataLoad{}, e.FromError(loadErr, "failed to load stored metadata")
	}

	result := MessageMetadataLoad{
		Stored: stored,
		Legacy: legacy,
	}
	if legacy != nil {
		result.Parsed = legacy
		return result, e.Nil()
	}
	if len(stored.Payload) == 0 {
		return MessageMetadataLoad{}, e.NewError("empty metadata", "failed to load message metadata").WithSeverity(e.Warning)
	}

	parsed := &tele.Message{}
	if uerr := json.Unmarshal(stored.Payload, parsed); uerr != nil {
		return MessageMetadataLoad{}, e.FromError(uerr, "failed to unmarshal message metadata")
	}
	result.Parsed = parsed
	return result, e.Nil()
}

func GetMetadata(message *models.Message) (*tele.Message, *e.ErrorInfo) {
	stored, legacy, err := loadStoredMessage(message)
	if e.IsNonNil(err) {
		return nil, err
	}
	if legacy != nil {
		return legacy, e.Nil()
	}
	if len(stored.Payload) == 0 {
		return nil, e.NewError("empty metadata", "failed to load message metadata").WithSeverity(e.Warning)
	}
	parsed := &tele.Message{}
	if uerr := json.Unmarshal(stored.Payload, parsed); uerr != nil {
		return nil, e.FromError(uerr, "failed to unmarshal message metadata")
	}
	return parsed, e.Nil()
}

func GetStoredMetadata(message *models.Message) (rawmessage.StoredMessage, *tele.Message, *e.ErrorInfo) {
	return loadStoredMessage(message)
}

func loadStoredMessage(message *models.Message) (rawmessage.StoredMessage, *tele.Message, *e.ErrorInfo) {
	botUser, err := GetBotUser(message.BusinessConnectionIDHash)
	if e.IsNonNil(err) {
		return rawmessage.StoredMessage{}, nil, err
	}

	key, err := utils.DecryptUserKey(botUser.DataEncryptionKey)
	if e.IsNonNil(err) {
		return rawmessage.StoredMessage{}, nil, e.FromError(err, "failed to decrypt user key")
	}

	stored, legacy, loadErr := rawmessage.LoadStoredMessage(int(message.MetadataFormat), message.Metadata, key)
	if loadErr != nil {
		return rawmessage.StoredMessage{}, nil, e.FromError(loadErr, "failed to load stored metadata")
	}
	return stored, legacy, e.Nil()
}

func LoadMediaGroupRawMessages(message *models.Message, key []byte) ([]json.RawMessage, int, *e.ErrorInfo) {
	if message.MediaGroupIDHash == "" {
		return nil, -1, e.Nil()
	}

	db := postgresql.GetDB()
	var rows []*models.Message
	eRaw := db.Model(&rows).
		Where("media_group_id_hash = ?", message.MediaGroupIDHash).
		Order("message_id ASC").
		Select()
	if e.IsNonNil(eRaw) {
		return nil, -1, e.FromError(eRaw, "failed to get media group messages")
	}

	raws := make([]json.RawMessage, 0, len(rows))
	editedIndex := -1
	for i, row := range rows {
		if row.MessageID == message.MessageID {
			editedIndex = i
		}
		stored, legacy, err := rawmessage.LoadStoredMessage(int(row.MetadataFormat), row.Metadata, key)
		if err != nil {
			return nil, -1, e.FromError(err, "failed to load media group metadata")
		}
		if len(stored.Payload) > 0 {
			raws = append(raws, stored.Payload)
			continue
		}
		if legacy == nil {
			continue
		}
		payload, merr := json.Marshal(legacy)
		if merr != nil {
			return nil, -1, e.FromError(merr, "failed to marshal legacy media group metadata")
		}
		raws = append(raws, payload)
	}
	return raws, editedIndex, e.Nil()
}

func GetUserHierarchyByTelegramID(tgUserID int64) (models.UserHierarchy, *e.ErrorInfo) {
	idHash, err := utils.ToSecureHash(tgUserID)
	if e.IsNonNil(err) {
		return models.UserHierarchy{}, err
	}

	user := &models.Telegramuser{}
	db := postgresql.GetDB()
	eraw := db.Model(user).Where("id_hash = ?", idHash).Select()
	if eraw == pg.ErrNoRows {
		return models.UserHierarchy{}, e.Nil()
	}
	if eraw != nil {
		return models.UserHierarchy{}, e.FromError(eraw, "failed to get user hierarchy").WithSeverity(e.Notice)
	}

	return models.GetUserHierarchy(db, user.ID, time.Now())
}

func CanReceiveByHierarchy(receiverID int64, actorID int64) (bool, *e.ErrorInfo) {
	receiver, err := GetUserHierarchyByTelegramID(receiverID)
	if e.IsNonNil(err) {
		return false, err
	}
	actor, err := GetUserHierarchyByTelegramID(actorID)
	if e.IsNonNil(err) {
		return false, err
	}
	return models.CanReceiveNotification(receiver, actor), e.Nil()
}
