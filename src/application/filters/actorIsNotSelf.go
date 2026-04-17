package filters

import (
	tele "gopkg.in/telebot.v4"
)

// type UpdateFilter interface {
// 	Filter(update tele.Update) bool
// }

// ActorIsNotSelf фильтрует обновления, в которых actor (отправитель сообщения) не является обладателем бота.
// Помогает предотвратить отправку сообщений если пользователь удалил сам свои сообщения
type ActorIsNotSelf struct {}

func (f ActorIsNotSelf) Filter(update tele.Update) bool {
	if update.Message == nil && update.BusinessMessage == nil && update.EditedBusinessMessage == nil && update.DeletedBusinessMessages == nil {
		return true
	}

	if update.Message != nil && update.Message.Sender.ID == update.Message.Chat.ID {
		return true
	}

	if update.BusinessMessage != nil && update.BusinessMessage.Sender.ID == update.BusinessMessage.Chat.ID {
		return true
	}

	if update.EditedBusinessMessage != nil && update.EditedBusinessMessage.Sender.ID == update.EditedBusinessMessage.Chat.ID {
		return true
	}

	// Пользователь может удалять одновременно и свои, и чужие сообщения
	// Поэтому чтобы не бомбить базу запросами на проверку владельца сообщения для каждого сообщения и вычисления того, чьи сообщения в итоге удалены
	// Просто идёт пропуск фильтрации
	//
	// ToDo: Сделать удобный способ проверить сендера каждого сообщения
	// Или сделать фичу платной
	// А лучше - всё сразу ))))
	if update.DeletedBusinessMessages != nil {
		return true
	}

	return false
}
