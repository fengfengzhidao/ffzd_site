package app

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

type MarkdownRenderer struct{ md goldmark.Markdown }

func NewMarkdownRenderer() *MarkdownRenderer {
	return &MarkdownRenderer{md: goldmark.New(
		goldmark.WithExtensions(extension.GFM, highlighting.NewHighlighting(highlighting.WithStyle("github"))),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	)}
}

func (r *MarkdownRenderer) Render(source string) (string, error) {
	var b bytes.Buffer
	if err := r.md.Convert([]byte(source), &b); err != nil {
		return "", err
	}
	return b.String(), nil
}

var multiDash = regexp.MustCompile(`-+`)

func Slugify(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	var b strings.Builder
	dash := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
			continue
		}
		if (unicode.IsSpace(r) || strings.ContainsRune("-_./", r)) && b.Len() > 0 && !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(multiDash.ReplaceAllString(b.String(), "-"), "-")
}

func UniqueSlug(base string, exists func(string) bool) string {
	base = Slugify(base)
	if base == "" {
		base = "post"
	}
	if !exists(base) {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !exists(candidate) {
			return candidate
		}
	}
}
