package endpoints

import (
	"encoding/json"

	postgresql "github.com/ChatDetectiveORG/business-events-edited-handler/src/infrastructure/postgresql"
	e "github.com/ChatDetectiveORG/shared/errors"
	models "github.com/ChatDetectiveORG/shared/postgresModels"
	utils "github.com/ChatDetectiveORG/shared/utils"
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
		ChatIDHash: chatIDHash,
		MessageID: mid,
		BusinessConnectionIDHash: businessConnectionIDHash,
	}

	db := postgresql.GetDB()
	eraw := db.Model(message).
		Where("chat_id_hash = ? AND message_id = ? AND business_connection_id_hash = ?", message.ChatIDHash, message.MessageID, message.BusinessConnectionIDHash).
		Select()
	if e.IsNonNil(eraw) {
		return nil,e.FromError(eraw, "failed to select deleted message").WithData(map[string]any{
			"chat_id_hash": message.ChatIDHash,
			"message_id": message.MessageID,
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

func GetMetadata(message *models.Message) (*tele.Message, *e.ErrorInfo) {
	botUser, err := GetBotUser(message.BusinessConnectionIDHash)
	if e.IsNonNil(err) {
		return nil, err
	}

	key, err := utils.DecryptUserKey(botUser.DataEncryptionKey)
	if e.IsNonNil(err) {
		return nil, e.FromError(err, "failed to decrypt user key")
	}

	decryptedMetadataJson, err := utils.Decrypt(message.Metadata, key)
	if e.IsNonNil(err) {
		return nil, e.FromError(err, "failed to decrypt message metadata")
	}

	var metadata = &tele.Message{}
	eraw := json.Unmarshal(decryptedMetadataJson, metadata)
	if e.IsNonNil(eraw) {
		return nil, e.FromError(eraw, "failed to unmarshal message metadata")
	}

	return metadata, e.Nil()
}
