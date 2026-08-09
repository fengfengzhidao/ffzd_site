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

func TestKeywordSlug(t *testing.T) {
	if got := KeywordSlug("gpt-5.6, Codex"); got != "gpt5.6" {
		t.Fatalf("unexpected keyword slug: %q", got)
	}
	if got := KeywordSlug("gpt-5.6，Codex"); got != "gpt5.6" {
		t.Fatalf("unexpected Chinese-separated keyword slug: %q", got)
	}
	if got := UniqueKeywordSlug("gpt5.6", func(v string) bool { return v == "gpt5.6" }); got != "gpt5.6-2" {
		t.Fatalf("unexpected unique keyword slug: %q", got)
	}
	if got := PublicPostPath(Post{Slug: "old-title", Keywords: "gpt-5.6"}); got != "gpt5.6" {
		t.Fatalf("legacy post path did not use keyword: %q", got)
	}
	if got := PublicPostPath(Post{Slug: "gpt5.6-2", Keywords: "gpt-5.6"}); got != "gpt5.6-2" {
		t.Fatalf("unique keyword suffix was lost: %q", got)
	}
}

func TestPlainTextSummary(t *testing.T) {
	rendered := "<h1>标题</h1><p>第一段 &amp; 第二段</p><pre><code>代码</code></pre>"
	if got := PlainTextSummary(rendered, 10); got != "标题 第一段 & 第" {
		t.Fatalf("unexpected summary: %q", got)
	}
	if got := PlainTextSummary("<p>短正文</p>", 60); got != "短正文" {
		t.Fatalf("short summary changed: %q", got)
	}
}
