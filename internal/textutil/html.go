// Package textutil converts Teams message bodies into plain text.
package textutil

import (
	"html"
	"regexp"
	"strings"
)

var (
	blockTagRe   = regexp.MustCompile(`(?i)<\s*(br|/p|/div|/li|/tr|/h[1-6]|/blockquote|/pre)\b[^>]*>`)
	listItemRe   = regexp.MustCompile(`(?i)<\s*li\b[^>]*>`)
	scriptLikeRe = regexp.MustCompile(`(?is)<\s*(script|style)\b.*?<\s*/\s*(script|style)\s*>`)
	tagRe        = regexp.MustCompile(`(?s)<[^>]*>`)
	spaceRe      = regexp.MustCompile(`[ \t\f\v\x{00a0}]+`)
	blankLinesRe = regexp.MustCompile(`\n{3,}`)
)

// HTMLToText strips HTML markup from s, preserving line breaks for block
// elements and decoding HTML entities.
func HTMLToText(s string) string {
	if s == "" {
		return ""
	}
	s = scriptLikeRe.ReplaceAllString(s, "")
	s = blockTagRe.ReplaceAllString(s, "\n")
	s = listItemRe.ReplaceAllString(s, "- ")
	s = tagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")

	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(spaceRe.ReplaceAllString(line, " "))
	}
	s = strings.Join(lines, "\n")
	s = blankLinesRe.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// BodyToText returns the plain-text form of a message body with the given
// Graph contentType ("html" or "text").
func BodyToText(contentType, content string) string {
	if strings.EqualFold(contentType, "html") {
		return HTMLToText(content)
	}
	return strings.TrimSpace(content)
}
