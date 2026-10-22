package clientapi

import (
	"regexp"
	"strings"

	"github.com/yldm-tech/forma/server/internal/response"
)

// A response's meta records the respondent's browser, operating system and device type as forma2 read them with ua-parser-js 2.0.10 (new UAParser(userAgent), no client hints). These are its rules for the browsers, systems and devices that make up nearly all survey traffic, in its order of precedence and with its names; a user agent outside them records no browser or system, and a device without a recognised type is a desktop.

type uaRule struct {
	re *regexp.Regexp
	// name is the fixed name, or "" to take the first capture group as written in the user agent.
	name string
	// unless excludes a match whose user agent also matches it, for the rules ua-parser-js wrote with a negative lookahead.
	unless *regexp.Regexp
}

func rule(pattern, name string) uaRule {
	return uaRule{re: regexp.MustCompile("(?i)" + pattern), name: name}
}

func ruleUnless(pattern, name, unless string) uaRule {
	r := rule(pattern, name)
	r.unless = regexp.MustCompile("(?i)" + unless)
	return r
}

func (r uaRule) match(ua string) (string, bool) {
	m := r.re.FindStringSubmatch(ua)
	if m == nil || (r.unless != nil && r.unless.MatchString(ua)) {
		return "", false
	}
	if r.name != "" {
		return r.name, true
	}
	return m[1], true
}

func firstMatch(rules []uaRule, ua string) string {
	for _, r := range rules {
		if name, ok := r.match(ua); ok {
			return name
		}
	}
	return ""
}

var browserRules = []uaRule{
	rule(`\b(?:crmo|crios)/([\w.]+)`, "Mobile Chrome"),
	rule(`webview.+edge/([\w.]+)`, "Edge WebView"),
	rule(`edg(?:e|ios|a)?/([\w.]+)`, "Edge"),
	rule(`(opera mini)/[-\w.]+`, ""),
	rule(`(opera [mobiletab]{3,6})\b.+version/[-\w.]+`, ""),
	rule(`(opera)(?:.+version/|[/ ]+)[\w.]+`, ""),
	rule(`opios[/ ]+[\w.]+`, "Opera Mini"),
	rule(`\bop(?:rg)?x/[\w.]+`, "Opera GX"),
	rule(`\bopr/[\w.]+`, "Opera"),
	rule(`(?:ms|\()(ie) [\w.]+`, ""),
	rule(`(vivaldi|duckduckgo|puffin|palemoon)/v?[-\w.]+`, ""),
	rule(`(brave)(?: chrome)?/[\d.]+`, ""),
	rule(`\bddg/[\w.]+`, "DuckDuckGo"),
	rule(`(?:\buc? ?browser|(?:juc.+)ucweb| ucpc)[/ ]?[\w.]+`, "UCBrowser"),
	rule(`micromessenger/[\w.]+`, "WeChat"),
	rule(`trident.+rv[: ][\w.]{1,9}\b.+like gecko`, "IE"),
	rule(`ya(?:search)?browser/[\w.]+`, "Yandex"),
	rule(`\bfocus/[\w.]+`, "Firefox Focus"),
	rule(`miuibrowser/[\w.]+`, "MIUI Browser"),
	rule(`fxios/[\w.-]+`, "Mobile Firefox"),
	rule(`samsungbrowser/[\w.]+`, "Samsung Internet"),
	rule(`(?:fban/fbios|fb_iab/fb4a)|;fbav/[\w.]+;`, "Facebook"),
	rule(`(kakao(?:talk|story))[/ ][\w.]+`, ""),
	rule(`safari (line)/[\w.]+`, ""),
	rule(`\b(line)/[\w.]+/iab`, ""),
	rule(`(twitter)(?:and| f.+e/[\w.]+)`, ""),
	rule(`(instagram|snapchat|klarna)[/ ][-\w.]+`, ""),
	rule(`\bgsa/[\w.]+ .*safari/`, "GSA"),
	rule(`(?:musical_ly|trill)(?:.+app_?version/|_)[\w.]+`, "TikTok"),
	rule(`\[(linkedin)app\]`, ""),
	rule(`(chromium)[/ ][-\w.]+`, ""),
	rule(`headlesschrome(?:/[\w.]+| )`, "Chrome Headless"),
	rule(`wv\).+chrome/[\w.]+.+edgw/`, "Edge WebView2"),
	rule(`; wv\).+chrome/[\w.]+`, "Chrome WebView"),
	rule(`droid.+ version/[\w.]+\b.+(?:mobile safari|safari)`, "Android Browser"),
	rule(`chrome/[\w.]+ mobile`, "Mobile Chrome"),
	rule(`(chrome|omniweb|arora|[tizenoka]{5} ?browser)/v?[\w.]+`, ""),
	rule(`version/[\w.,]+ .*mobile(?:/\w+ | ?)safari`, "Mobile Safari"),
	rule(`iphone .*mobile(?:/\w+ | ?)safari`, "Mobile Safari"),
	rule(`version/[\w.,]+ .*(safari)`, ""),
	rule(`webkit.+?(mobile ?safari|safari)/[\w.]+`, ""),
	rule(`(webkit|khtml)/[\w.]+`, ""),
	rule(`(?:mobile|tablet);.*firefox/[\w.-]+`, "Mobile Firefox"),
	rule(`(firefox)/[\w.]+`, ""),
}

var osRules = []uaRule{
	rule(`(windows (?:phone|mobile|iot))`, ""),
	rule(`(windows)[/ ](?:1[01]|2000|3\.1|7|8(?:\.1)?|9[58]|me|server 20\d\d|vista|xp)`, ""),
	ruleUnless(`windows nt`, "Windows", `windows nt.+xbox`),
	rule(`\bwin(?:3| ?9|n)`, "Windows"),
	rule(`[adehimnop]{4,7}\b(?:.*os \w+ like mac|; opera)`, "iOS"),
	rule(`(?:ios;fbsv|ip(?:ad|hone)(?: |.+i(?:pad)?)os|apple ?tv.+ios)[/ ][\w.]+`, "iOS"),
	rule(`\btvos ?[\w.]+`, "iOS"),
	rule(`cfnetwork/.+darwin`, "iOS"),
	rule(`mac os x|macintosh|mac_powerpc\b`, "macOS"),
	rule(`(harmonyos)[/ ]?[\d.]*`, ""),
	rule(`(android|blackberry|kaios|tizen|sailfish)\w*[-/.; ]?[\d.]*`, ""),
	rule(`watch(?: ?os[,/ ]|\d,\d/)[\d.]+`, "watchOS"),
	rule(`cros \w+(?:\)| [\w.]+\b)`, "Chrome OS"),
	rule(`(playstation|nintendo) \w+`, ""),
	rule(`(xbox); +xbox [^);]+`, ""),
	rule(`linux.+(mint)[/() ]?[\w.]*`, ""),
	rule(`([kxln]?ubuntu|debian|fedora|centos|red ?hat|manjaro|opensuse|suse|gentoo|arch)(?: gnu[/ ]linux)?(?: enterprise)?(?:[- ]linux)?(?:-gnu)?`, ""),
	rule(`(linux)(?: (?:arm|x86|ppc)\w*| ?)[\w.]*`, ""),
	rule(`\b([-frentopcghs]{0,5}bsd)`, ""),
}

// deviceRules decide the device type, the first match winning.
var deviceRules = []struct {
	re  *regexp.Regexp
	typ string
	// unless excludes matches ua-parser-js ruled out with a lookahead.
	unless *regexp.Regexp
	// typeFromGroup takes the type from the last capture group: mobile, vr or a digit (a tablet).
	typeFromGroup bool
}{
	{re: regexp.MustCompile(`(?i)\b(sch-i[89]0\d|shw-m380s|sm-[ptx]\w{2,4}|gt-[pn]\d{2,4}|sgh-t8[56]9|nexus 10)`), typ: "tablet"},
	{re: regexp.MustCompile(`(?i)\b(sm-[lr]\d\d[0156][fnuw]?s?|gear live)\b`), typ: "wearable"},
	{re: regexp.MustCompile(`(?i)\b((?:s[cgp]h|gt|sm)-\w+|galaxy nexus)`), typ: "mobile", unless: regexp.MustCompile(`(?i)\b(?:s[cgp]h|gt|sm)-[lr]`)},
	{re: regexp.MustCompile(`(?i)(?:/|\()(ip(?:hone|od)[\w, ]*)[/);]`), typ: "mobile"},
	{re: regexp.MustCompile(`(?i)\b(?:ios|apple\w+)/.+[(/](ipad)`), typ: "tablet"},
	{re: regexp.MustCompile(`(?i)\b(ipad)[\d,]*[;\] ].+(mac |i(pad)?)os`), typ: "tablet"},
	{re: regexp.MustCompile(`(?i)(watch)(?: ?os[,/]|\d,\d/)[\d.]+`), typ: "wearable"},
	{re: regexp.MustCompile(`(?i)(playstation [345portablevi]+)`), typ: "console"},
	{re: regexp.MustCompile(`(?i)\b(xbox(?: one)?)[); ]`), typ: "console"},
	{re: regexp.MustCompile(`(?i)\b(nintendo) (\w+)`), typ: "console"},
	{re: regexp.MustCompile(`(?i)(quest(?: \d| pro)?s?).+vr`), typ: "xr"},
	{re: regexp.MustCompile(`(?i)(tesla)(?: qtcarbrowser|/[-\w.]+)`), typ: "embedded"},
	{re: regexp.MustCompile(`(?i)apple ?tv`), typ: "smarttv"},
	{re: regexp.MustCompile(`(?i)droid.+; ([\w- ]+) (4k|android|smart|google)[- ]?tv`), typ: "smarttv"},
	{re: regexp.MustCompile(`(?i)\b((4k|android|smart|opera)[- ]?tv|tv; rv:|large screen[\w ]+safari)\b`), typ: "smarttv"},
	{re: regexp.MustCompile(`(?i)droid .+?; ([^;]+?)(?: bui|; wv\)|\) applew|; hmsc).+?(mobile|vr|\d) safari`), typeFromGroup: true},
	{re: regexp.MustCompile(`(?i)\b((tablet|tab)[;/])`), typ: "tablet"},
	{re: regexp.MustCompile(`(?i)\bfocus/\d`), typ: "tablet", unless: regexp.MustCompile(`(?i)\bfocus/\d.+mobile`)},
	{re: regexp.MustCompile(`(?i)(phone|mobile(?:[;/]| [ \w/.]*safari))`), typ: "mobile"},
}

func deviceType(ua string) string {
	for _, r := range deviceRules {
		m := r.re.FindStringSubmatch(ua)
		if m == nil || (r.unless != nil && r.unless.MatchString(ua)) {
			continue
		}
		if !r.typeFromGroup {
			return r.typ
		}
		switch strings.ToLower(m[len(m)-1]) {
		case "mobile":
			return "mobile"
		case "vr":
			return "xr"
		}
		return "tablet"
	}
	return ""
}

// parseUserAgent reads a User-Agent header as forma2's response routes did.
func parseUserAgent(ua string) response.UserAgent {
	if ua == "" {
		return response.UserAgent{}
	}
	return response.UserAgent{Browser: firstMatch(browserRules, ua), OS: firstMatch(osRules, ua), Device: deviceType(ua)}
}
