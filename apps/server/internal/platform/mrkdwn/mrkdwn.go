// Package mrkdwn makes text that someone outside the workspace typed (an answer, a hidden field, a contact attribute, metadata) safe to place inside a message written in Slack's mrkdwn, so that Slack shows it as typed: it cannot notify a channel, mention a person or a group, forge a link whose label hides its target, or take on formatting. Every place Forma posts such text to Slack escapes it with Escape: the workflow's Slack message action and the Slack integration.
package mrkdwn

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/slack-go/slack/slackutilsx"
)

// zeroWidthSpace separates a character from its neighbours without showing anything.
const zeroWidthSpace = "\u200b"

// broadcast matches the words Slack can turn into an @here, @channel or @everyone notification without the <!here> syntax.
var broadcast = regexp.MustCompile(`(?i)@(here|channel|everyone)`)

// Escape returns s as literal mrkdwn text. Only s is escaped: markup around it, such as a workflow template's own formatting, is left to the caller.
//
//   - &, < and > become entities, the only escapes mrkdwn has, so <!channel>, <!here>, <@U123>, <#C123>, <!subteam^S123> and <https://example.com|label> show as typed instead of notifying, mentioning or linking.
//   - @here, @channel and @everyone get a zero-width space after the @, so Slack cannot expand them into a notification either.
//   - *, _, ~ and ` get a zero-width space on each side, because mrkdwn cannot escape them, so they open and close no bold, italics, strikethrough or code. Between two letters or digits mrkdwn never reads them as markup, so there they stay as they are: snake_case, first_last@example.com and a URL's path keep their characters.
//   - > gets a zero-width space before it, so it cannot start a quote.
func Escape(s string) string {
	runes := []rune(broadcast.ReplaceAllString(s, "@"+zeroWidthSpace+"${1}"))
	var b strings.Builder
	b.Grow(len(s))
	for i, r := range runes {
		switch r {
		case '*', '_', '~', '`':
			if i > 0 && i < len(runes)-1 && wordRune(runes[i-1]) && wordRune(runes[i+1]) {
				b.WriteRune(r)
				continue
			}
			b.WriteString(zeroWidthSpace)
			b.WriteRune(r)
			b.WriteString(zeroWidthSpace)
		case '>':
			b.WriteString(zeroWidthSpace)
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return slackutilsx.EscapeMessage(b.String())
}

func wordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
