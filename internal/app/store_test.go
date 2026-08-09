package app

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.DB.Close() })
	return s
}

func TestStoreAdminSessionAndMigration(t *testing.T) {
	s := newTestStore(t)
	if err := s.EnsureAdmin("admin", "very-strong-password"); err != nil {
		t.Fatal(err)
	}
	admin, err := s.Authenticate("admin", "very-strong-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate("admin", "wrong-password"); err == nil {
		t.Fatal("wrong password accepted")
	}
	token, csrf, err := s.CreateSession(admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.GetSession(token)
	if err != nil || session.CSRF != csrf {
		t.Fatalf("session mismatch: %#v %v", session, err)
	}
	if err = s.DeleteSession(token); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetSession(token); err != sql.ErrNoRows {
		t.Fatalf("deleted session remains: %v", err)
	}
	var versions int
	if err = s.DB.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&versions); err != nil || versions != 4 {
		t.Fatalf("migration tracking failed: %d %v", versions, err)
	}
}

func TestStoreSiteIconSetting(t *testing.T) {
	s := newTestStore(t)
	settings, err := s.Settings()
	if err != nil || settings.SiteIcon != "" {
		t.Fatalf("unexpected default site icon: %q %v", settings.SiteIcon, err)
	}
	settings.SiteIcon = "/uploads/2026/08/icon.png"
	if err = s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	settings, err = s.Settings()
	if err != nil || settings.SiteIcon != "/uploads/2026/08/icon.png" {
		t.Fatalf("site icon was not saved: %q %v", settings.SiteIcon, err)
	}
}

func TestVisibilityMigrationPreservesLegacyPublishingState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`
		CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY);
		INSERT INTO schema_migrations(version) VALUES(1),(2);
		CREATE TABLE posts (id INTEGER PRIMARY KEY, status TEXT NOT NULL CHECK(status IN ('draft','published')));
		INSERT INTO posts(id,status) VALUES(1,'published'),(2,'draft');
	`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	rows, err := s.DB.Query("SELECT status,is_visible FROM posts ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var values []struct {
		status  string
		visible int
	}
	for rows.Next() {
		var value struct {
			status  string
			visible int
		}
		if err = rows.Scan(&value.status, &value.visible); err != nil {
			t.Fatal(err)
		}
		values = append(values, value)
	}
	if len(values) != 2 || values[0].status != "published" || values[0].visible != 1 || values[1].status != "published" || values[1].visible != 0 {
		t.Fatalf("legacy publishing state was not migrated: %#v", values)
	}
}

func TestStorePostLifecycleFiltersAndConstraints(t *testing.T) {
	s := newTestStore(t)
	if err := s.SaveTaxonomy("category", 0, "Go", "go"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTaxonomy("tag", 0, "SQLite", "sqlite"); err != nil {
		t.Fatal(err)
	}
	categories, _ := s.Categories()
	tags, _ := s.Tags()
	categoryID := categories[0].ID
	html := "<p>正文</p>"
	published := &Post{Title: "已发布", Slug: "published", Summary: "摘要", Markdown: "正文", HTML: html, Status: "published", IsVisible: true, CategoryID: &categoryID}
	if err := s.SavePost(published, []int64{tags[0].ID}); err != nil {
		t.Fatal(err)
	}
	hidden := &Post{Title: "隐藏文章", Slug: "hidden", Markdown: "隐藏", HTML: "<p>隐藏</p>", Status: "published", IsVisible: false}
	if err := s.SavePost(hidden, nil); err != nil {
		t.Fatal(err)
	}
	values, total, err := s.ListPosts(context.Background(), true, "", "", 1, 10)
	if err != nil || total != 1 || len(values) != 1 || values[0].Slug != "published" {
		t.Fatalf("public filtering failed: %#v %d %v", values, total, err)
	}
	values, total, err = s.ListPosts(context.Background(), true, "tag", "sqlite", 1, 1)
	if err != nil || total != 1 || len(values) != 1 {
		t.Fatalf("tag filtering failed: %#v %d %v", values, total, err)
	}
	if _, err = s.PublishedBySlug("hidden"); err != sql.ErrNoRows {
		t.Fatalf("hidden post leaked publicly: %v", err)
	}
	if err = s.SetPostVisibility(hidden.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublishedBySlug("hidden"); err != nil {
		t.Fatalf("visible post is unavailable: %v", err)
	}
	if err = s.DeleteTaxonomy("category", categoryID); err == nil {
		t.Fatal("referenced category deletion should fail")
	}
	if err = s.DeleteTaxonomy("tag", tags[0].ID); err == nil {
		t.Fatal("referenced tag deletion should fail")
	}
	if err = s.DeletePost(published.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteTaxonomy("category", categoryID); err != nil {
		t.Fatal(err)
	}
}

func TestSavePostWithInlineTaxonomiesIsAtomic(t *testing.T) {
	s := newTestStore(t)
	p := &Post{Title: "内联分类标签", Slug: "inline-taxonomies", Markdown: "正文", HTML: "<p>正文</p>", Status: "published", IsVisible: true}
	if err := s.SavePostWithTaxonomies(p, nil, "Go Web", []string{"Go", "SQLite", "go"}); err != nil {
		t.Fatal(err)
	}
	saved, err := s.PostByID(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.CategoryName != "Go Web" || len(saved.Tags) != 2 {
		t.Fatalf("inline taxonomies were not linked: %#v", saved)
	}

	failed := &Post{Title: "应回滚", Slug: "rollback-taxonomies", Markdown: "正文", HTML: "<p>正文</p>", Status: "published", IsVisible: true}
	if err := s.SavePostWithTaxonomies(failed, []int64{999999}, "不应保留", []string{"不应保留"}); err == nil {
		t.Fatal("invalid tag should make the transaction fail")
	}
	var count int
	if err := s.DB.QueryRow("SELECT COUNT(*) FROM categories WHERE name='不应保留'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("category was not rolled back: %d %v", count, err)
	}
	if err := s.DB.QueryRow("SELECT COUNT(*) FROM posts WHERE slug='rollback-taxonomies'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("post was not rolled back: %d %v", count, err)
	}
}
