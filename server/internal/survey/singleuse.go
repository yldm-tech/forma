package survey

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
	"github.com/yldm-tech/forma/server/internal/survey/surveydoc"
)

// singleUseSignaturePrefix is forma2's SINGLE_USE_SIGNATURE_PAYLOAD_PREFIX.
const singleUseSignaturePrefix = "forma.single-use.v1"

// SingleUseLink is the query of one single-use survey link: an encrypted id alone, or a plain id with its signature.
type SingleUseLink struct {
	SuID    string `json:"suId"`
	SuToken string `json:"suToken,omitempty"`
}

// SingleUseSignature is forma2's generateSurveySingleUseSignature: HMAC-SHA256 of "forma.single-use.v1:<surveyId>:<suId>" keyed with ENCRYPTION_KEY's text, hex. surveyID is the id the link was signed for: the survey's UUID, or for a link forma2 issued, the survey's forma2 id.
func SingleUseSignature(keyText, surveyID, singleUseID string) string {
	mac := hmac.New(sha256.New, []byte(keyText))
	mac.Write([]byte(singleUseSignaturePrefix + ":" + surveyID + ":" + singleUseID))
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
		links = append(links, SingleUseLink{SuID: id, SuToken: SingleUseSignature(string(s.cfg.EncryptionKeyText), surveyID.String(), id)})
	}
	return links, nil
}

// SingleUse is a survey's singleUse document, forma2's TSurveySingleUse.
type SingleUse struct {
	Enabled     bool    `json:"enabled"`
	IsEncrypted bool    `json:"isEncrypted"`
	Heading     *string `json:"heading,omitempty"`
	Subheading  *string `json:"subheading,omitempty"`
}

// SingleUseSettings reads the survey's singleUse document; an absent one is single use switched off.
func (s *Survey) SingleUseSettings() SingleUse {
	var su SingleUse
	if !isNullJSON(s.SingleUse) {
		_ = json.Unmarshal(s.SingleUse, &su)
	}
	return su
}

// CheckSingleUseLink is forma2's checkAndValidateSingleUseId: the canonical single-use id a link's suId and suToken name for the survey, or "" when they name none. An encrypted suId must open to a cuid: one forma2 sealed ("iv:ciphertext:tag" under ENCRYPTION_KEY) or one this server sealed for this survey. A plain suId must carry its signature; a link forma2 issued was signed over the survey's forma2 id, which is also the id in that link's path, so linkSurveyID (the id the request named the survey by) is tried too when it is the survey's forma2 id.
func (s *Service) CheckSingleUseLink(sv *Survey, linkSurveyID, suID, suToken string) string {
	if strings.TrimSpace(suID) == "" {
		return ""
	}
	if sv.SingleUseSettings().IsEncrypted {
		id := s.openSingleUseID(sv.ID, suID)
		if !isCuid(id) {
			return ""
		}
		return id
	}
	trimmed := strings.TrimSpace(suID)
	signedFor := []string{sv.ID.String()}
	if linkSurveyID != sv.ID.String() && legacyid.Matches(linkSurveyID, sv.ID) {
		signedFor = append(signedFor, linkSurveyID)
	}
	for _, id := range signedFor {
		want := SingleUseSignature(string(s.cfg.EncryptionKeyText), id, trimmed)
		if suToken != "" && hmac.Equal([]byte(want), []byte(suToken)) {
			return trimmed
		}
	}
	return ""
}

// openSingleUseID decrypts an encrypted suId, or returns "" when it does not open.
func (s *Service) openSingleUseID(surveyID uuid.UUID, suID string) string {
	if strings.Contains(suID, ":") {
		id, err := secret.DecryptForma2(suID, s.cfg.EncryptionKey, false)
		if err != nil {
			return ""
		}
		return id
	}
	sealed, err := base64.RawURLEncoding.DecodeString(suID)
	if err != nil {
		return ""
	}
	id, err := s.cipher.Open(sealed, singleUseContext(surveyID))
	if err != nil {
		return ""
	}
	return string(id)
}

// isCuid is @paralleldrive/cuid2's isCuid, which forma2 required of a decrypted single-use id: 2 to 32 lowercase letters and digits.
func isCuid(id string) bool {
	return len(id) >= 2 && len(id) <= 32 && surveydoc.IsCUID2(id)
}
