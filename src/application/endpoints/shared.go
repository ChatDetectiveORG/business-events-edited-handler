package endpoints

import (
	e "github.com/ChatDetectiveORG/shared/errors"
	utils "github.com/ChatDetectiveORG/shared/utils"
	models "github.com/ChatDetectiveORG/shared/postgresModels"
	postgresql "github.com/ChatDetectiveORG/business-events-edited-handler/src/infrastructure/postgresql"
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
