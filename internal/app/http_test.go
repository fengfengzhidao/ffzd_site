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
	"time"
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

func TestTopicAdminAndPublicReadingRoutes(t *testing.T) {
	a := newTestApp(t)
	post := &Post{Title: "专题文章", Slug: "topic-post", Markdown: "正文", HTML: "<p>专题正文</p>", Status: "published", IsVisible: true}
	if err := a.store.SavePost(post, nil); err != nil {
		t.Fatal(err)
	}
	admin, err := a.store.Authenticate("admin", "very-strong-password")
	if err != nil {
		t.Fatal(err)
	}
	token, csrf, err := a.store.CreateSession(admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	session := &http.Cookie{Name: "session", Value: token}
	handler := a.server.Handler

	create := url.Values{"csrf": {csrf}, "name": {"Go 入门专题"}}
	response := perform(handler, http.MethodPost, "/admin/topics/new", strings.NewReader(create.Encode()), session)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("create topic failed: %d %s", response.Code, response.Body.String())
	}
	topics, _ := a.store.Topics()
	if len(topics) != 1 {
		t.Fatalf("topic was not created: %#v", topics)
	}
	node := url.Values{"csrf": {csrf}, "title": {"第一章：快速开始"}, "sort_order": {"1"}}
	response = perform(handler, http.MethodPost, fmt.Sprintf("/admin/topics/%d/nodes", topics[0].ID), strings.NewReader(node.Encode()), session)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("create node failed: %d %s", response.Code, response.Body.String())
	}
	jsonNode := url.Values{"csrf": {csrf}, "title": {"第二章：异步刷新"}, "parent_id": {"1"}, "post_id": {strconv.FormatInt(post.ID, 10)}, "sort_order": {"2"}}
	jsonRequest := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/topics/%d/nodes", topics[0].ID), strings.NewReader(jsonNode.Encode()))
	jsonRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	jsonRequest.Header.Set("Accept", "application/json")
	jsonRequest.AddCookie(session)
	jsonResponse := httptest.NewRecorder()
	handler.ServeHTTP(jsonResponse, jsonRequest)
	if jsonResponse.Code != http.StatusOK || !strings.Contains(jsonResponse.Body.String(), `"selectedID"`) || !strings.Contains(jsonResponse.Body.String(), "第二章：异步刷新") {
		t.Fatalf("JSON node refresh failed: %d %s", jsonResponse.Code, jsonResponse.Body.String())
	}
	adminPage := perform(handler, http.MethodGet, "/admin/topics", nil, session)
	for _, expected := range []string{`class="admin-nav-link active" href="/admin/topics"`, "关联文档", `data-topic-tree`, `data-topic-toggle`, `type="hidden" name="parent_id"`, "第一章：快速开始"} {
		if adminPage.Code != http.StatusOK || !strings.Contains(adminPage.Body.String(), expected) {
			t.Fatalf("topic admin page is missing %q: %d %s", expected, adminPage.Code, adminPage.Body.String())
		}
	}
	if strings.Contains(adminPage.Body.String(), `<select name="parent_id">`) {
		t.Fatal("topic parent directory should not be manually selectable")
	}
	appJS := perform(handler, http.MethodGet, "/static/app.js", nil)
	if appJS.Code != http.StatusOK || !strings.Contains(appJS.Body.String(), "renderNodes(result.nodes") || !strings.Contains(appJS.Body.String(), "节点已保存，文档树已刷新") || !strings.Contains(appJS.Body.String(), "selected?.dataset.directory ? selected : null") || !strings.Contains(appJS.Body.String(), "syncNodeVisibility") {
		t.Fatalf("topic tree partial refresh script is missing: %d", appJS.Code)
	}
	style := perform(handler, http.MethodGet, "/static/style.css", nil)
	if style.Code != http.StatusOK || !strings.Contains(style.Body.String(), ".topic-node-list>button[hidden]{display:none}") {
		t.Fatalf("collapsed topic children are not explicitly hidden: %d", style.Code)
	}
	listPage := perform(handler, http.MethodGet, "/topics", nil)
	publicTopicPath := "/topics/" + url.PathEscape(topics[0].Slug)
	if listPage.Code != http.StatusOK || !strings.Contains(listPage.Body.String(), "Go 入门专题") || !strings.Contains(listPage.Body.String(), `href="/topics/`) {
		t.Fatalf("public topic list failed: %d %s", listPage.Code, listPage.Body.String())
	}
	detailPage := perform(handler, http.MethodGet, publicTopicPath, nil)
	if detailPage.Code != http.StatusOK || !strings.Contains(detailPage.Body.String(), "专题正文") || !strings.Contains(detailPage.Body.String(), "第一章：快速开始") || !strings.Contains(detailPage.Body.String(), `data-topic-tree-toggle`) || strings.Contains(detailPage.Body.String(), "ZgotmplZ") {
		t.Fatalf("public topic reader failed: %d %s", detailPage.Code, detailPage.Body.String())
	}
	rawUnicodeDetail := perform(handler, http.MethodGet, "/topics/"+topics[0].Slug, nil)
	if rawUnicodeDetail.Code != http.StatusOK || !strings.Contains(rawUnicodeDetail.Body.String(), "专题正文") {
		t.Fatalf("raw Unicode topic URL failed: %d %s", rawUnicodeDetail.Code, rawUnicodeDetail.Body.String())
	}
	missingTopic := perform(handler, http.MethodGet, "/topics/not-found", nil)
	if missingTopic.Code != http.StatusNotFound || missingTopic.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("topic 404 must not be cached: %d %q", missingTopic.Code, missingTopic.Header().Get("Cache-Control"))
	}
	sitemap := perform(handler, http.MethodGet, "/sitemap.xml", nil)
	if sitemap.Code != http.StatusOK || !strings.Contains(sitemap.Body.String(), "/topics") || !strings.Contains(sitemap.Body.String(), "/topic-post") {
		t.Fatalf("topic sitemap entries are missing: %s", sitemap.Body.String())
	}
	if badCSRF := perform(handler, http.MethodPost, "/admin/topics/new", strings.NewReader(url.Values{"name": {"越权专题"}}.Encode()), session); badCSRF.Code != http.StatusForbidden {
		t.Fatalf("topic mutation did not enforce CSRF: %d", badCSRF.Code)
	}
}

func TestPageViewCountingAndAnalyticsPage(t *testing.T) {
	a := newTestApp(t)
	if err := a.store.SaveTaxonomy("category", 0, "Go", "go"); err != nil {
		t.Fatal(err)
	}
	if err := a.store.SaveTaxonomy("tag", 0, "统计", "stats"); err != nil {
		t.Fatal(err)
	}
	categories, _ := a.store.Categories()
	tags, _ := a.store.Tags()
	post := &Post{Title: "浏览量文章", Slug: "view-count-post", Markdown: "正文", HTML: "<p>正文</p>", Status: "published", IsVisible: true, CategoryID: &categories[0].ID}
	if err := a.store.SavePost(post, []int64{tags[0].ID}); err != nil {
		t.Fatal(err)
	}
	topic := &Topic{Name: "浏览量专题", Slug: "view-count-topic"}
	if err := a.store.SaveTopic(topic); err != nil {
		t.Fatal(err)
	}
	if err := a.store.SaveTopicNode(&TopicNode{TopicID: topic.ID, PostID: &post.ID, Title: "统计正文"}); err != nil {
		t.Fatal(err)
	}

	handler := a.server.Handler
	for _, path := range []string{"/", "/posts", "/categories/go", "/tags/stats"} {
		if response := perform(handler, http.MethodGet, path, nil); response.Code != http.StatusOK {
			t.Fatalf("counted page %s status = %d", path, response.Code)
		}
	}
	if response := perform(handler, http.MethodGet, "/home/posts", nil); response.Code != http.StatusOK {
		t.Fatalf("home fragment status = %d", response.Code)
	}
	article := perform(handler, http.MethodGet, "/posts/view-count-post", nil)
	if article.Code != http.StatusOK || !strings.Contains(article.Body.String(), "1 次浏览") {
		t.Fatalf("article view count missing: %d %s", article.Code, article.Body.String())
	}
	if response := perform(handler, http.MethodGet, "/topics", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "1 次浏览") {
		t.Fatalf("topic list view count missing: %d %s", response.Code, response.Body.String())
	}
	topicPage := perform(handler, http.MethodGet, "/topics/view-count-topic", nil)
	if topicPage.Code != http.StatusOK || !strings.Contains(topicPage.Body.String(), "专题 2 次浏览") {
		t.Fatalf("topic reader view count missing: %d %s", topicPage.Code, topicPage.Body.String())
	}
	for _, path := range []string{"/missing-page", "/sitemap.xml", "/robots.txt", "/static/style.css", "/admin/stats"} {
		_ = perform(handler, http.MethodGet, path, nil)
	}

	admin, _ := a.store.Authenticate("admin", "very-strong-password")
	token, _, err := a.store.CreateSession(admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	statsPage := perform(handler, http.MethodGet, "/admin/stats", nil, &http.Cookie{Name: "session", Value: token})
	for _, expected := range []string{
		`class="admin-nav-link active" href="/admin/stats"`,
		`<span>累计浏览</span><strong>7</strong>`,
		"近 30 天全站浏览量",
		"浏览量文章",
		"浏览量专题",
		`style="--bar-height:`,
		`data-tooltip=`,
		`aria-label=`,
	} {
		if statsPage.Code != http.StatusOK || !strings.Contains(statsPage.Body.String(), expected) {
			t.Fatalf("analytics page missing %q: %d %s", expected, statsPage.Code, statsPage.Body.String())
		}
	}
	if strings.Contains(statsPage.Body.String(), "ZgotmplZ") {
		t.Fatal("analytics chart contains escaped unsafe CSS")
	}
	stored, err := a.store.PostByID(post.ID)
	if err != nil || stored.ViewCount != 2 {
		t.Fatalf("article counter = %d, %v", stored.ViewCount, err)
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

func TestHomeDynamicSearchAndFeedbackManagement(t *testing.T) {
	a := newTestApp(t)
	if err := a.store.SaveTaxonomy("tag", 0, "SQLite", "sqlite"); err != nil {
		t.Fatal(err)
	}
	tags, _ := a.store.Tags()
	matching := &Post{Title: "Go SQLite 实战", Slug: "go-sqlite", Markdown: "正文", HTML: "<p>正文</p>", Status: "published", IsVisible: true}
	if err := a.store.SavePost(matching, []int64{tags[0].ID}); err != nil {
		t.Fatal(err)
	}
	other := &Post{Title: "Go 网络编程", Slug: "go-network", Markdown: "正文", HTML: "<p>正文</p>", Status: "published", IsVisible: true}
	if err := a.store.SavePost(other, nil); err != nil {
		t.Fatal(err)
	}
	settings, _ := a.store.Settings()
	settings.PostsPerPage = 1
	if err := a.store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	handler := a.server.Handler

	home := perform(handler, http.MethodGet, "/", nil)
	for _, expected := range []string{`class="container home-main"`, `data-home-search-input`, `class="home-sidebar"`, "站点信息", "标签列表", "在线反馈", `data-home-page="2"`} {
		if home.Code != http.StatusOK || !strings.Contains(home.Body.String(), expected) {
			t.Fatalf("home is missing %q: %d %s", expected, home.Code, home.Body.String())
		}
	}
	filtered := perform(handler, http.MethodGet, "/?q=SQLite&tag=sqlite", nil)
	if filtered.Code != http.StatusOK || !strings.Contains(filtered.Body.String(), "Go SQLite 实战") || strings.Contains(filtered.Body.String(), "Go 网络编程") || !strings.Contains(filtered.Body.String(), `value="SQLite"`) {
		t.Fatalf("full home filtering failed: %d %s", filtered.Code, filtered.Body.String())
	}
	fragment := perform(handler, http.MethodGet, "/home/posts?q=SQLite&tag=sqlite", nil)
	if fragment.Code != http.StatusOK || !strings.Contains(fragment.Body.String(), "Go SQLite 实战") || strings.Contains(fragment.Body.String(), "<html") {
		t.Fatalf("home result fragment failed: %d %s", fragment.Code, fragment.Body.String())
	}
	tooLong := perform(handler, http.MethodGet, "/?q="+url.QueryEscape(strings.Repeat("长", 101)), nil)
	if tooLong.Code != http.StatusBadRequest {
		t.Fatalf("overlong search status = %d", tooLong.Code)
	}

	feedbackForm := url.Values{"name": {"访客"}, "contact": {"visitor@example.com"}, "content": {"这是一条私密反馈"}}
	submitted := perform(handler, http.MethodPost, "/feedback", strings.NewReader(feedbackForm.Encode()))
	if submitted.Code != http.StatusSeeOther || submitted.Header().Get("Location") != "/?feedback=saved#feedback" {
		t.Fatalf("feedback submit failed: %d %s", submitted.Code, submitted.Body.String())
	}
	publicHome := perform(handler, http.MethodGet, "/", nil)
	if strings.Contains(publicHome.Body.String(), "这是一条私密反馈") {
		t.Fatal("private feedback leaked on the public home page")
	}
	honeypot := url.Values{"website": {"spam.example"}, "content": {"垃圾内容"}}
	if response := perform(handler, http.MethodPost, "/feedback", strings.NewReader(honeypot.Encode())); response.Code != http.StatusSeeOther {
		t.Fatalf("honeypot response = %d", response.Code)
	}
	feedback, total, err := a.store.Feedback(1, 20)
	if err != nil || total != 1 || len(feedback) != 1 {
		t.Fatalf("unexpected stored feedback: %#v %d %v", feedback, total, err)
	}
	if response := perform(handler, http.MethodPost, "/feedback", strings.NewReader(url.Values{"content": {""}}.Encode())); response.Code != http.StatusBadRequest {
		t.Fatalf("empty feedback status = %d", response.Code)
	}
	if response := perform(handler, http.MethodGet, "/admin/feedback", nil); response.Code != http.StatusSeeOther {
		t.Fatalf("anonymous admin feedback status = %d", response.Code)
	}

	admin, _ := a.store.Authenticate("admin", "very-strong-password")
	token, csrf, err := a.store.CreateSession(admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	session := &http.Cookie{Name: "session", Value: token}
	adminPage := perform(handler, http.MethodGet, "/admin/feedback", nil, session)
	if adminPage.Code != http.StatusOK || !strings.Contains(adminPage.Body.String(), "这是一条私密反馈") || !strings.Contains(adminPage.Body.String(), `class="admin-nav-link active" href="/admin/feedback"`) {
		t.Fatalf("admin feedback page failed: %d %s", adminPage.Code, adminPage.Body.String())
	}
	withoutCSRF := perform(handler, http.MethodPost, fmt.Sprintf("/admin/feedback/%d/read", feedback[0].ID), strings.NewReader("is_read=1"), session)
	if withoutCSRF.Code != http.StatusForbidden {
		t.Fatalf("feedback update without CSRF status = %d", withoutCSRF.Code)
	}
	readForm := url.Values{"csrf": {csrf}, "is_read": {"1"}}
	if response := perform(handler, http.MethodPost, fmt.Sprintf("/admin/feedback/%d/read", feedback[0].ID), strings.NewReader(readForm.Encode()), session); response.Code != http.StatusSeeOther {
		t.Fatalf("feedback read update status = %d", response.Code)
	}
	deleteForm := url.Values{"csrf": {csrf}}
	if response := perform(handler, http.MethodPost, fmt.Sprintf("/admin/feedback/%d/delete", feedback[0].ID), strings.NewReader(deleteForm.Encode()), session); response.Code != http.StatusSeeOther {
		t.Fatalf("feedback delete status = %d", response.Code)
	}
}

func TestSubmissionLimiter(t *testing.T) {
	limiter := newSubmissionLimiter(2, time.Minute)
	if !limiter.Allow("192.0.2.1") || !limiter.Allow("192.0.2.1") || limiter.Allow("192.0.2.1") {
		t.Fatal("submission limiter did not enforce its limit")
	}
	if !limiter.Allow("192.0.2.2") {
		t.Fatal("submission limiter mixed different clients")
	}
}

func TestFeedbackAcceptsMultipartFormData(t *testing.T) {
	a := newTestApp(t)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range map[string]string{
		"name": "千五", "contact": "2222", "content": "你好你",
	} {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/feedback", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	response := httptest.NewRecorder()
	a.server.Handler.ServeHTTP(response, req)
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"ok":true`) {
		t.Fatalf("multipart feedback failed: %d %s", response.Code, response.Body.String())
	}
	feedback, total, err := a.store.Feedback(1, 20)
	if err != nil || total != 1 || feedback[0].Name != "千五" || feedback[0].Contact != "2222" || feedback[0].Content != "你好你" {
		t.Fatalf("multipart feedback was not stored: %#v %d %v", feedback, total, err)
	}
}
