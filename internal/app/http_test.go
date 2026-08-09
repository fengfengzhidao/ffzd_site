package app

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	a, err := New(Config{Addr: "127.0.0.1:0", DatabasePath: filepath.Join(dir, "blog.db"), UploadDir: filepath.Join(dir, "uploads"), AdminUsername: "admin", AdminPassword: "very-strong-password", SessionSecret: "test-session-secret-with-enough-entropy"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

func perform(handler http.Handler, method, path string, body io.Reader, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestPublicRoutesHideInvisiblePostsAndExposeSEO(t *testing.T) {
	a := newTestApp(t)
	pub := &Post{Title: "公开文章", Slug: "public", Summary: "用于 SEO", Keywords: "Go,测试", Markdown: "# 正文", HTML: `<h1>正文</h1><img src="/uploads/example.webp" alt="示例图片">`, Status: "published", IsVisible: true}
	if err := a.store.SavePost(pub, nil); err != nil {
		t.Fatal(err)
	}
	hidden := &Post{Title: "隐藏文章", Slug: "hidden", Markdown: "hidden", HTML: "<p>hidden</p>", Status: "published", IsVisible: false}
	if err := a.store.SavePost(hidden, nil); err != nil {
		t.Fatal(err)
	}
	handler := a.server.Handler
	res := perform(handler, "GET", "/posts/public", nil)
	if res.Code != 200 || !strings.Contains(res.Body.String(), "公开文章") || !strings.Contains(res.Body.String(), "application/ld+json") || !strings.Contains(res.Body.String(), `content="Go,测试"`) {
		t.Fatalf("published page failed: %d %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Header().Get("Content-Security-Policy"), "img-src 'self' data: blob: http: https:") {
		t.Fatal("image CSP must allow local Blob previews")
	}
	for _, expected := range []string{`data-image-lightbox`, `data-image-lightbox-close`, `aria-label="图片预览"`} {
		if !strings.Contains(res.Body.String(), expected) {
			t.Fatalf("article image preview is missing %q", expected)
		}
	}
	if res = perform(handler, "GET", "/posts/hidden", nil); res.Code != 404 {
		t.Fatalf("hidden post status = %d", res.Code)
	}
	if res = perform(handler, "GET", "/posts/public", nil); res.Code != 200 {
		t.Fatalf("legacy slug status = %d", res.Code)
	}
	keywordPost := &Post{Title: "GPT 发布说明", Slug: "old-gpt-title", Keywords: "gpt-5.6, Codex", Markdown: "正文", HTML: "<p>正文</p>", Status: "published", IsVisible: true}
	if err := a.store.SavePost(keywordPost, nil); err != nil {
		t.Fatal(err)
	}
	if res = perform(handler, "GET", "/posts/gpt5.6", nil); res.Code != 200 || !strings.Contains(res.Body.String(), "GPT 发布说明") {
		t.Fatalf("keyword URL failed: %d %s", res.Code, res.Body.String())
	}
	if res = perform(handler, "GET", "/posts", nil); res.Code != 200 || !strings.Contains(res.Body.String(), `href="/posts/gpt5.6"`) || strings.Contains(res.Body.String(), `href="/posts/old-gpt-title"`) {
		t.Fatalf("article list did not use keyword URL: %d %s", res.Code, res.Body.String())
	}
	if res = perform(handler, "GET", "/sitemap.xml", nil); res.Code != 200 || !strings.Contains(res.Body.String(), "/posts/go") || !strings.Contains(res.Body.String(), "/posts/gpt5.6") || strings.Contains(res.Body.String(), "/posts/public") || strings.Contains(res.Body.String(), "/posts/hidden") || strings.Contains(res.Body.String(), "/posts/old-gpt-title") {
		t.Fatalf("bad sitemap: %s", res.Body.String())
	}
	if res = perform(handler, "GET", "/admin/", nil); res.Code != http.StatusSeeOther {
		t.Fatalf("admin did not redirect: %d", res.Code)
	}
}

func TestLoginCSRFAndAuthenticatedDashboard(t *testing.T) {
	a := newTestApp(t)
	handler := a.server.Handler
	loginPage := perform(handler, "GET", "/admin/login", nil)
	if loginPage.Code != 200 {
		t.Fatal(loginPage.Code)
	}
	var csrfCookie *http.Cookie
	for _, c := range loginPage.Result().Cookies() {
		if c.Name == "login_csrf" {
			csrfCookie = c
		}
	}
	if csrfCookie == nil {
		t.Fatal("missing login csrf cookie")
	}
	form := url.Values{"username": {"admin"}, "password": {"very-strong-password"}, "csrf": {csrfCookie.Value}}
	loggedIn := perform(handler, "POST", "/admin/login", strings.NewReader(form.Encode()), csrfCookie)
	if loggedIn.Code != http.StatusSeeOther {
		t.Fatalf("login failed: %d %s", loggedIn.Code, loggedIn.Body.String())
	}
	var session *http.Cookie
	for _, c := range loggedIn.Result().Cookies() {
		if c.Name == "session" {
			session = c
		}
	}
	if session == nil {
		t.Fatal("missing session cookie")
	}
	dashboard := perform(handler, "GET", "/admin/", nil, session)
	if dashboard.Code != 200 || !strings.Contains(dashboard.Body.String(), "控制台") || !strings.Contains(dashboard.Body.String(), `class="admin-layout"`) {
		t.Fatalf("dashboard failed: %d", dashboard.Code)
	}
	if !strings.Contains(dashboard.Body.String(), `href="/admin/" title="控制台" aria-current="page"`) {
		t.Fatal("dashboard navigation is not active")
	}
	postsPage := perform(handler, "GET", "/admin/posts", nil, session)
	if postsPage.Code != 200 || !strings.Contains(postsPage.Body.String(), `class="admin-nav-link active" href="/admin/posts"`) {
		t.Fatalf("posts navigation is not active: %d", postsPage.Code)
	}
	if !strings.Contains(postsPage.Body.String(), `data-sidebar-toggle`) || !strings.Contains(postsPage.Body.String(), `data-sidebar-close`) {
		t.Fatal("responsive sidebar controls are missing")
	}
	if strings.Contains(postsPage.Body.String(), `/admin/taxonomies`) {
		t.Fatal("standalone taxonomy navigation should be removed")
	}

	formPage := perform(handler, "GET", "/admin/posts?compose=new", nil, session)
	if formPage.Code != 200 || !strings.Contains(formPage.Body.String(), `name="category_name"`) || !strings.Contains(formPage.Body.String(), `data-tag-combobox`) || !strings.Contains(formPage.Body.String(), `class="composer-layer is-open"`) {
		t.Fatalf("drawer taxonomy fields are missing: %d", formPage.Code)
	}
	for _, expected := range []string{`data-info-open`, `name="keywords"`, `data-info-layer`, `data-editor-count`, `composer-live-preview`, "自动从正文提取 60 个字符", "可粘贴或选择图片上传"} {
		if !strings.Contains(formPage.Body.String(), expected) {
			t.Fatalf("two-step editor is missing %q", expected)
		}
	}
	appJS := perform(handler, "GET", "/static/app.js", nil)
	if appJS.Code != http.StatusOK || appJS.Header().Get("Cache-Control") != "no-cache" || !strings.Contains(appJS.Body.String(), `addEventListener('paste'`) || !strings.Contains(appJS.Body.String(), "uploadAndInsertImages") {
		t.Fatalf("editor paste upload behavior is missing: %d", appJS.Code)
	}
	if strings.Contains(formPage.Body.String(), "保存为草稿") || strings.Contains(formPage.Body.String(), `name="status"`) {
		t.Fatal("draft controls should not exist in the article composer")
	}
	legacyFormPage := perform(handler, "GET", "/admin/posts/new", nil, session)
	if legacyFormPage.Code != http.StatusSeeOther || legacyFormPage.Header().Get("Location") != "/admin/posts?compose=new" {
		t.Fatalf("legacy editor route should redirect to drawer: %d %s", legacyFormPage.Code, legacyFormPage.Header().Get("Location"))
	}
	storedSession, err := a.store.GetSession(session.Value)
	if err != nil {
		t.Fatal(err)
	}
	coverForm := url.Values{"csrf": {storedSession.CSRF}, "source": {"external"}, "external_url": {"https://images.example.com/cover.webp"}}
	coverAdded := perform(handler, "POST", "/admin/covers", strings.NewReader(coverForm.Encode()), session)
	if coverAdded.Code != http.StatusSeeOther {
		t.Fatalf("external cover add failed: %d %s", coverAdded.Code, coverAdded.Body.String())
	}
	coversPage := perform(handler, "GET", "/admin/covers", nil, session)
	if coversPage.Code != http.StatusOK || !strings.Contains(coversPage.Body.String(), `class="admin-nav-link active" href="/admin/covers"`) || !strings.Contains(coversPage.Body.String(), "https://images.example.com/cover.webp") {
		t.Fatalf("cover management page failed: %d %s", coversPage.Code, coversPage.Body.String())
	}
	for _, expected := range []string{`data-cover-upload-open`, `data-cover-upload-dialog`, `data-cover-drop-zone`, `data-cover-preview-loading`, `class="cover-time"`, "选择图片或粘贴图片"} {
		if !strings.Contains(coversPage.Body.String(), expected) {
			t.Fatalf("cover upload modal is missing %q", expected)
		}
	}
	secondCoverForm := url.Values{"csrf": {storedSession.CSRF}, "external_url": {"https://images.example.com/cover-2.webp"}}
	jsonCoverRequest := httptest.NewRequest("POST", "/admin/covers", strings.NewReader(secondCoverForm.Encode()))
	jsonCoverRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	jsonCoverRequest.Header.Set("Accept", "application/json")
	jsonCoverRequest.AddCookie(session)
	jsonCoverResponse := httptest.NewRecorder()
	handler.ServeHTTP(jsonCoverResponse, jsonCoverRequest)
	if jsonCoverResponse.Code != http.StatusOK || !strings.Contains(jsonCoverResponse.Body.String(), `"URL":"https://images.example.com/cover-2.webp"`) {
		t.Fatalf("JSON cover upload failed: %d %s", jsonCoverResponse.Code, jsonCoverResponse.Body.String())
	}
	covers, err := a.store.Covers()
	if err != nil || len(covers) != 2 {
		t.Fatalf("cover library was not saved: %#v %v", covers, err)
	}
	var selectedCoverID int64
	for _, cover := range covers {
		if cover.URL == "https://images.example.com/cover.webp" {
			selectedCoverID = cover.ID
		}
	}
	postForm := url.Values{
		"csrf":          {storedSession.CSRF},
		"title":         {"内联分类测试"},
		"markdown":      {"正文"},
		"keywords":      {"Go-1.26, SQLite"},
		"is_visible":    {"1"},
		"category_name": {"新分类"},
		"new_tags":      {"标签一，标签二, 标签一"},
		"cover_id":      {strconv.FormatInt(selectedCoverID, 10)},
	}
	savedPost := perform(handler, "POST", "/admin/posts/new", strings.NewReader(postForm.Encode()), session)
	if savedPost.Code != http.StatusSeeOther {
		t.Fatalf("inline taxonomy post save failed: %d %s", savedPost.Code, savedPost.Body.String())
	}
	categories, _ := a.store.Categories()
	tags, _ := a.store.Tags()
	if len(categories) != 1 || categories[0].Name != "新分类" || len(tags) != 2 {
		t.Fatalf("inline taxonomies were not created: %#v %#v", categories, tags)
	}
	saved, err := a.store.PostByID(1)
	if err != nil || saved.Status != "published" || !saved.IsVisible || saved.Summary != "正文" || saved.Keywords != "Go-1.26, SQLite" || saved.Slug != "go1.26" || saved.CoverURL != "https://images.example.com/cover.webp" {
		t.Fatalf("new article should be published and visible by default: %#v %v", saved, err)
	}
	postsPage = perform(handler, "GET", "/admin/posts", nil, session)
	if !strings.Contains(postsPage.Body.String(), `href="/posts/go1.26">内联分类测试</a>`) || !strings.Contains(postsPage.Body.String(), `class="admin-post-cover"`) || !strings.Contains(postsPage.Body.String(), `/random-cover`) {
		t.Fatal("admin post title should link to the public post detail page")
	}
	publicPosts := perform(handler, "GET", "/posts", nil)
	if publicPosts.Code != http.StatusOK || !strings.Contains(publicPosts.Body.String(), `class="post-cover"`) || !strings.Contains(publicPosts.Body.String(), `src="https://images.example.com/cover.webp"`) {
		t.Fatalf("public post cover is missing: %d %s", publicPosts.Code, publicPosts.Body.String())
	}
	randomized := perform(handler, "POST", "/admin/posts/1/random-cover", strings.NewReader(url.Values{"csrf": {storedSession.CSRF}}.Encode()), session)
	if randomized.Code != http.StatusSeeOther {
		t.Fatalf("random cover action failed: %d %s", randomized.Code, randomized.Body.String())
	}
	if !strings.Contains(postsPage.Body.String(), `href="/admin/posts?compose=1&amp;info=1">基本信息</a>`) {
		t.Fatal("admin post list should expose the basic information editor")
	}
	for i := 0; i < 38; i++ {
		if _, err := a.store.AddCover(fmt.Sprintf("https://images.example.com/library-%02d.webp", i), "external"); err != nil {
			t.Fatal(err)
		}
	}
	infoPage := perform(handler, "GET", "/admin/posts?compose=1&info=1", nil, session)
	for _, expected := range []string{`class="post-info-layer is-open"`, `data-info-direct="true"`, "编辑文章基本信息", `data-info-direct-save`, `value="内联分类测试"`, `data-current-cover`, `data-cover-select-layer`, `data-cover-select-confirm`, `data-post-cover-options`, `name="cover_id"`, "上传新封面"} {
		if infoPage.Code != http.StatusOK || !strings.Contains(infoPage.Body.String(), expected) {
			t.Fatalf("basic information editor is missing %q: %d", expected, infoPage.Code)
		}
	}
	if count := strings.Count(infoPage.Body.String(), `class="post-cover-choice"`); count != 40 {
		t.Fatalf("large cover library rendered %d choices, want 40", count)
	}
	editPage := perform(handler, "GET", "/admin/posts?compose=1", nil, session)
	if editPage.Code != http.StatusOK || !strings.Contains(editPage.Body.String(), "编辑文章") || !strings.Contains(editPage.Body.String(), "内联分类测试") {
		t.Fatalf("edit composer did not load saved post: %d", editPage.Code)
	}
	editForm := url.Values{
		"csrf":       {storedSession.CSRF},
		"title":      {"内联分类测试（已编辑）"},
		"slug":       {saved.Slug},
		"markdown":   {"更新后的正文"},
		"keywords":   {"Go-1.26, SQLite"},
		"is_visible": {"1"},
	}
	updatedPost := perform(handler, "POST", "/admin/posts/1", strings.NewReader(editForm.Encode()), session)
	if updatedPost.Code != http.StatusSeeOther {
		t.Fatalf("post update failed: %d %s", updatedPost.Code, updatedPost.Body.String())
	}
	updated, err := a.store.PostByID(1)
	if err != nil || updated.Slug != "go1.26" {
		t.Fatalf("published slug changed while editing: %#v %v", updated, err)
	}
	newPage := perform(handler, "GET", "/admin/posts?compose=new", nil, session)
	if newPage.Code != http.StatusOK || !strings.Contains(newPage.Body.String(), "发布文章") || !strings.Contains(newPage.Body.String(), `data-new-composer`) || !strings.Contains(newPage.Body.String(), `action="/admin/posts/new"`) || !strings.Contains(newPage.Body.String(), `class="post-info-field"`) || strings.Contains(newPage.Body.String(), `value="内联分类测试"`) || strings.Contains(newPage.Body.String(), `>正文</textarea>`) {
		t.Fatalf("new composer retained edit state: %d %s", newPage.Code, newPage.Body.String())
	}
	toggle := url.Values{"csrf": {storedSession.CSRF}, "is_visible": {"0"}}
	toggled := perform(handler, "POST", "/admin/posts/1/visibility", strings.NewReader(toggle.Encode()), session)
	if toggled.Code != http.StatusSeeOther {
		t.Fatalf("visibility toggle failed: %d", toggled.Code)
	}
	if public := perform(handler, "GET", "/posts/内联分类测试", nil); public.Code != http.StatusNotFound {
		t.Fatalf("hidden article leaked publicly: %d", public.Code)
	}

	removedPage := perform(handler, "GET", "/admin/taxonomies", nil, session)
	if removedPage.Code != http.StatusNotFound {
		t.Fatalf("removed taxonomy page status = %d", removedPage.Code)
	}
}

func TestParseNewTagNames(t *testing.T) {
	names, err := parseNewTagNames("Go, SQLite，go；Web\nAPI")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(names, "|"); got != "Go|SQLite|Web|API" {
		t.Fatalf("unexpected tags: %s", got)
	}
	if _, err := parseNewTagNames(strings.Repeat("长", 51)); err == nil {
		t.Fatal("overlong tag should be rejected")
	}
}

func TestValidateExternalImageURL(t *testing.T) {
	if got, err := validateExternalImageURL(" https://example.com/image.webp "); err != nil || got != "https://example.com/image.webp" {
		t.Fatalf("valid external image URL rejected: %q %v", got, err)
	}
	for _, raw := range []string{"javascript:alert(1)", "//example.com/image.png", "https://user:pass@example.com/image.png"} {
		if _, err := validateExternalImageURL(raw); err == nil {
			t.Fatalf("unsafe external image URL accepted: %s", raw)
		}
	}
}

func TestImageValidationAndSave(t *testing.T) {
	a := newTestApp(t)
	validPath := filepath.Join(t.TempDir(), "tiny.png")
	// PNG signature is sufficient for MIME validation; the upload endpoint does not transform images.
	if err := os.WriteFile(validPath, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0}, 0600); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(validPath)
	defer f.Close()
	path, err := a.saveImage(f, &multipart.FileHeader{Filename: "tiny.png"})
	if err != nil || !strings.HasSuffix(path, ".png") {
		t.Fatalf("valid image rejected: %s %v", path, err)
	}
	badPath := filepath.Join(t.TempDir(), "bad.txt")
	_ = os.WriteFile(badPath, []byte("not an image"), 0600)
	bad, _ := os.Open(badPath)
	defer bad.Close()
	if _, err = a.saveImage(bad, &multipart.FileHeader{Filename: "bad.txt"}); err == nil {
		t.Fatal("non-image accepted")
	}
}

func TestSettingsUploadSiteIcon(t *testing.T) {
	a := newTestApp(t)
	admin, err := a.store.Authenticate("admin", "very-strong-password")
	if err != nil {
		t.Fatal(err)
	}
	token, csrf, err := a.store.CreateSession(admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fields := map[string]string{
		"csrf": csrf, "site_title": "图标测试站", "author": "站长", "tagline": "测试",
		"description": "测试描述", "site_url": "https://example.com", "seo_keywords": "test",
		"posts_per_page": "12", "footer_text": "页脚",
	}
	for key, value := range fields {
		if err = writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := writer.CreateFormFile("site_icon", "favicon.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/settings", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: "session", Value: token})
	res := httptest.NewRecorder()
	a.server.Handler.ServeHTTP(res, req)
	if res.Code != http.StatusSeeOther {
		t.Fatalf("settings save failed: %d %s", res.Code, res.Body.String())
	}
	settings, err := a.store.Settings()
	if err != nil || !strings.HasSuffix(settings.SiteIcon, ".png") {
		t.Fatalf("site icon setting missing: %q %v", settings.SiteIcon, err)
	}
	home := perform(a.server.Handler, http.MethodGet, "/", nil)
	if home.Code != http.StatusOK || !strings.Contains(home.Body.String(), `<link rel="icon" href="`+settings.SiteIcon+`">`) {
		t.Fatalf("favicon link missing from public page: %d", home.Code)
	}
	dashboard := perform(a.server.Handler, http.MethodGet, "/admin/", nil, &http.Cookie{Name: "session", Value: token})
	if dashboard.Code != http.StatusOK || !strings.Contains(dashboard.Body.String(), `<span class="admin-brand-mark has-image"><img src="`+settings.SiteIcon+`"`) {
		t.Fatalf("site icon missing from admin brand: %d", dashboard.Code)
	}
	icon := perform(a.server.Handler, http.MethodGet, settings.SiteIcon, nil)
	if icon.Code != http.StatusOK || icon.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("saved icon is not publicly available: %d %q", icon.Code, icon.Header().Get("Content-Type"))
	}
}
