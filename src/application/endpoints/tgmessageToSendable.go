package endpoints

// TODO: УДАЛИТЬ ЭТОТ ФАЙЛ
// BuildMediaGroup Теряет описания прикреплённые к не-первым медиа

import (
	telegram "github.com/ChatDetectiveORG/shared/telegram"
	tele "gopkg.in/telebot.v4"
)

// TgMessageToSendable converts a Message to a sendable object for bot.Send().
// For media groups, use BuildMediaGroup with all messages.
func TgMessageToSendable(msg *tele.Message) (interface{}, bool) {
	return telegram.TgMessageToSendable(msg)
}

// BuildMessageSummary creates a structured summary with reply, forward, via-bot, quote metadata.
func BuildMessageSummary(msg *tele.Message) *telegram.MessageSummary {
	return telegram.BuildMessageSummary(msg)
}

// !!! Теряет описания прикреплённые к не-первым медиа
// BuildMediaGroup builds an Album from messages sharing the same AlbumID.
func BuildMediaGroup(msgs []*tele.Message) (tele.Album, string, bool) {
	return telegram.BuildMediaGroup(msgs)
}

