// Package contactsurvey is what a contact has to do with surveys: the activity on the contact page (forma2's ActivitySection, ActivityTimeline and the response and display counts of AttributesSection) and personal links, a contact's own link to a link survey, issued one at a time from the contact page (generatePersonalSurveyLinkAction) or for every contact of a segment as a CSV from the share modal (generatePersonalLinksAction). The management API's contact links (mgmtapi) build their links with Link too, so every surface issues the same link.
package contactsurvey

import (
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/linksurvey"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/server/internal/survey"
)

// Link is forma2's getContactSurveyLink: the contact's personal link to the survey on the public domain, a token signed at now that expires after expiresIn (never when it is not positive), with fresh single-use parameters when the survey takes single-use links.
func Link(cfg config.Config, surveys *survey.Service, sv *survey.Survey, contactID uuid.UUID, now time.Time, expiresIn time.Duration) (string, error) {
	token, err := linksurvey.NewContactToken(cfg, contactID, sv.ID, now, expiresIn)
	if err != nil {
		return "", err
	}
	out := surveys.PublicDomain() + "/c/" + token
	if settings := sv.SingleUseSettings(); settings.Enabled {
		su, err := surveys.NewSingleUseLink(sv.ID, settings.IsEncrypted)
		if err != nil {
			return "", err
		}
		q := url.Values{"suId": {su.SuID}}
		if su.SuToken != "" {
			q.Set("suToken", su.SuToken)
		}
		out += "?" + q.Encode()
	}
	return out, nil
}

// Days is a number of days as a token lifetime.
func Days(days int) time.Duration {
	return time.Duration(days) * 24 * time.Hour
}

// ExpiresAt is forma2's calculateExpirationDate: now plus the days as an ISO string, nil without them.
func ExpiresAt(now time.Time, days int) *string {
	if days == 0 {
		return nil
	}
	s := jsvalue.ISOString(now.Add(Days(days)))
	return &s
}
