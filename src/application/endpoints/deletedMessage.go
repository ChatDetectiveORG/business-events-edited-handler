package endpoints

// import (
// 	"app/src/infrastructure/postgresql"
// 	"slices"
// 	"time"

// 	e "github.com/ChatDetectiveORG/shared/errors"
// 	h "github.com/ChatDetectiveORG/shared/handlers"
// 	models "github.com/ChatDetectiveORG/shared/postgresModels"
// 	utils "github.com/ChatDetectiveORG/shared/utils"
// 	tele "gopkg.in/telebot.v4"
// )

// func NewDeletedMessageEndpoint() h.Endpoint {
// 	ep := h.Endpoint{}
// 	ep.Init(
// 		"save",
// 		*h.HandlerChain{}.Init(
// 			10 * time.Second,
// 			h.InitChainHandler(getAllDeletedMessagesInfo, h.EndOnError),
// 		),
// 		h.BusinessEvent(h.BusEventTypeNew),
// 	)

// 	return ep
// }

// func getDeletedMessageInfo(mid int, update tele.Update, hashe *h.HandlerChainHashe) *e.ErrorInfo {
// 	message := &models.Message{
// 		ChatID: utils.Int64ToHash(update.DeletedBusinessMessages.Chat.ID),
// 		MessageID: mid,
// 		BusinessConnectionID: update.DeletedBusinessMessages.BusinessConnectionID,
// 	}

// 	db := postgresql.GetDB()
// 	err := db.Model(message).
// 		Where("chat_id = ? AND message_id = ? AND business_connection_id = ?", message.ChatID, message.MessageID, message.BusinessConnectionID).
// 		Select()
// 	if e.IsNonNil(err) {
// 		return e.FromError(err, "failed to select deleted message")
// 	}

// 	handledMessageIDS, ok := hashe.Get("handledMessageIDS")
// 	if !ok {
// 		handledMessageIDS = []int{}
// 	}

// 	if slices.Contains(handledMessageIDS.([]int), mid) {
// 		return e.Nil()
// 	}

// 	toSend, ok := hashe.Get("toSend")
// 	if !ok {
// 		toSend = []interface{}{}
// 	}


// 	if message.Metadata.AlbumID != "" {
// 		var mediaGroupParticipants []*models.Message

// 		err := db.Model(&mediaGroupParticipants).
// 			Where("metadata->>'album_id' = ?", message.Metadata.AlbumID).
// 			Order("created_at ASC").
// 			Select()
// 		if e.IsNonNil(err) {
// 			return e.FromError(err, "failed to select media group participants")
// 		}

// 		var mediaGroupParticipantsTelegram []*tele.Message
// 		for _, mediaGroupParticipant := range mediaGroupParticipants {
// 			mediaGroupParticipantsTelegram = append(mediaGroupParticipantsTelegram, mediaGroupParticipant.Metadata)
// 		}

// 		mediaGroup, _, ok := BuildMediaGroup(mediaGroupParticipantsTelegram)
// 		if !ok {
// 			return e.FromError(err, "failed to build media group")
// 		}

// 		toSend = append(toSend.([]interface{}), mediaGroup)
// 		hashe.Set("toSend", toSend)

// 		handledMessageIDS = append(handledMessageIDS.([]int), message.MessageID)
// 		hashe.Set("handledMessageIDS", handledMessageIDS)

// 		return e.Nil()
// 	}

// 	handledMessageIDS = append(handledMessageIDS.([]int), message.MessageID)
// 	hashe.Set("handledMessageIDS", handledMessageIDS)

// 	m, ok := TgMessageToSendable(message.Metadata)
// 	if !ok {
// 		return e.FromError(err, "failed to convert message to sendable")
// 	}
// 	toSend = append(toSend.([]interface{}), m)
// 	hashe.Set("toSend", toSend)

// 	messageSummaries, ok := hashe.Get("messageSummaries")
// 	if !ok {
// 		return e.FromError("Failed to gt message summaries")
// 	}

// 	return e.Nil()
// }

// func getAllDeletedMessagesInfo(update tele.Update, hashe *h.HandlerChainHashe) *e.ErrorInfo {
// 	for _, deletedMessage := range update.DeletedBusinessMessages.MessageIDs {
// 		getDeletedMessageInfo(deletedMessage, update, hashe)
// 	}

// 	return e.Nil()
// }

// func returnDeletedMessageNotification(update tele.Update, hashe *h.HandlerChainHashe) *e.ErrorInfo {

// 	return e.Nil()
// }
