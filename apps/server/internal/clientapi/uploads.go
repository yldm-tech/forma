package clientapi

import (
	"encoding/json"
	"net/url"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/storage"
)

// fileUploadElement is a survey's element as the upload rules read it.
type fileUploadElement = storage.AnswerElement

// surveyElements reads a survey's elements in document order.
func surveyElements(blocks json.RawMessage) ([]fileUploadElement, error) {
	return storage.AnswerElements(blocks)
}

// fileUploadElements is forma2's getSurveyFileUploadConfigs.
func fileUploadElements(elements []fileUploadElement) []fileUploadElement {
	return storage.FileUploadElements(elements)
}

// validClientFileUploads is forma2's validateClientFileUploads for the client widget, which only ever emits the fully scoped shape: no legacy prefix is accepted.
func (h *Handler) validClientFileUploads(data map[string]any, elements []fileUploadElement, workspaceID, surveyID uuid.UUID) bool {
	return storage.ValidAnswerFiles(data, elements, storage.UploadScope{WorkspaceID: workspaceID, SurveyID: surveyID, AppOrigins: []*url.URL{h.cfg.WebAppURL, h.cfg.PublicURL}})
}

// otherOptionTooLong is forma2's validateOtherOptionLengthForMultipleChoice.
func otherOptionTooLong(data map[string]any, keys []string, elements []fileUploadElement, responseLanguage *string) string {
	return storage.OtherOptionTooLong(data, keys, elements, responseLanguage)
}
