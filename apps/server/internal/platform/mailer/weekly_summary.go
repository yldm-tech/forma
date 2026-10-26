package mailer

import (
	"math"
	"strconv"
	"strings"
)

// WeeklySummary is the weekly summary email a member gets every Monday for one organization: the surveys they can see that had responses in the week or the week before, each with its responses, completion rate, NPS and CSAT against the week before, and top tags. PeriodStart and PeriodEnd are the week's first and last day (both inclusive) in TimeZone, the organization's display time zone. Surveys holds the busiest surveys; MoreSurveys counts the others that had responses. The URLs are absolute.
type WeeklySummary struct {
	OrganizationName  string
	PeriodStart       string
	PeriodEnd         string
	TimeZone          string
	Responses         int
	PreviousResponses int
	Surveys           []WeeklySummarySurvey
	MoreSurveys       int
	SettingsURL       string
	LogoURL           string
}

// WeeklySummarySurvey is one survey of the weekly summary. SummaryURL opens its summary filtered to the week.
type WeeklySummarySurvey struct {
	Name              string
	WorkspaceName     string
	SummaryURL        string
	Responses         int
	PreviousResponses int
	Completed         int
	// NPS and CSAT are the survey's first NPS and CSAT element's score; nil when it has no such element.
	NPS  *WeeklySummaryScore
	CSAT *WeeklySummaryScore
	Tags []WeeklySummaryTag
}

// WeeklySummaryScore is a score in the week and in the week before; nil when nobody answered with a number then. NPS runs from -100 to 100, CSAT is the share of satisfied answers in percent.
type WeeklySummaryScore struct {
	Current  *float64
	Previous *float64
}

// WeeklySummaryTag is one of the tags most put on the survey's responses of the week.
type WeeklySummaryTag struct {
	Name  string
	Count int
}

func (WeeklySummary) template() string   { return "weekly_summary" }
func (WeeklySummary) subjectKey() string { return "emails.weekly_summary_email_subject" }
func (c WeeklySummary) logo() string     { return c.LogoURL }
func (WeeklySummary) unsigned()          {}

// The subject names the organization, whose weekly summary a member of several organizations gets once each.
func (c WeeklySummary) subject(t func(string, ...any) string) string {
	return t(c.subjectKey(), "organizationName", c.OrganizationName)
}

// CompletionRate is the share of the week's responses that are completed, "–" without responses.
func (s WeeklySummarySurvey) CompletionRate() string {
	if s.Responses == 0 {
		return noValue
	}
	return formatNumber(float64(s.Completed)/float64(s.Responses)*100) + "%"
}

// TagList is the top tags with their counts, "billing (3), ui (2)".
func (s WeeklySummarySurvey) TagList() string {
	parts := make([]string, len(s.Tags))
	for i, tag := range s.Tags {
		parts[i] = tag.Name + " (" + strconv.Itoa(tag.Count) + ")"
	}
	return strings.Join(parts, ", ")
}

// NPSText and PreviousNPSText are the NPS of the week and of the week before, "–" when nobody answered.
func (s WeeklySummaryScore) NPSText() string         { return formatScore(s.Current, "") }
func (s WeeklySummaryScore) PreviousNPSText() string { return formatScore(s.Previous, "") }

// CSATText and PreviousCSATText are the CSAT share of the week and of the week before, "–" when nobody answered.
func (s WeeklySummaryScore) CSATText() string         { return formatScore(s.Current, "%") }
func (s WeeklySummaryScore) PreviousCSATText() string { return formatScore(s.Previous, "%") }

// noValue stands for a number there is nothing to compute from.
const noValue = "–"

func formatScore(v *float64, unit string) string {
	if v == nil {
		return noValue
	}
	return formatNumber(*v) + unit
}

// formatNumber rounds to one decimal and drops a trailing ".0": 35, 82.5, -12.
func formatNumber(v float64) string {
	r := math.Round(v*10) / 10
	if r == 0 {
		r = 0 // no "-0"
	}
	return strconv.FormatFloat(r, 'f', -1, 64)
}
