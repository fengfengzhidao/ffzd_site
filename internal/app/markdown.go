package app

import (
	"bytes"
	"fmt"
	stdhtml "html"
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
var htmlTag = regexp.MustCompile(`<[^>]+>`)

func PlainTextSummary(rendered string, limit int) string {
	if limit <= 0 {
		return ""
	}
	text := stdhtml.UnescapeString(htmlTag.ReplaceAllString(rendered, " "))
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}

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

// KeywordSlug returns the URL-safe form of the first article keyword. Hyphens
// and whitespace are intentionally removed so a keyword such as "gpt-5.6"
// is reachable at /posts/gpt5.6, while dots remain meaningful version markers.
func KeywordSlug(keywords string) string {
	first := strings.FieldsFunc(keywords, func(r rune) bool {
		return r == ',' || r == '，' || r == ';' || r == '；' || r == '\n' || r == '\r'
	})
	if len(first) == 0 {
		return ""
	}
	keyword := strings.TrimSpace(strings.ToLower(first[0]))
	var b strings.Builder
	for _, r := range keyword {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' {
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), ".")
}

// PublicPostPath keeps keyword-based paths for older articles while preserving
// a stored numeric suffix when two articles share the same first keyword.
func PublicPostPath(p Post) string {
	keywordPath := KeywordSlug(p.Keywords)
	if keywordPath == "" {
		return p.Slug
	}
	if p.Slug == keywordPath || strings.HasPrefix(p.Slug, keywordPath+"-") {
		return p.Slug
	}
	return keywordPath
}

func UniqueKeywordSlug(base string, exists func(string) bool) string {
	if base == "" {
		return ""
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
