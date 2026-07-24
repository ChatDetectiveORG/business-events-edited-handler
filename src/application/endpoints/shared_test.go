package endpoints_test

import (
	"testing"

	endpoints "github.com/ChatDetectiveORG/business-events-edited-handler/src/application/endpoints"
	models "github.com/ChatDetectiveORG/shared/postgresModels"
	e "github.com/ChatDetectiveORG/shared/errors"
)

func TestLoadMediaGroupRawMessagesSkipsEmptyHash(t *testing.T) {
	raws, editedIndex, err := endpoints.LoadMediaGroupRawMessages(&models.Message{
		MediaGroupIDHash: "",
	}, []byte("01234567890123456789012345678901"))
	if !e.IsNil(err) {
		t.Fatalf("LoadMediaGroupRawMessages: %v", err)
	}
	if raws != nil {
		t.Fatalf("raws = %#v, want nil", raws)
	}
	if editedIndex != -1 {
		t.Fatalf("editedIndex = %d, want -1", editedIndex)
	}
}
