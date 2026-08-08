package app

import (
	"strings"
	"testing"
)

func TestMarkdownRenderIsSafeAndHighlightsCode(t *testing.T) {
	r := NewMarkdownRenderer()
	html, err := r.Render("<script>alert('x')</script>\n\n```go\nfmt.Println(\"ok\")\n```")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "<script>") {
		t.Fatalf("raw HTML was rendered: %s", html)
	}
	if !strings.Contains(html, "style=") || !strings.Contains(html, "fmt") {
		t.Fatalf("code was not highlighted: %s", html)
	}
}

func TestSlugifyAndUniqueSlug(t *testing.T) {
	if got := Slugify(" Go 与 Web / 入门！ "); got != "go-与-web-入门" {
		t.Fatalf("unexpected slug: %q", got)
	}
	existing := map[string]bool{"文章": true, "文章-2": true}
	if got := UniqueSlug("文章", func(v string) bool { return existing[v] }); got != "文章-3" {
		t.Fatalf("unexpected unique slug: %q", got)
	}
}
