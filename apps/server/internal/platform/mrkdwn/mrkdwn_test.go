package mrkdwn

import (
	"strings"
	"testing"
)

const z = zeroWidthSpace

func TestEscape(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, in, want string }{
		{"plain text", "Great product, 10/10", "Great product, 10/10"},
		{"empty", "", ""},
		{"channel and here notifications", "<!channel> <!here> <!everyone>", "&lt;!channel" + z + "&gt; &lt;!here" + z + "&gt; &lt;!everyone" + z + "&gt;"},
		{"user, channel and group mentions", "<@U123> <#C123|general> <!subteam^S123>", "&lt;@U123" + z + "&gt; &lt;#C123|general" + z + "&gt; &lt;!subteam^S123" + z + "&gt;"},
		{"disguised link", "<https://evil.example/login|Your Forma session expired>", "&lt;https://evil.example/login|Your Forma session expired" + z + "&gt;"},
		{"bare broadcast words", "@channel @Here @EVERYONE hi", "@" + z + "channel @" + z + "Here @" + z + "EVERYONE hi"},
		{"an entity typed as text", "&lt;!channel&gt; &amp;", "&amp;lt;!channel&amp;gt; &amp;amp;"},
		{"bold, italics, strikethrough and code", "*urgent* _now_ ~old~ `code`", z + "*" + z + "urgent" + z + "*" + z + " " + z + "_" + z + "now" + z + "_" + z + " " + z + "~" + z + "old" + z + "~" + z + " " + z + "`" + z + "code" + z + "`" + z},
		{"code block", "```x```", strings.Repeat(z+"`"+z, 3) + "x" + strings.Repeat(z+"`"+z, 3)},
		{"quote", "> Forma security team\n> reset here", z + "&gt; Forma security team\n" + z + "&gt; reset here"},
		{"between letters or digits", "first_last@example.com snake_case 2*3 https://example.com/a_b", "first_last@example.com snake_case 2*3 https://example.com/a_b"},
		{"at the edges", "_x x_", z + "_" + z + "x x" + z + "_" + z},
		{"non-Latin words", "日本_語", "日本_語"},
		{"URL with _ after / and =", "https://drive.google.com/file/d/1aB-_xYz/view https://www.youtube.com/watch?v=_kZ3abc", "https://drive.google.com/file/d/1aB-_xYz/view https://www.youtube.com/watch?v=_kZ3abc"},
		{"URL with ~", "https://example.com/~bob/cv.pdf", "https://example.com/~bob/cv.pdf"},
		{"upload URL with a camera file name", "https://forma.example/files/elements/e/_DSC1234--fid--0b5c.jpg", "https://forma.example/files/elements/e/_DSC1234--fid--0b5c.jpg"},
		{"URL in a sentence", "see *https://example.com/_a_* now", "see " + z + "*" + z + "https://example.com/_a_* now"},
		{"URL with entities", "https://example.com/?a=_1&b=<2>", "https://example.com/?a=_1&amp;b=&lt;2" + z + "&gt;"},
		{"email with _ at the start", "_jane@example.com", "_jane@example.com"},
		{"emails whose domain starts with a broadcast word", "ops@channelpartners.com jane@hereford.ac.uk bob@everyone.org", "ops@channelpartners.com jane@hereford.ac.uk bob@everyone.org"},
		{"handles that start with a broadcast word", "@channelpartners @heresy", "@channelpartners @heresy"},
		{"broadcast words next to punctuation", "(@here), @channel. _@everyone_ @here@here", "(@" + z + "here), @" + z + "channel. " + z + "_" + z + "@" + z + "everyone" + z + "_" + z + " @" + z + "here@here"},
	}
	for _, c := range cases {
		if got := Escape(c.in); got != c.want {
			t.Errorf("%s: Escape(%q)\n got %q\nwant %q", c.name, c.in, got, c.want)
		}
	}
}

// Whatever a respondent types, the escaped text holds no character mrkdwn reads as the start of a mention, a link or an entity of its own, and no formatting character next to anything but a zero-width space or a letter or digit.
// A workflow template that wraps an answer in link syntax gets the answer's URL as the link's target, unchanged.
func TestEscapeKeepsLinkTargets(t *testing.T) {
	t.Parallel()
	for _, url := range []string{"https://www.youtube.com/watch?v=_kZ3abc", "https://example.com/~bob/cv.pdf", "https://forma.example/files/e/_DSC1234.jpg"} {
		if got := "<" + Escape(url) + "|Open>"; got != "<"+url+"|Open>" {
			t.Errorf("link target for %q: got %q", url, got)
		}
	}
}

func TestEscapeLeavesNoMarkup(t *testing.T) {
	t.Parallel()
	hostile := "<!channel> *<https://evil.example|click>* _@here_ ~`>`~ &lt;"
	got := Escape(hostile)
	if strings.ContainsAny(got, "<>") {
		t.Fatalf("Escape kept < or >: %q", got)
	}
	if strings.Contains(strings.ToLower(got), "@here") {
		t.Fatalf("Escape kept @here: %q", got)
	}
	if !strings.Contains(got, "&lt;!") {
		t.Fatalf("Escape lost the text: %q", got)
	}
	runes := []rune(got)
	for i, r := range runes {
		if !strings.ContainsRune("*_~`", r) {
			continue
		}
		for _, n := range []int{i - 1, i + 1} {
			if n < 0 || n >= len(runes) || string(runes[n]) != z {
				t.Fatalf("formatting character %q at %d is not fenced: %q", r, i, got)
			}
		}
	}
}
