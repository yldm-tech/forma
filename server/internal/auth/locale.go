package auth

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/yldm-tech/forma/server/internal/platform/mailer"
)

// localeParts are the subtags of a BCP 47 tag that matching cares about, lowercased.
type localeParts struct {
	language, script, region string
}

var (
	scriptSubtag = regexp.MustCompile(`^[a-z]{4}$`)
	regionSubtag = regexp.MustCompile(`^([a-z]{2}|[0-9]{3})$`)
	qualityParam = regexp.MustCompile(`(?i)^\s*q\s*=\s*([0-9.]+)\s*$`)
)

// traditionalChineseRegions are written in Traditional Han; every other Chinese region is Simplified.
var traditionalChineseRegions = map[string]bool{"tw": true, "hk": true, "mo": true}

// parseLocaleTag is forma2's parseLocaleTag: a Chinese tag with a region but no script, which is what browsers send, gets the script its region implies.
func parseLocaleTag(tag string) localeParts {
	subtags := strings.Split(strings.ToLower(tag), "-")
	p := localeParts{language: subtags[0]}
	for _, s := range subtags[1:] {
		if p.script == "" && scriptSubtag.MatchString(s) {
			p.script = s
		} else if p.region == "" && regionSubtag.MatchString(s) {
			p.region = s
		}
	}
	if p.script == "" && p.language == "zh" && p.region != "" {
		p.script = "hans"
		if traditionalChineseRegions[p.region] {
			p.script = "hant"
		}
	}
	return p
}

// scoreLocale is how well an available locale serves a requested one; 0 is no match. Script outranks region, so zh-HK reaches the Traditional catalogue.
func scoreLocale(available, requested localeParts) int {
	switch {
	case available.language != requested.language:
		return 0
	case available.script != "" && requested.script != "" && available.script != requested.script:
		return 0
	case available.region == requested.region && available.script == requested.script:
		return 4
	case available.script != "" && available.script == requested.script:
		return 3
	case available.region != "" && available.region == requested.region:
		return 2
	}
	return 1
}

// matchLocale is forma2's findMatchingLocale, which gave a new SSO user the best of the shipped locales for their browser's Accept-Language: requested tags are tried best weighted first, ties in header order, and the first with any match wins.
func matchLocale(acceptLanguage string) string {
	type weighted struct {
		parts   localeParts
		quality float64
	}
	var requested []weighted
	for _, entry := range strings.Split(acceptLanguage, ",") {
		tag, params, _ := strings.Cut(entry, ";")
		tag = strings.TrimSpace(tag)
		quality := 1.0
		for _, param := range strings.Split(params, ";") {
			if m := qualityParam.FindStringSubmatch(param); m != nil {
				q, err := strconv.ParseFloat(m[1], 64)
				if err != nil {
					q = -1
				}
				quality = q
				break
			}
		}
		if tag == "" || tag == "*" || quality <= 0 {
			continue
		}
		requested = append(requested, weighted{parts: parseLocaleTag(tag), quality: quality})
	}
	sort.SliceStable(requested, func(i, j int) bool { return requested[i].quality > requested[j].quality })
	for _, r := range requested {
		best, bestScore := "", 0
		for _, locale := range mailer.Locales {
			if score := scoreLocale(parseLocaleTag(locale), r.parts); score > bestScore {
				best, bestScore = locale, score
			}
		}
		if best != "" {
			return best
		}
	}
	return defaultLocale
}
