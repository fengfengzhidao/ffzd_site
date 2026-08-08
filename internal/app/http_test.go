package app

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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

func TestPublicRoutesHideDraftAndExposeSEO(t *testing.T) {
	a := newTestApp(t)
	pub := &Post{Title: "公开文章", Slug: "public", Summary: "用于 SEO", Markdown: "# 正文", HTML: "<h1>正文</h1>", Status: "published"}
	if err := a.store.SavePost(pub, nil); err != nil {
		t.Fatal(err)
	}
	draft := &Post{Title: "草稿", Slug: "draft", Markdown: "hidden", HTML: "<p>hidden</p>", Status: "draft"}
	if err := a.store.SavePost(draft, nil); err != nil {
		t.Fatal(err)
	}
	handler := a.server.Handler
	res := perform(handler, "GET", "/posts/public", nil)
	if res.Code != 200 || !strings.Contains(res.Body.String(), "公开文章") || !strings.Contains(res.Body.String(), "application/ld+json") {
		t.Fatalf("published page failed: %d %s", res.Code, res.Body.String())
	}
	if res = perform(handler, "GET", "/posts/draft", nil); res.Code != 404 {
		t.Fatalf("draft status = %d", res.Code)
	}
	if res = perform(handler, "GET", "/sitemap.xml", nil); res.Code != 200 || !strings.Contains(res.Body.String(), "/posts/public") || strings.Contains(res.Body.String(), "/posts/draft") {
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

	formPage := perform(handler, "GET", "/admin/posts/new", nil, session)
	if formPage.Code != 200 || !strings.Contains(formPage.Body.String(), `name="new_category"`) || !strings.Contains(formPage.Body.String(), `name="new_tags"`) {
		t.Fatalf("inline taxonomy fields are missing: %d", formPage.Code)
	}
	storedSession, err := a.store.GetSession(session.Value)
	if err != nil {
		t.Fatal(err)
	}
	postForm := url.Values{
		"csrf":         {storedSession.CSRF},
		"title":        {"内联分类测试"},
		"markdown":     {"正文"},
		"status":       {"draft"},
		"new_category": {"新分类"},
		"new_tags":     {"标签一，标签二, 标签一"},
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
	if dashboard.Code != http.StatusOK || !strings.Contains(dashboard.Body.String(), `<span class="admin-brand-mark"><img src="`+settings.SiteIcon+`"`) {
		t.Fatalf("site icon missing from admin brand: %d", dashboard.Code)
	}
	icon := perform(a.server.Handler, http.MethodGet, settings.SiteIcon, nil)
	if icon.Code != http.StatusOK || icon.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("saved icon is not publicly available: %d %q", icon.Code, icon.Header().Get("Content-Type"))
	}
}
