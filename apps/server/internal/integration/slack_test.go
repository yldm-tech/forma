package integration

import (
	"strings"
	"testing"
	"unicode/utf16"
)

// slackText cuts like truncate, but after escaping: an answer whose escapes would take it past Slack's section limit is cut earlier, and a plain one exactly where truncate cuts it.
func TestSlackTextFitsTheSectionLimit(t *testing.T) {
	t.Parallel()
	plain := strings.Repeat("a", slackMessageLimit+10)
	if got := slackText(plain); got != truncate(plain, slackMessageLimit) {
		t.Errorf("plain text cut at %d units, want truncate's %d", utf16Len(got), utf16Len(truncate(plain, slackMessageLimit)))
	}
	short := "<b>"
	if got := slackText(short); got != "&lt;b\u200b&gt;" {
		t.Errorf("short text = %q", got)
	}
	for _, hostile := range []string{strings.Repeat("*", 2000), strings.Repeat("<!channel>", 400), strings.Repeat("&>", 1000)} {
		got := slackText(hostile)
		if n := len(utf16.Encode([]rune(got))); n > slackMessageLimit+len("...") || !strings.HasSuffix(got, "...") {
			t.Errorf("escaped %q... is %d units", hostile[:10], n)
		}
		if strings.ContainsAny(strings.TrimSuffix(got, "..."), "<>") || strings.HasSuffix(strings.TrimSuffix(got, "..."), "&") {
			t.Errorf("cut inside an escape: %q", got[len(got)-20:])
		}
	}
}
