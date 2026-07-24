package editedMessage_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ChatDetectiveORG/business-events-edited-handler/src/application/endpoints/editedMessage"
	postgresql "github.com/ChatDetectiveORG/business-events-edited-handler/src/infrastructure/postgresql"
	e "github.com/ChatDetectiveORG/shared/errors"
	"github.com/ChatDetectiveORG/shared/testutil/chaintest"
	"github.com/ChatDetectiveORG/shared/testutil/pgfixture"
	"github.com/ChatDetectiveORG/shared/telegram/rawmessage"
)

const (
	testBusinessConnection = "test-business-connection"
	testOwnerID            = int64(900001)
	testCustomerID         = int64(900002)
	testMessageID          = 501
)

// TestEditedMessageChain publishes notification and updates metadata after a text edit.
// Requires CHAINTEST_DATABASE_URL (see pgfixture.FormatSkipHint).
func TestEditedMessageChain_TextEdit(t *testing.T) {
	db := pgfixture.Open(t)
	pgfixture.Reset(t, db)
	t.Cleanup(postgresql.ResetDBForTest)

	postgresql.SetDBForTest(db)

	owner := pgfixture.SeedBotUser(t, db, pgfixture.BotUserSpec{
		TelegramID:           testOwnerID,
		BusinessConnectionID: testBusinessConnection,
	})
	message := pgfixture.SeedBusinessMessage(t, db, owner, pgfixture.BusinessMessageSpec{
		BusinessConnectionID: testBusinessConnection,
		CustomerChatID:       testCustomerID,
		MessageID:            testMessageID,
		Text:                 "old text",
	})

	update := chaintest.EditedBusinessMessageUpdate(
		testMessageID,
		testBusinessConnection,
		testCustomerID,
		"Customer",
		"new text",
	)

	capture := chaintest.NewOutgoingCapture(t, 8)
	chaintest.RunEndpoint(t, editedMessage.NewEditedMessageEndpoint(), update, capture, "mirror-test")

	requests := capture.Collect(500 * time.Millisecond)
	if len(requests) == 0 {
		t.Fatal("expected outgoing requests for edited text message")
	}
	chaintest.AssertAnyTextSubstr(t, requests, "old text", "new text")

	reloaded := message
	if err := db.Model(reloaded).WherePK().Select(); err != nil {
		t.Fatalf("reload message: %v", err)
	}
	if reloaded.MetadataFormat != rawmessage.MetadataFormatRawAPIv1 {
		t.Fatalf("metadata format = %d, want raw api v1", reloaded.MetadataFormat)
	}
	parsed := pgfixture.LoadMessageMetadata(t, owner, reloaded)
	if !strings.Contains(parsed.Text, "new text") {
		t.Fatalf("stored metadata text = %q, want new text", parsed.Text)
	}
}

func TestEditedMessageChain_PollEdit(t *testing.T) {
	db := pgfixture.Open(t)
	pgfixture.Reset(t, db)
	t.Cleanup(postgresql.ResetDBForTest)

	postgresql.SetDBForTest(db)

	owner := pgfixture.SeedBotUser(t, db, pgfixture.BotUserSpec{
		TelegramID:           testOwnerID,
		BusinessConnectionID: testBusinessConnection,
	})
	oldPollMetadata := json.RawMessage(`{
		"message_id": ` + itoa(testMessageID) + `,
		"business_connection_id": "` + testBusinessConnection + `",
		"chat": {"id": ` + itoa64(testCustomerID) + `},
		"poll": {
			"question": "Old question?",
			"options": [{"text": "A"}, {"text": "B"}]
		}
	}`)
	message := pgfixture.SeedBusinessMessage(t, db, owner, pgfixture.BusinessMessageSpec{
		BusinessConnectionID: testBusinessConnection,
		CustomerChatID:       testCustomerID,
		MessageID:            testMessageID,
		RawJSON:              oldPollMetadata,
	})

	newPoll := json.RawMessage(`{
		"question": "New question?",
		"options": [{"text": "C"}, {"text": "D"}]
	}`)
	update := chaintest.EditedBusinessMessagePollUpdate(
		testMessageID,
		testBusinessConnection,
		testCustomerID,
		"Customer",
		newPoll,
	)

	capture := chaintest.NewOutgoingCapture(t, 8)
	chaintest.RunEndpoint(t, editedMessage.NewEditedMessageEndpoint(), update, capture, "mirror-test")

	requests := capture.Collect(500 * time.Millisecond)
	if chaintest.CountRawMethod(requests, "sendPoll") < 2 {
		t.Fatalf("expected at least 2 sendPoll raw_api requests, got %#v", requests)
	}
	chaintest.AssertAnyTextSubstr(t, requests, "изменил сообщение")

	reloaded := message
	if err := db.Model(reloaded).WherePK().Select(); err != nil {
		t.Fatalf("reload message: %v", err)
	}
	stored, _, loadErr := rawmessage.LoadStoredMessage(int(reloaded.MetadataFormat), reloaded.Metadata, owner.DataKey)
	if loadErr != nil {
		t.Fatalf("load metadata: %v", loadErr)
	}
	if !strings.Contains(string(stored.Payload), "New question?") {
		t.Fatalf("stored metadata = %s, want new poll question", stored.Payload)
	}
}

func TestEditedMessageChain_ChecklistEdit(t *testing.T) {
	db := pgfixture.Open(t)
	pgfixture.Reset(t, db)
	t.Cleanup(postgresql.ResetDBForTest)

	postgresql.SetDBForTest(db)

	owner := pgfixture.SeedBotUser(t, db, pgfixture.BotUserSpec{
		TelegramID:           testOwnerID,
		BusinessConnectionID: testBusinessConnection,
	})
	oldChecklistMetadata := json.RawMessage(`{
		"message_id": ` + itoa(testMessageID) + `,
		"business_connection_id": "` + testBusinessConnection + `",
		"chat": {"id": ` + itoa64(testCustomerID) + `},
		"checklist": {
			"title": "Old list",
			"tasks": [{"id": 1, "text": "Old task"}]
		}
	}`)
	message := pgfixture.SeedBusinessMessage(t, db, owner, pgfixture.BusinessMessageSpec{
		BusinessConnectionID: testBusinessConnection,
		CustomerChatID:       testCustomerID,
		MessageID:            testMessageID,
		RawJSON:              oldChecklistMetadata,
	})

	newChecklist := json.RawMessage(`{
		"title": "New list",
		"tasks": [{"id": 1, "text": "New task"}]
	}`)
	update := chaintest.EditedBusinessMessageChecklistUpdate(
		testMessageID,
		testBusinessConnection,
		testCustomerID,
		"Customer",
		newChecklist,
	)

	capture := chaintest.NewOutgoingCapture(t, 8)
	chaintest.RunEndpoint(t, editedMessage.NewEditedMessageEndpoint(), update, capture, "mirror-test")

	requests := capture.Collect(500 * time.Millisecond)
	if chaintest.CountRawMethod(requests, "sendMessage") < 2 {
		t.Fatalf("expected at least 2 sendMessage raw_api requests for checklist notification fallback, got %#v", requests)
	}
	chaintest.AssertAnyTextSubstr(t, requests, "изменил сообщение")

	reloaded := message
	if err := db.Model(reloaded).WherePK().Select(); err != nil {
		t.Fatalf("reload message: %v", err)
	}
	stored, _, loadErr := rawmessage.LoadStoredMessage(int(reloaded.MetadataFormat), reloaded.Metadata, owner.DataKey)
	if loadErr != nil {
		t.Fatalf("load metadata: %v", loadErr)
	}
	if !strings.Contains(string(stored.Payload), "New list") {
		t.Fatalf("stored metadata = %s, want new checklist title", stored.Payload)
	}
}

func TestEditedMessageChain_RichEdit(t *testing.T) {
	db := pgfixture.Open(t)
	pgfixture.Reset(t, db)
	t.Cleanup(postgresql.ResetDBForTest)

	postgresql.SetDBForTest(db)

	owner := pgfixture.SeedBotUser(t, db, pgfixture.BotUserSpec{
		TelegramID:           testOwnerID,
		BusinessConnectionID: testBusinessConnection,
	})
	oldRichMetadata := json.RawMessage(`{
		"message_id": ` + itoa(testMessageID) + `,
		"business_connection_id": "` + testBusinessConnection + `",
		"chat": {"id": ` + itoa64(testCustomerID) + `},
		"rich_message": {
			"blocks": [{"type": "paragraph", "text": "Old rich"}]
		}
	}`)
	message := pgfixture.SeedBusinessMessage(t, db, owner, pgfixture.BusinessMessageSpec{
		BusinessConnectionID: testBusinessConnection,
		CustomerChatID:       testCustomerID,
		MessageID:            testMessageID,
		RawJSON:              oldRichMetadata,
	})

	newRich := json.RawMessage(`{
		"blocks": [{"type": "paragraph", "text": "New rich"}]
	}`)
	update := chaintest.EditedBusinessMessageRichUpdate(
		testMessageID,
		testBusinessConnection,
		testCustomerID,
		"Customer",
		newRich,
	)

	capture := chaintest.NewOutgoingCapture(t, 8)
	chaintest.RunEndpoint(t, editedMessage.NewEditedMessageEndpoint(), update, capture, "mirror-test")

	requests := capture.Collect(500 * time.Millisecond)
	if chaintest.CountRawMethod(requests, "sendRichMessage") < 2 {
		t.Fatalf("expected at least 2 sendRichMessage raw_api requests, got %#v", requests)
	}
	chaintest.AssertAnyTextSubstr(t, requests, "изменил сообщение")

	reloaded := message
	if err := db.Model(reloaded).WherePK().Select(); err != nil {
		t.Fatalf("reload message: %v", err)
	}
	stored, _, loadErr := rawmessage.LoadStoredMessage(int(reloaded.MetadataFormat), reloaded.Metadata, owner.DataKey)
	if loadErr != nil {
		t.Fatalf("load metadata: %v", loadErr)
	}
	if !strings.Contains(string(stored.Payload), "New rich") {
		t.Fatalf("stored metadata = %s, want new rich text", stored.Payload)
	}
}

func TestEditedMessageChain_GiveawayMetadataNotUpdated(t *testing.T) {
	db := pgfixture.Open(t)
	pgfixture.Reset(t, db)
	t.Cleanup(postgresql.ResetDBForTest)

	postgresql.SetDBForTest(db)

	owner := pgfixture.SeedBotUser(t, db, pgfixture.BotUserSpec{
		TelegramID:           testOwnerID,
		BusinessConnectionID: testBusinessConnection,
	})
	oldGiveawayMetadata := json.RawMessage(`{
		"message_id": ` + itoa(testMessageID) + `,
		"business_connection_id": "` + testBusinessConnection + `",
		"chat": {"id": ` + itoa64(testCustomerID) + `},
		"giveaway": {
			"chat_ids": [1],
			"winners_selection_date": 1700000000,
			"winner_count": 1
		}
	}`)
	message := pgfixture.SeedBusinessMessage(t, db, owner, pgfixture.BusinessMessageSpec{
		BusinessConnectionID: testBusinessConnection,
		CustomerChatID:       testCustomerID,
		MessageID:            testMessageID,
		RawJSON:              oldGiveawayMetadata,
	})

	update := chaintest.EditedBusinessMessageUpdate(
		testMessageID,
		testBusinessConnection,
		testCustomerID,
		"Customer",
		"edited text",
	)

	capture := chaintest.NewOutgoingCapture(t, 4)
	errInfo := chaintest.RunEndpointExpectError(t, editedMessage.NewEditedMessageEndpoint(), update, capture, "mirror-test")
	if !e.IsNonNil(errInfo) {
		t.Fatal("expected handler error for unreplayable giveaway metadata")
	}

	requests := capture.Collect(100 * time.Millisecond)
	if chaintest.CountRawMethod(requests, "sendMessage") > 0 || chaintest.CountRawMethod(requests, "sendPoll") > 0 {
		t.Fatalf("unexpected raw_api requests for giveaway metadata: %#v", requests)
	}

	reloaded := message
	if err := db.Model(reloaded).WherePK().Select(); err != nil {
		t.Fatalf("reload message: %v", err)
	}
	stored, _, loadErr := rawmessage.LoadStoredMessage(int(reloaded.MetadataFormat), reloaded.Metadata, owner.DataKey)
	if loadErr != nil {
		t.Fatalf("load metadata: %v", loadErr)
	}
	if strings.Contains(string(stored.Payload), "edited text") {
		t.Fatalf("metadata must not be updated on builder error, got %s", stored.Payload)
	}
	if !strings.Contains(string(stored.Payload), "giveaway") {
		t.Fatalf("metadata should still contain giveaway snapshot, got %s", stored.Payload)
	}
}

func itoa(v int) string {
	return strconv.Itoa(v)
}

func itoa64(v int64) string {
	return strconv.FormatInt(v, 10)
}
