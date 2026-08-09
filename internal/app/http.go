package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

//go:embed templates/*.html static/*
var webFS embed.FS

type App struct {
	cfg             Config
	store           *Store
	markdown        *MarkdownRenderer
	templates       *template.Template
	server          *http.Server
	limiter         *loginLimiter
	feedbackLimiter *submissionLimiter
}

type viewData struct {
	Settings                                                      Settings
	Title, Description, Canonical, CSRF, Error, Flash, FilterName string
	CurrentPath                                                   string
	Admin                                                         *Session
	Post                                                          *Post
	Posts                                                         []Post
	Covers                                                        []Cover
	Feedbacks                                                     []Feedback
	Categories                                                    []Category
	Tags                                                          []Tag
	Stats                                                         DashboardStats
	Page, TotalPages, Total                                       int
	Query, ActiveTag                                              string
	Now                                                           time.Time
	JSONLD                                                        template.JS
	ComposerOpen, InfoOpen                                        bool
}

type contextKey string

const sessionKey contextKey = "session"

func New(cfg Config) (*App, error) {
	store, err := OpenStore(cfg.DatabasePath)
	if err != nil {
		return nil, err
	}
	if err = store.EnsureAdmin(cfg.AdminUsername, cfg.AdminPassword); err != nil {
		store.DB.Close()
		return nil, err
	}
	funcs := template.FuncMap{
		"date": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.Local().Format("2006-01-02")
		},
		"formatTime": func(t time.Time) string { return t.Local().Format("2006-01-02 15:04") },
		"safeHTML":   func(s string) template.HTML { return template.HTML(s) },
		"postPath":   PublicPostPath,
		"coverSelected": func(id int64, selected *int64) bool {
			return selected != nil && id == *selected
		},
		"tagNames": func(tags []Tag) string {
			names := make([]string, 0, len(tags))
			for _, tag := range tags {
				names = append(names, tag.Name)
			}
			return strings.Join(names, ", ")
		},
		"seq": func(n int) []int {
			v := make([]int, n)
			for i := range v {
				v[i] = i + 1
			}
			return v
		},
		"navActive": func(current, target string) bool {
			if target == "/admin/" {
				return current == target
			}
			return strings.HasPrefix(current, target)
		},
		"homeURL": func(query, tag string, page int) string {
			values := url.Values{}
			if query != "" {
				values.Set("q", query)
			}
			if tag != "" {
				values.Set("tag", tag)
			}
			if page > 1 {
				values.Set("page", strconv.Itoa(page))
			}
			if encoded := values.Encode(); encoded != "" {
				return "/?" + encoded
			}
			return "/"
		},
	}
	t, err := template.New("pages").Funcs(funcs).ParseFS(webFS, "templates/*.html")
	if err != nil {
		store.DB.Close()
		return nil, err
	}
	a := &App{cfg: cfg, store: store, markdown: NewMarkdownRenderer(), templates: t, limiter: newLoginLimiter(), feedbackLimiter: newSubmissionLimiter(3, 10*time.Minute)}
	a.server = &http.Server{Addr: cfg.Addr, Handler: a.routes(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	return a, nil
}
func (a *App) Close() error { return a.store.DB.Close() }
func (a *App) ListenAndServe() error {
	err := a.server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
func (a *App) Shutdown(ctx context.Context) error { return a.server.Shutdown(ctx) }

func (a *App) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer, a.securityHeaders)
	staticFS, _ := fs.Sub(webFS, "static")
	staticHandler := http.StripPrefix("/static/", http.FileServer(http.FS(staticFS)))
	r.Handle("/static/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		staticHandler.ServeHTTP(w, r)
	}))
	r.Handle("/uploads/*", http.StripPrefix("/uploads/", http.FileServer(http.Dir(a.cfg.UploadDir))))
	r.Get("/", a.home)
	r.Get("/home/posts", a.homePosts)
	r.Post("/feedback", a.submitFeedback)
	r.Get("/posts", a.posts)
	r.Get("/posts/{slug}", a.postDetail)
	r.Get("/categories/{slug}", a.categoryPosts)
	r.Get("/tags/{slug}", a.tagPosts)
	r.Get("/sitemap.xml", a.sitemap)
	r.Get("/robots.txt", a.robots)
	r.Route("/admin", func(r chi.Router) {
		r.Get("/login", a.loginPage)
		r.Post("/login", a.login)
		r.Group(func(r chi.Router) {
			r.Use(a.requireAuth, a.requireCSRF)
			r.Get("/", a.dashboard)
			r.Post("/logout", a.logout)
			r.Get("/posts", a.adminPosts)
			r.Get("/posts/new", a.editPostPage)
			r.Post("/posts/new", a.savePost)
			r.Get("/posts/{id}", a.editPostPage)
			r.Post("/posts/{id}", a.savePost)
			r.Post("/posts/{id}/visibility", a.togglePostVisibility)
			r.Post("/posts/{id}/random-cover", a.randomizePostCover)
			r.Post("/posts/{id}/delete", a.deletePost)
			r.Get("/covers", a.coversPage)
			r.Post("/covers", a.addCover)
			r.Post("/covers/{id}/delete", a.deleteCover)
			r.Get("/feedback", a.adminFeedback)
			r.Post("/feedback/{id}/read", a.setFeedbackRead)
			r.Post("/feedback/{id}/delete", a.deleteFeedback)
			r.Post("/preview", a.preview)
			r.Post("/upload", a.upload)
			r.Get("/settings", a.settingsPage)
			r.Post("/settings", a.saveSettings)
		})
	})
	r.NotFound(a.notFound)
	return r
}

func (a *App) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob: http: https:; style-src 'self' 'unsafe-inline'; script-src 'self'; form-action 'self'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func (a *App) baseData(r *http.Request, title, desc string) viewData {
	settings, _ := a.store.Settings()
	canonical := strings.TrimRight(settings.SiteURL, "/") + r.URL.Path
	if r.URL.RawQuery != "" {
		canonical += "?" + r.URL.RawQuery
	}
	d := viewData{Settings: settings, Title: title, Description: desc, Canonical: canonical, CurrentPath: r.URL.Path, Now: time.Now()}
	if s, ok := r.Context().Value(sessionKey).(*Session); ok {
		d.Admin = s
		d.CSRF = s.CSRF
	}
	return d
}
func (a *App) render(w http.ResponseWriter, r *http.Request, name string, data viewData, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := a.templates.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("template %s: %v", name, err)
	}
}
func pageOf(r *http.Request) int {
	p, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if p < 1 {
		p = 1
	}
	return p
}
func pages(total, per int) int {
	if per < 1 {
		return 1
	}
	n := (total + per - 1) / per
	if n < 1 {
		return 1
	}
	return n
}

func (a *App) home(w http.ResponseWriter, r *http.Request) {
	d := a.baseData(r, "", "")
	if !a.populateHomePosts(w, r, &d) {
		return
	}
	d.Tags, _ = a.store.PublicTags()
	if r.URL.Query().Get("feedback") == "saved" {
		d.Flash = "感谢反馈，我们已经收到。"
	}
	a.render(w, r, "home.html", d, 200)
}

func (a *App) homePosts(w http.ResponseWriter, r *http.Request) {
	d := a.baseData(r, "", "")
	if !a.populateHomePosts(w, r, &d) {
		return
	}
	a.render(w, r, "home_post_results", d, http.StatusOK)
}

func (a *App) populateHomePosts(w http.ResponseWriter, r *http.Request, d *viewData) bool {
	d.Query = strings.TrimSpace(r.URL.Query().Get("q"))
	d.ActiveTag = strings.TrimSpace(r.URL.Query().Get("tag"))
	if utf8.RuneCountInString(d.Query) > 100 {
		http.Error(w, "搜索词不能超过 100 个字符", http.StatusBadRequest)
		return false
	}
	if utf8.RuneCountInString(d.ActiveTag) > 200 {
		http.Error(w, "标签参数无效", http.StatusBadRequest)
		return false
	}
	d.Page = pageOf(r)
	posts, total, err := a.store.SearchPublicPosts(r.Context(), d.Query, d.ActiveTag, d.Page, d.Settings.PostsPerPage)
	if err != nil {
		http.Error(w, "文章加载失败", http.StatusInternalServerError)
		return false
	}
	d.Total = total
	d.TotalPages = pages(total, d.Settings.PostsPerPage)
	if d.Page > d.TotalPages {
		d.Page = d.TotalPages
		posts, _, err = a.store.SearchPublicPosts(r.Context(), d.Query, d.ActiveTag, d.Page, d.Settings.PostsPerPage)
		if err != nil {
			http.Error(w, "文章加载失败", http.StatusInternalServerError)
			return false
		}
	}
	d.Posts = posts
	return true
}

func (a *App) submitFeedback(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var err error
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		err = r.ParseMultipartForm(16 << 10)
	} else {
		err = r.ParseForm()
	}
	if err != nil {
		http.Error(w, "反馈内容过大", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(r.FormValue("website")) != "" {
		a.feedbackSuccess(w, r)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	contact := strings.TrimSpace(r.FormValue("contact"))
	content := strings.TrimSpace(r.FormValue("content"))
	if content == "" {
		http.Error(w, "请填写反馈内容", http.StatusBadRequest)
		return
	}
	if utf8.RuneCountInString(name) > 50 || utf8.RuneCountInString(contact) > 100 || utf8.RuneCountInString(content) > 1000 {
		http.Error(w, "反馈字段超过长度限制", http.StatusBadRequest)
		return
	}
	if !a.feedbackLimiter.Allow(clientIP(r)) {
		http.Error(w, "提交过于频繁，请稍后再试", http.StatusTooManyRequests)
		return
	}
	if _, err := a.store.AddFeedback(name, contact, content); err != nil {
		http.Error(w, "反馈提交失败", http.StatusInternalServerError)
		return
	}
	a.feedbackSuccess(w, r)
}

func (a *App) feedbackSuccess(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		return
	}
	http.Redirect(w, r, "/?feedback=saved#feedback", http.StatusSeeOther)
}
func (a *App) posts(w http.ResponseWriter, r *http.Request) { a.renderPostList(w, r, "", "") }
func (a *App) categoryPosts(w http.ResponseWriter, r *http.Request) {
	a.renderPostList(w, r, "category", chi.URLParam(r, "slug"))
}
func (a *App) tagPosts(w http.ResponseWriter, r *http.Request) {
	a.renderPostList(w, r, "tag", chi.URLParam(r, "slug"))
}
func (a *App) renderPostList(w http.ResponseWriter, r *http.Request, kind, slug string) {
	d := a.baseData(r, "文章", "浏览全部已发布文章")
	p := pageOf(r)
	posts, total, _ := a.store.ListPosts(r.Context(), true, kind, slug, p, d.Settings.PostsPerPage)
	d.Posts = posts
	d.Page = p
	d.TotalPages = pages(total, d.Settings.PostsPerPage)
	if kind != "" {
		d.FilterName = slug
	}
	if total == 0 && kind != "" {
		a.notFound(w, r)
		return
	}
	a.render(w, r, "posts.html", d, 200)
}
func (a *App) postDetail(w http.ResponseWriter, r *http.Request) {
	p, err := a.store.PublishedByPath(chi.URLParam(r, "slug"))
	if err != nil {
		a.notFound(w, r)
		return
	}
	desc := p.Summary
	if desc == "" {
		desc = p.Title
	}
	d := a.baseData(r, p.Title, desc)
	d.Post = &p
	payload, _ := json.Marshal(map[string]any{"@context": "https://schema.org", "@type": "BlogPosting", "headline": p.Title, "description": desc, "keywords": p.Keywords, "datePublished": p.PublishedAt, "dateModified": p.UpdatedAt, "author": map[string]string{"@type": "Person", "name": d.Settings.Author}})
	d.JSONLD = template.JS(payload)
	a.render(w, r, "post.html", d, 200)
}
func (a *App) notFound(w http.ResponseWriter, r *http.Request) {
	d := a.baseData(r, "页面未找到", "请求的页面不存在")
	a.render(w, r, "404.html", d, 404)
}

func (a *App) sitemap(w http.ResponseWriter, r *http.Request) {
	settings, _ := a.store.Settings()
	slugs, _ := a.store.PublishedSlugs()
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	fmt.Fprintf(w, "<?xml version=\"1.0\" encoding=\"UTF-8\"?><urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\"><url><loc>%s/</loc></url><url><loc>%s/posts</loc></url>", template.HTMLEscapeString(strings.TrimRight(settings.SiteURL, "/")), template.HTMLEscapeString(strings.TrimRight(settings.SiteURL, "/")))
	for _, s := range slugs {
		fmt.Fprintf(w, "<url><loc>%s/posts/%s</loc></url>", template.HTMLEscapeString(strings.TrimRight(settings.SiteURL, "/")), url.PathEscape(s))
	}
	io.WriteString(w, "</urlset>")
}
func (a *App) robots(w http.ResponseWriter, r *http.Request) {
	settings, _ := a.store.Settings()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "User-agent: *\nAllow: /\nDisallow: /admin/\nSitemap: %s/sitemap.xml\n", strings.TrimRight(settings.SiteURL, "/"))
}

func (a *App) signedLoginToken(value string) string {
	m := hmac.New(sha256.New, []byte(a.cfg.SessionSecret))
	m.Write([]byte(value))
	return value + "." + hex.EncodeToString(m.Sum(nil))
}
func (a *App) verifyLoginToken(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return false
	}
	expected := a.signedLoginToken(parts[0])
	return subtle.ConstantTimeCompare([]byte(token), []byte(expected)) == 1
}
func (a *App) loginPage(w http.ResponseWriter, r *http.Request) {
	raw, _ := randomHex(24)
	token := a.signedLoginToken(raw)
	http.SetCookie(w, &http.Cookie{Name: "login_csrf", Value: token, Path: "/admin/login", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 600})
	d := a.baseData(r, "管理员登录", "")
	d.CSRF = token
	if r.URL.Query().Get("error") != "" {
		d.Error = "用户名或密码错误"
	}
	a.render(w, r, "login.html", d, 200)
}
func (a *App) login(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("login_csrf")
	if err != nil || !a.verifyLoginToken(cookie.Value) || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(r.FormValue("csrf"))) != 1 {
		http.Error(w, "请求已过期", 403)
		return
	}
	ip := clientIP(r)
	if !a.limiter.Allow(ip) {
		http.Error(w, "尝试次数过多，请稍后再试", http.StatusTooManyRequests)
		return
	}
	admin, err := a.store.Authenticate(strings.TrimSpace(r.FormValue("username")), r.FormValue("password"))
	if err != nil {
		a.limiter.Fail(ip)
		http.Redirect(w, r, "/admin/login?error=1", 303)
		return
	}
	a.limiter.Success(ip)
	token, _, err := a.store.CreateSession(admin.ID)
	if err != nil {
		http.Error(w, "无法创建会话", 500)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil, MaxAge: 86400})
	http.Redirect(w, r, "/admin/", 303)
}
func (a *App) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("session")
		if err != nil {
			http.Redirect(w, r, "/admin/login", 303)
			return
		}
		s, err := a.store.GetSession(c.Value)
		if err != nil {
			http.Redirect(w, r, "/admin/login", 303)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey, s)))
	})
}
func (a *App) requireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			s, _ := r.Context().Value(sessionKey).(*Session)
			provided := r.Header.Get("X-CSRF-Token")
			if provided == "" {
				if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
					r.Body = http.MaxBytesReader(w, r.Body, 11<<20)
				}
				provided = r.FormValue("csrf")
			}
			if s == nil || subtle.ConstantTimeCompare([]byte(s.CSRF), []byte(provided)) != 1 {
				http.Error(w, "CSRF 校验失败", 403)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	if c, e := r.Cookie("session"); e == nil {
		_ = a.store.DeleteSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "session", Path: "/", MaxAge: -1, HttpOnly: true})
	http.Redirect(w, r, "/admin/login", 303)
}

func (a *App) dashboard(w http.ResponseWriter, r *http.Request) {
	d := a.baseData(r, "控制台", "")
	d.Stats, _ = a.store.Stats()
	d.Posts, _, _ = a.store.ListPosts(r.Context(), false, "", "", 1, 5)
	a.render(w, r, "dashboard.html", d, 200)
}
func (a *App) adminPosts(w http.ResponseWriter, r *http.Request) {
	d := a.baseData(r, "文章管理", "")
	d.Posts, _, _ = a.store.ListPosts(r.Context(), false, "", "", 1, 100)
	d.Categories, _ = a.store.Categories()
	d.Tags, _ = a.store.Tags()
	d.Covers, _ = a.store.Covers()
	d.Post = &Post{Status: "published", IsVisible: true}
	if r.URL.Query().Get("saved") == "1" {
		d.Flash = "文章已保存"
	} else if r.URL.Query().Get("cover_changed") == "1" {
		d.Flash = "文章封面已随机更换"
	}
	d.Error = r.URL.Query().Get("cover_error")
	if compose := r.URL.Query().Get("compose"); compose != "" {
		d.ComposerOpen = true
		if id, _ := strconv.ParseInt(compose, 10, 64); id > 0 {
			p, err := a.store.PostByID(id)
			if err != nil {
				a.notFound(w, r)
				return
			}
			d.Post = &p
			d.InfoOpen = r.URL.Query().Get("info") == "1"
		}
	}
	a.render(w, r, "admin_posts.html", d, 200)
}
func (a *App) editPostPage(w http.ResponseWriter, r *http.Request) {
	compose := chi.URLParam(r, "id")
	if compose == "" {
		compose = "new"
	}
	http.Redirect(w, r, "/admin/posts?compose="+url.QueryEscape(compose), http.StatusSeeOther)
}
func (a *App) savePost(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var old Post
	if id > 0 {
		old, _ = a.store.PostByID(id)
	}
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		http.Error(w, "标题不能为空", 400)
		return
	}
	keywords := strings.TrimSpace(r.FormValue("keywords"))
	slug := strings.TrimSpace(r.FormValue("slug"))
	if old.PublishedAt != nil {
		slug = old.Slug
	} else if keywordSlug := KeywordSlug(keywords); keywordSlug != "" {
		slug = UniqueKeywordSlug(keywordSlug, func(v string) bool { return a.store.SlugExists(v, id) })
	} else {
		slug = UniqueSlug(title, func(v string) bool { return a.store.SlugExists(v, id) })
	}
	html, err := a.markdown.Render(r.FormValue("markdown"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	summary := strings.TrimSpace(r.FormValue("summary"))
	if summary == "" {
		summary = PlainTextSummary(html, 60)
	}
	coverID := old.CoverID
	if values, present := r.Form["cover_id"]; present {
		coverID = nil
		if raw := strings.TrimSpace(values[0]); raw != "" {
			parsed, parseErr := strconv.ParseInt(raw, 10, 64)
			if parseErr != nil || parsed <= 0 {
				http.Error(w, "请选择有效的文章封面", http.StatusBadRequest)
				return
			}
			coverID = &parsed
		}
	}
	p := Post{ID: id, Title: title, Slug: slug, Summary: summary, Keywords: keywords, Markdown: r.FormValue("markdown"), HTML: html, Status: "published", IsVisible: r.FormValue("is_visible") == "1", CoverID: coverID, PublishedAt: old.PublishedAt}
	newCategory := strings.TrimSpace(r.FormValue("category_name"))
	if utf8.RuneCountInString(newCategory) > 80 {
		http.Error(w, "新分类名称不能超过 80 个字符", 400)
		return
	}
	newTags, err := parseNewTagNames(r.FormValue("new_tags"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if len(newTags) > 30 {
		http.Error(w, "每篇文章最多选择 30 个标签", 400)
		return
	}
	if err = a.store.SavePostWithTaxonomies(&p, nil, newCategory, newTags); err != nil {
		http.Error(w, "保存失败: "+err.Error(), 400)
		return
	}
	http.Redirect(w, r, "/admin/posts?saved=1", 303)
}

func (a *App) togglePostVisibility(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if id <= 0 {
		http.Error(w, "无效文章", http.StatusBadRequest)
		return
	}
	if err := a.store.SetPostVisibility(id, r.FormValue("is_visible") == "1"); err != nil {
		http.Error(w, "更新可见性失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/admin/posts", http.StatusSeeOther)
}

func (a *App) randomizePostCover(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if id <= 0 {
		http.Error(w, "无效文章", http.StatusBadRequest)
		return
	}
	if err := a.store.RandomizePostCover(id); err != nil {
		http.Redirect(w, r, "/admin/posts?cover_error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/posts?cover_changed=1", http.StatusSeeOther)
}

func parseNewTagNames(raw string) ([]string, error) {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，' || r == ';' || r == '；' || r == '\n' || r == '\r'
	})
	seen := make(map[string]struct{}, len(parts))
	names := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		if utf8.RuneCountInString(name) > 50 {
			return nil, errors.New("单个标签名称不能超过 50 个字符")
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		names = append(names, name)
	}
	if len(names) > 20 {
		return nil, errors.New("一次最多新建 20 个标签")
	}
	return names, nil
}
func (a *App) deletePost(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := a.store.DeletePost(id); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	http.Redirect(w, r, "/admin/posts", 303)
}
func (a *App) preview(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	html, err := a.markdown.Render(r.FormValue("markdown"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, html)
}

func (a *App) upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	file, header, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "请选择不超过 10 MB 的图片", 400)
		return
	}
	defer file.Close()
	path, err := a.saveImage(file, header)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": path})
}

func (a *App) coversPage(w http.ResponseWriter, r *http.Request) {
	d := a.baseData(r, "文章封面", "")
	d.Covers, _ = a.store.Covers()
	switch {
	case r.URL.Query().Get("saved") == "1":
		d.Flash = "封面已添加"
	case r.URL.Query().Get("deleted") == "1":
		d.Flash = "封面记录已移除，已上传文件仍保留"
	}
	d.Error = r.URL.Query().Get("error")
	a.render(w, r, "admin_covers.html", d, http.StatusOK)
}

func (a *App) adminFeedback(w http.ResponseWriter, r *http.Request) {
	d := a.baseData(r, "在线反馈", "")
	d.Page = pageOf(r)
	var err error
	d.Feedbacks, d.Total, err = a.store.Feedback(d.Page, 20)
	if err != nil {
		http.Error(w, "反馈加载失败", http.StatusInternalServerError)
		return
	}
	d.TotalPages = pages(d.Total, 20)
	if d.Page > d.TotalPages {
		d.Page = d.TotalPages
		d.Feedbacks, _, err = a.store.Feedback(d.Page, 20)
		if err != nil {
			http.Error(w, "反馈加载失败", http.StatusInternalServerError)
			return
		}
	}
	switch r.URL.Query().Get("status") {
	case "updated":
		d.Flash = "反馈状态已更新"
	case "deleted":
		d.Flash = "反馈已删除"
	}
	a.render(w, r, "admin_feedback.html", d, http.StatusOK)
}

func (a *App) setFeedbackRead(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if id <= 0 {
		http.Error(w, "无效反馈", http.StatusBadRequest)
		return
	}
	if err := a.store.SetFeedbackRead(id, r.FormValue("is_read") == "1"); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			a.notFound(w, r)
			return
		}
		http.Error(w, "状态更新失败", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/feedback?status=updated", http.StatusSeeOther)
}

func (a *App) deleteFeedback(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if id <= 0 {
		http.Error(w, "无效反馈", http.StatusBadRequest)
		return
	}
	if err := a.store.DeleteFeedback(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			a.notFound(w, r)
			return
		}
		http.Error(w, "反馈删除失败", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/feedback?status=deleted", http.StatusSeeOther)
}

func (a *App) addCover(w http.ResponseWriter, r *http.Request) {
	source := ""
	var coverURL string
	var err error
	var file multipart.File
	var header *multipart.FileHeader
	fileErr := http.ErrMissingFile
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		file, header, fileErr = r.FormFile("image")
	}
	switch {
	case fileErr == nil:
		source = "upload"
		defer file.Close()
		coverURL, err = a.saveImage(file, header)
	case errors.Is(fileErr, http.ErrMissingFile) && strings.TrimSpace(r.FormValue("external_url")) != "":
		source = "external"
		coverURL, err = validateExternalImageURL(r.FormValue("external_url"))
	case !errors.Is(fileErr, http.ErrMissingFile):
		err = errors.New("图片上传失败")
	default:
		err = errors.New("请选择图片、粘贴剪贴板图片，或填写图片 URL")
	}
	var cover Cover
	if err == nil {
		cover, err = a.store.AddCover(coverURL, source)
	}
	if err != nil {
		if strings.Contains(r.Header.Get("Accept"), "application/json") {
			http.Error(w, "添加失败："+err.Error(), http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, "/admin/covers?error="+url.QueryEscape("添加失败："+err.Error()), http.StatusSeeOther)
		return
	}
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cover)
		return
	}
	http.Redirect(w, r, "/admin/covers?saved=1", http.StatusSeeOther)
}

func validateExternalImageURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) > 2048 {
		return "", errors.New("外部链接不能超过 2048 个字符")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return "", errors.New("请输入有效的 HTTP 或 HTTPS 图片链接")
	}
	return parsed.String(), nil
}

func (a *App) deleteCover(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if id <= 0 {
		http.Error(w, "无效封面", http.StatusBadRequest)
		return
	}
	if err := a.store.DeleteCover(id); err != nil {
		http.Redirect(w, r, "/admin/covers?error="+url.QueryEscape("移除失败："+err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/covers?deleted=1", http.StatusSeeOther)
}
func (a *App) saveImage(file multipart.File, header *multipart.FileHeader) (string, error) {
	const maxImageSize int64 = 10 << 20
	head := make([]byte, 512)
	n, err := io.ReadFull(file, head)
	if err != nil && err != io.ErrUnexpectedEOF {
		return "", err
	}
	head = head[:n]
	mime := http.DetectContentType(head)
	exts := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}
	ext, ok := exts[mime]
	if !ok {
		return "", errors.New("仅支持 PNG、JPEG、GIF 或 WebP 图片")
	}
	name, err := randomHex(16)
	if err != nil {
		return "", err
	}
	now := time.Now()
	rel := filepath.Join(strconv.Itoa(now.Year()), fmt.Sprintf("%02d", now.Month()), name+ext)
	full := filepath.Join(a.cfg.UploadDir, rel)
	if err = os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return "", err
	}
	out, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return "", err
	}
	saved := false
	defer func() {
		_ = out.Close()
		if !saved {
			_ = os.Remove(full)
		}
	}()
	if _, err = out.Write(head); err != nil {
		return "", err
	}
	written, err := io.Copy(out, io.LimitReader(file, maxImageSize-int64(len(head))+1))
	if err != nil {
		return "", err
	}
	if int64(len(head))+written > maxImageSize {
		return "", errors.New("图片不能超过 10 MB")
	}
	_ = header
	saved = true
	return "/uploads/" + strings.ReplaceAll(rel, "\\", "/"), nil
}

func (a *App) settingsPage(w http.ResponseWriter, r *http.Request) {
	d := a.baseData(r, "网站设置", "")
	a.render(w, r, "settings.html", d, 200)
}
func (a *App) saveSettings(w http.ResponseWriter, r *http.Request) {
	pp, _ := strconv.Atoi(r.FormValue("posts_per_page"))
	if pp < 1 || pp > 100 {
		pp = 10
	}
	current, err := a.store.Settings()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	v := Settings{SiteTitle: strings.TrimSpace(r.FormValue("site_title")), Tagline: strings.TrimSpace(r.FormValue("tagline")), Description: strings.TrimSpace(r.FormValue("description")), Author: strings.TrimSpace(r.FormValue("author")), SiteURL: strings.TrimSpace(r.FormValue("site_url")), SEOKeywords: strings.TrimSpace(r.FormValue("seo_keywords")), FooterText: strings.TrimSpace(r.FormValue("footer_text")), SiteIcon: current.SiteIcon, PostsPerPage: pp}
	if v.SiteTitle == "" || v.SiteURL == "" {
		http.Error(w, "站名和站点 URL 必填", 400)
		return
	}
	file, header, fileErr := r.FormFile("site_icon")
	if fileErr == nil {
		defer file.Close()
		v.SiteIcon, err = a.saveImage(file, header)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
	} else if !errors.Is(fileErr, http.ErrMissingFile) {
		http.Error(w, "网站图标上传失败", 400)
		return
	} else if r.FormValue("remove_site_icon") == "1" {
		v.SiteIcon = ""
	}
	if err := a.store.SaveSettings(v); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/admin/settings?saved=1", 303)
}

type loginAttempt struct {
	fails        int
	blockedUntil time.Time
}

type submissionLimiter struct {
	mu     sync.Mutex
	items  map[string][]time.Time
	limit  int
	window time.Duration
}

func newSubmissionLimiter(limit int, window time.Duration) *submissionLimiter {
	return &submissionLimiter{items: make(map[string][]time.Time), limit: limit, window: window}
}

func (l *submissionLimiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-l.window)
	attempts := l.items[ip]
	kept := attempts[:0]
	for _, attempt := range attempts {
		if attempt.After(cutoff) {
			kept = append(kept, attempt)
		}
	}
	if len(kept) >= l.limit {
		l.items[ip] = kept
		return false
	}
	l.items[ip] = append(kept, time.Now())
	return true
}

type loginLimiter struct {
	mu    sync.Mutex
	items map[string]loginAttempt
}

func newLoginLimiter() *loginLimiter { return &loginLimiter{items: map[string]loginAttempt{}} }
func (l *loginLimiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return time.Now().After(l.items[ip].blockedUntil)
}
func (l *loginLimiter) Fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	v := l.items[ip]
	v.fails++
	if v.fails >= 5 {
		v.blockedUntil = time.Now().Add(5 * time.Minute)
		v.fails = 0
	}
	l.items[ip] = v
}
func (l *loginLimiter) Success(ip string) { l.mu.Lock(); delete(l.items, ip); l.mu.Unlock() }
func clientIP(r *http.Request) string {
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return host
}
