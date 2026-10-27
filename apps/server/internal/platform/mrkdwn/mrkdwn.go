// Package mrkdwn makes text that someone outside the workspace typed (an answer, a hidden field, a contact attribute, metadata) safe to place inside a message written in Slack's mrkdwn, so that Slack shows it as typed: it cannot notify a channel, mention a person or a group, forge a link whose label hides its target, or take on formatting. Every place Forma posts such text to Slack escapes it with Escape: the workflow's Slack message action and the Slack integration.
package mrkdwn

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/slack-go/slack/slackutilsx"
)

// zeroWidthSpace separates a character from its neighbours without showing anything.
const zeroWidthSpace = "\u200b"

// broadcast matches the words Slack can turn into an @here, @channel or @everyone notification without the <!here> syntax. fenceBroadcasts checks the neighbours, since RE2 has no lookaround.
var broadcast = regexp.MustCompile(`(?i)@(?:here|channel|everyone)`)

// link matches the tokens Slack autolinks: a URL (a scheme followed by :// and everything up to whitespace, < or >) and an email address. Slack shows such a token as a link and applies no formatting inside it.
var link = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://[^\s<>]+|[\p{L}\p{N}._%+~-]+@[\p{L}\p{N}-]+(?:\.[\p{L}\p{N}-]+)+`)

// Escape returns s as literal mrkdwn text. Only s is escaped: markup around it, such as a workflow template's own formatting, is left to the caller.
//
//   - &, < and > become entities, the only escapes mrkdwn has, so <!channel>, <!here>, <@U123>, <#C123>, <!subteam^S123> and <https://example.com|label> show as typed instead of notifying, mentioning or linking.
//   - @here, @channel and @everyone standing as words of their own get a zero-width space after the @, so Slack cannot expand them into a notification either. After a letter or digit (an email address such as jane@hereford.ac.uk) or before one (a handle such as @channelpartners) they are no notification, so they stay as they are.
//   - *, _, ~ and ` get a zero-width space on each side, because mrkdwn cannot escape them, so they open and close no bold, italics, strikethrough or code. Between two letters or digits mrkdwn never reads them as markup, and inside a URL or an email address Slack shows a link without formatting, so there they stay as they are: snake_case, _jane@example.com and https://example.com/~bob/_DSC1.jpg keep their characters, and a link's target is not changed.
//   - > gets a zero-width space before it, so it cannot start a quote.
func Escape(s string) string {
	s = fenceBroadcasts(s)
	inLink := make([]bool, len(s))
	for _, m := range link.FindAllStringIndex(s, -1) {
		for i := m[0]; i < m[1]; i++ {
			inLink[i] = true
		}
	}
	type char struct {
		r    rune
		link bool
	}
	chars := make([]char, 0, len(s))
	for i, r := range s {
		chars = append(chars, char{r, inLink[i]})
	}
	var b strings.Builder
	b.Grow(len(s))
	for i, c := range chars {
		switch c.r {
		case '*', '_', '~', '`':
			if c.link || i > 0 && i < len(chars)-1 && wordRune(chars[i-1].r) && wordRune(chars[i+1].r) {
				b.WriteRune(c.r)
				continue
			}
			b.WriteString(zeroWidthSpace)
			b.WriteRune(c.r)
			b.WriteString(zeroWidthSpace)
		case '>':
			b.WriteString(zeroWidthSpace)
			b.WriteRune(c.r)
		default:
			b.WriteRune(c.r)
		}
	}
	return slackutilsx.EscapeMessage(b.String())
}

// fenceBroadcasts puts a zero-width space after the @ of every broadcast word that is not preceded or followed by a letter or digit.
func fenceBroadcasts(s string) string {
	var b strings.Builder
	last := 0
	for _, m := range broadcast.FindAllStringIndex(s, -1) {
		before, _ := utf8.DecodeLastRuneInString(s[:m[0]])
		after, _ := utf8.DecodeRuneInString(s[m[1]:])
		if m[0] > 0 && wordRune(before) || m[1] < len(s) && wordRune(after) {
			continue
		}
		b.WriteString(s[last : m[0]+1])
		b.WriteString(zeroWidthSpace)
		last = m[0] + 1
	}
	b.WriteString(s[last:])
	return b.String()
}

func wordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
