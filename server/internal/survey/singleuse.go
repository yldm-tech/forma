package survey

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/platform/apperr"
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

// cuid2Shape is zod's cuid2 check, what forma2's isCuid required of a decrypted single-use id.
var cuid2Shape = regexp.MustCompile(`^[0-9a-z]+$`)

// CanonicalSingleUseID is forma2's validateSurveySingleUseLinkParams: the single-use id a link's suId and suToken stand for, or false when they do not open. suId is trimmed. On an encrypted link it opens as SingleUseLinks sealed it (base64url, bound to the survey) or as forma2 encrypted it (symmetricEncrypt under ENCRYPTION_KEY), and must hold an id of cuid2's shape; on a plain link suToken must be its signature, compared in constant time.
func CanonicalSingleUseID(cipher *secret.Cipher, key []byte, surveyID uuid.UUID, isEncrypted bool, suID, suToken string) (string, bool) {
	suID = strings.TrimSpace(suID)
	if suID == "" {
		return "", false
	}
	if !isEncrypted {
		if suToken == "" || !hmac.Equal([]byte(SingleUseSignature(key, surveyID, suID)), []byte(suToken)) {
			return "", false
		}
		return suID, true
	}
	id := ""
	if sealed, err := base64.RawURLEncoding.DecodeString(suID); err == nil {
		if plain, err := cipher.Open(sealed, singleUseContext(surveyID)); err == nil {
			id = string(plain)
		}
	}
	if id == "" {
		plain, err := secret.DecryptForma2(suID, key, false)
		if err != nil {
			return "", false
		}
		id = plain
	}
	if len(id) < 2 || len(id) > 32 || !cuid2Shape.MatchString(id) {
		return "", false
	}
	return id, true
}
