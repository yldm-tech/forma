package survey

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/survey/surveydoc"
)

// singleUseSignaturePrefix is forma2's SINGLE_USE_SIGNATURE_PAYLOAD_PREFIX.
const singleUseSignaturePrefix = "forma.single-use.v1"

// SingleUseLink is the query of one single-use survey link: an encrypted id alone, or a plain id with its signature.
type SingleUseLink struct {
	SuID    string `json:"suId"`
	SuToken string `json:"suToken,omitempty"`
}

// SingleUseSignature is forma2's generateSurveySingleUseSignature: HMAC-SHA256 of "forma.single-use.v1:<surveyId>:<suId>" under ENCRYPTION_KEY, hex. forma2 keyed it with the key's text; a 64-character hex key, the documented form, is its lowercase hex here.
func SingleUseSignature(key []byte, surveyID uuid.UUID, singleUseID string) string {
	mac := hmac.New(sha256.New, []byte(hex.EncodeToString(key)))
	mac.Write([]byte(singleUseSignaturePrefix + ":" + surveyID.String() + ":" + singleUseID))
	return hex.EncodeToString(mac.Sum(nil))
}

// singleUseContext binds an encrypted single-use id to its survey, so an id sealed for one survey does not open for another.
func singleUseContext(surveyID uuid.UUID) []byte {
	return []byte("survey-single-use:" + surveyID.String())
}

// SingleUseLinksInput is forma2's ZGenerateSingleUseIdAction without the survey id.
type SingleUseLinksInput struct {
	IsEncrypted bool    `json:"isEncrypted"`
	Count       *int    `json:"count"`
	SingleUseID *string `json:"singleUseId"`
}

// SingleUseLinks is forma2's generateSingleUseIdsAction behind workspace.write: count new links, encrypted (the id sealed under ENCRYPTION_KEY) or plain (a new or custom id with its signature). A custom id is allowed only alone and unencrypted.
func (s *Service) SingleUseLinks(ctx context.Context, caller Caller, surveyID uuid.UUID, in SingleUseLinksInput) ([]SingleUseLink, error) {
	count := 1
	if in.Count != nil {
		count = *in.Count
	}
	if count < 1 || count > 5000 {
		return nil, apperr.Validation([]apperr.FieldError{{Field: "count", Rule: "range", Param: "1-5000"}})
	}
	custom := ""
	if in.SingleUseID != nil {
		custom = strings.TrimSpace(*in.SingleUseID)
		if custom == "" || len([]rune(custom)) > 255 {
			return nil, apperr.Validation([]apperr.FieldError{{Field: "singleUseId", Rule: "length", Param: "1-255"}})
		}
		if in.IsEncrypted || count != 1 {
			return nil, apperr.InvalidInput(apperr.CodeValidation, "Custom single-use IDs can only be generated one at a time without encryption")
		}
	}
	if _, err := s.authorized(ctx, caller, surveyID, authz.SurveyWrite); err != nil {
		return nil, err
	}
	links := make([]SingleUseLink, 0, count)
	for range count {
		if in.IsEncrypted {
			sealed, err := s.cipher.Seal([]byte(surveydoc.NewID()), singleUseContext(surveyID))
			if err != nil {
				return nil, fmt.Errorf("seal single-use id: %w", err)
			}
			links = append(links, SingleUseLink{SuID: base64.RawURLEncoding.EncodeToString(sealed)})
			continue
		}
		id := custom
		if id == "" {
			id = surveydoc.NewID()
		}
		links = append(links, SingleUseLink{SuID: id, SuToken: SingleUseSignature(s.cfg.EncryptionKey, surveyID, id)})
	}
	return links, nil
}
