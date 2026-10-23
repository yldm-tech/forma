package surveydoc

import (
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// trimJS is JavaScript's String.prototype.trim: Unicode white space and line terminators, the byte order mark included.
func trimJS(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return unicode.IsSpace(r) || r == '\ufeff' })
}

func parseFragment(s string) []*html.Node {
	nodes, err := html.ParseFragment(strings.NewReader(s), &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body})
	if err != nil {
		return nil
	}
	return nodes
}

// IsValidHTML is forma2's isValidHTML: the string contains at least one element.
func IsValidHTML(s string) bool {
	if s == "" {
		return false
	}
	for _, n := range parseFragment(s) {
		if n.Type == html.ElementNode {
			return true
		}
	}
	return false
}

// TextContent is forma2's getTextContent: the text of an HTML string with its markup removed (a <br> becomes a newline), or the plain string itself, trimmed. Entities are decoded only in markup, as forma2's parser did.
func TextContent(s string) string {
	if trimJS(s) == "" {
		return ""
	}
	if !IsValidHTML(s) {
		return trimJS(s)
	}
	var b strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			b.WriteString(n.Data)
		case html.ElementNode:
			if n.DataAtom == atom.Br {
				b.WriteString("\n")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for _, n := range parseFragment(s) {
		walk(n)
	}
	return trimJS(b.String())
}
