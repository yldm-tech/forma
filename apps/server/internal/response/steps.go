package response

import (
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/apps/server/internal/survey"
)

// maxLoggedKeys caps the keys one log line carries: the key list is attacker-controlled on a public endpoint.
const maxLoggedKeys = 20

func capped[T any](entries []T) map[string]any {
	n := min(len(entries), maxLoggedKeys)
	return map[string]any{"entries": entries[:n], "omitted": len(entries) - n}
}

// verifiedEmailKey is forma2's VERIFIED_EMAIL_RESPONSE_KEY, the answer the verified-email gate stamps from its token.
const verifiedEmailKey = "verifiedEmail"

// Ingest is forma2's applyIngestContractToResponseData, the Embedded Data ingest contract every client create and update runs before validation, quotas and the verified-email gate: question answers pass, the survey's ingested fields are stored under their declared keys, everything else is dropped. Drops and flags are only logged; they never fail a request. The allow-list is the survey's stored fields alone, so it fails closed.
func (s *Service) Ingest(sv *Survey, data []jsvalue.Member) embeddeddata.IngestResult {
	ingested := embeddeddata.OfSource(sv.Fields, embeddeddata.SourceIngested)
	result := embeddeddata.Ingest(data, sv.Fields, sv.ElementIDs())
	if len(result.Dropped) > 0 || len(result.Flags) > 0 {
		s.log.Info("Embedded Data ingest dropped or flagged incoming keys", "survey_id", sv.ID, "dropped", capped(result.Dropped), "flags", capped(result.Flags))
	}
	unknown := false
	for _, d := range result.Dropped {
		unknown = unknown || d.Reason == embeddeddata.DropUnknownKey
	}
	// Louder on purpose: a survey that declares hidden fields but has no ingested field rows drops every value, which reads like one typo'd parameter at info level.
	if legacy := len(sv.HiddenFields.FieldIDs); legacy > 0 && len(ingested) == 0 && unknown {
		s.log.Warn("Embedded Data ingest resolved no ingested fields for a survey that declares hidden fields, so every incoming value was dropped",
			"survey_id", sv.ID, "legacy_field_id_count", legacy, "embedded_field_count", len(sv.Fields), "dropped", capped(result.Dropped))
	}
	// The legacy hiddenFields.enabled flag is not an ingest gate; logged so it is greppable if a customer ever relies on it.
	if !sv.HiddenFields.Enabled {
		var keys []string
		for _, f := range ingested {
			if _, ok := result.Data[f.StorageKey]; ok {
				keys = append(keys, f.StorageKey)
			}
		}
		if len(keys) > 0 {
			s.log.Info("Embedded Data ingested into a survey whose legacy hiddenFields.enabled flag is false", "survey_id", sv.ID, "keys", capped(keys))
		}
	}
	return result
}

// IngestUpdate is the update's ingest: with email verification on, a client-sent verifiedEmail is dropped first, since only the create path's token check may write it, then the contract runs.
func (s *Service) IngestUpdate(sv *Survey, data []jsvalue.Member) embeddeddata.IngestResult {
	if sv.IsVerifyEmailEnabled {
		kept := data[:0:0]
		for _, m := range data {
			if m.Key != verifiedEmailKey {
				kept = append(kept, m)
			}
		}
		data = kept
	}
	return s.Ingest(sv, data)
}

// SingleUseID is forma2's single-use check of a new response to a link survey with single-use links on: the response must name the id its page link (meta.url's suId and suToken) stands for, and the canonical id replaces the one sent. A survey without single-use links passes, with the id the client sent. v2 differs from v1 only in the details it reports: the workspace as workspaceId rather than environmentId. Both refuse with 400.
func (s *Service) SingleUseID(sv *Survey, workspaceID uuid.UUID, v2 bool, singleUseID, metaURL *string) (*string, error) {
	if sv.Type != survey.TypeLink || !sv.SingleUse.Enabled {
		return singleUseID, nil
	}
	workspaceKey := "environmentId"
	if v2 {
		workspaceKey = "workspaceId"
	}
	refuse := func(message string, extra ...any) error {
		return badRequest(message, details(append([]any{"surveyId", sv.ID.String(), workspaceKey, workspaceID.String()}, extra...)...))
	}
	if singleUseID == nil || *singleUseID == "" {
		return nil, refuse("Missing single use id")
	}
	if metaURL == nil || *metaURL == "" {
		return nil, refuse("Missing or invalid URL in response metadata")
	}
	u, ok := parseAbsoluteURL(*metaURL)
	if !ok {
		return nil, refuse("Invalid URL in response metadata", "error", "Invalid URL")
	}
	query := u.Query()
	suID := query.Get("suId")
	if suID == "" {
		return nil, refuse("Missing single use id")
	}
	canonical := survey.CanonicalSingleUseID(s.cfg, s.cipher, sv.ID, sv.SingleUse.IsEncrypted, suID, query.Get("suToken"))
	if canonical == "" || canonical != *singleUseID {
		return nil, refuse("Invalid single use id")
	}
	return &canonical, nil
}

// parseAbsoluteURL is `new URL(s)` succeeding: an absolute URL, with a host when its scheme is one of the WHATWG special schemes.
func parseAbsoluteURL(s string) (*url.URL, bool) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u.Scheme == "" {
		return nil, false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "ws", "wss", "ftp":
		if u.Host == "" {
			return nil, false
		}
	}
	return u, true
}
