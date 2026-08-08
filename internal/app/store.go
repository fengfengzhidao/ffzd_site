package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct{ DB *sql.DB }

func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{DB: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	if _, err := s.DB.Exec("CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)"); err != nil {
		return err
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		prefix := strings.SplitN(entry.Name(), "_", 2)[0]
		version, err := strconv.Atoi(prefix)
		if err != nil {
			return fmt.Errorf("无效迁移文件 %s", entry.Name())
		}
		var exists int
		if err = s.DB.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version=?", version).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return err
		}
		tx, err := s.DB.Begin()
		if err != nil {
			return err
		}
		if _, err = tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("执行迁移 %s: %w", entry.Name(), err)
		}
		if _, err = tx.Exec("INSERT INTO schema_migrations(version) VALUES(?)", version); err != nil {
			tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) EnsureAdmin(username, password string) error {
	var count int
	if err := s.DB.QueryRow("SELECT COUNT(*) FROM admins").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if username == "" || password == "" {
		return errors.New("首次启动需要设置 ADMIN_USERNAME 和 ADMIN_PASSWORD")
	}
	if len(password) < 12 {
		return errors.New("ADMIN_PASSWORD 至少需要 12 个字符")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec("INSERT INTO admins(username,password_hash) VALUES(?,?)", username, string(hash))
	return err
}

func (s *Store) Authenticate(username, password string) (*Admin, error) {
	var a Admin
	var hash string
	if err := s.DB.QueryRow("SELECT id,username,password_hash FROM admins WHERE username=?", username).Scan(&a.ID, &a.Username, &hash); err != nil {
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return nil, err
	}
	return &a, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	return hex.EncodeToString(b), err
}
func tokenHash(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }

func (s *Store) CreateSession(adminID int64) (string, string, error) {
	token, err := randomHex(32)
	if err != nil {
		return "", "", err
	}
	csrf, err := randomHex(24)
	if err != nil {
		return "", "", err
	}
	_, err = s.DB.Exec("INSERT INTO sessions(token_hash,admin_id,csrf_token,expires_at) VALUES(?,?,?,?)", tokenHash(token), adminID, csrf, time.Now().Add(24*time.Hour))
	return token, csrf, err
}
func (s *Store) GetSession(token string) (*Session, error) {
	var v Session
	err := s.DB.QueryRow(`SELECT s.admin_id,a.username,s.csrf_token,s.expires_at FROM sessions s JOIN admins a ON a.id=s.admin_id WHERE s.token_hash=? AND s.expires_at>?`, tokenHash(token), time.Now()).Scan(&v.AdminID, &v.Username, &v.CSRF, &v.ExpiresAt)
	return &v, err
}
func (s *Store) DeleteSession(token string) error {
	_, err := s.DB.Exec("DELETE FROM sessions WHERE token_hash=?", tokenHash(token))
	return err
}

func (s *Store) Settings() (Settings, error) {
	rows, err := s.DB.Query("SELECT key,value FROM site_settings")
	if err != nil {
		return Settings{}, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return Settings{}, err
		}
		m[k] = v
	}
	pp, _ := strconv.Atoi(m["posts_per_page"])
	if pp < 1 || pp > 100 {
		pp = 10
	}
	return Settings{
		SiteTitle:    m["site_title"],
		Tagline:      m["tagline"],
		Description:  m["description"],
		Author:       m["author"],
		SiteURL:      m["site_url"],
		SEOKeywords:  m["seo_keywords"],
		FooterText:   m["footer_text"],
		SiteIcon:     m["site_icon"],
		PostsPerPage: pp,
	}, rows.Err()
}
func (s *Store) SaveSettings(v Settings) error {
	items := map[string]string{"site_title": v.SiteTitle, "tagline": v.Tagline, "description": v.Description, "author": v.Author, "site_url": strings.TrimRight(v.SiteURL, "/"), "seo_keywords": v.SEOKeywords, "footer_text": v.FooterText, "site_icon": v.SiteIcon, "posts_per_page": strconv.Itoa(v.PostsPerPage)}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, val := range items {
		if _, err = tx.Exec("INSERT INTO site_settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", k, val); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func scanPost(row interface{ Scan(...any) error }) (Post, error) {
	var p Post
	var catID sql.NullInt64
	var catName, catSlug sql.NullString
	var pub sql.NullTime
	err := row.Scan(&p.ID, &p.Title, &p.Slug, &p.Summary, &p.Markdown, &p.HTML, &p.Status, &catID, &catName, &catSlug, &pub, &p.CreatedAt, &p.UpdatedAt)
	if catID.Valid {
		p.CategoryID = &catID.Int64
	}
	p.CategoryName = catName.String
	p.CategorySlug = catSlug.String
	if pub.Valid {
		p.PublishedAt = &pub.Time
	}
	return p, err
}

const postSelect = `SELECT p.id,p.title,p.slug,p.summary,p.markdown,p.html,p.status,p.category_id,c.name,c.slug,p.published_at,p.created_at,p.updated_at FROM posts p LEFT JOIN categories c ON c.id=p.category_id`

func (s *Store) PostByID(id int64) (Post, error) {
	p, e := scanPost(s.DB.QueryRow(postSelect+" WHERE p.id=?", id))
	if e == nil {
		p.Tags, _ = s.PostTags(id)
	}
	return p, e
}
func (s *Store) PublishedBySlug(slug string) (Post, error) {
	p, e := scanPost(s.DB.QueryRow(postSelect+" WHERE p.slug=? AND p.status='published'", slug))
	if e == nil {
		p.Tags, _ = s.PostTags(p.ID)
	}
	return p, e
}

func (s *Store) ListPosts(ctx context.Context, public bool, filterKind, filterSlug string, page, perPage int) ([]Post, int, error) {
	where := []string{"1=1"}
	args := []any{}
	joins := ""
	if public {
		where = append(where, "p.status='published'")
	}
	if filterKind == "category" {
		where = append(where, "c.slug=?")
		args = append(args, filterSlug)
	}
	if filterKind == "tag" {
		joins = " JOIN post_tags ptf ON ptf.post_id=p.id JOIN tags tf ON tf.id=ptf.tag_id "
		where = append(where, "tf.slug=?")
		args = append(args, filterSlug)
	}
	cond := strings.Join(where, " AND ")
	var total int
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(DISTINCT p.id) FROM posts p LEFT JOIN categories c ON c.id=p.category_id "+joins+" WHERE "+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := postSelect + joins + " WHERE " + cond + " GROUP BY p.id ORDER BY CASE WHEN p.published_at IS NULL THEN p.updated_at ELSE p.published_at END DESC LIMIT ? OFFSET ?"
	args = append(args, perPage, (page-1)*perPage)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Post
	for rows.Next() {
		p, e := scanPost(rows)
		if e != nil {
			return nil, 0, e
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if err := rows.Close(); err != nil {
		return nil, 0, err
	}
	for i := range out {
		out[i].Tags, _ = s.PostTags(out[i].ID)
	}
	return out, total, nil
}

func (s *Store) SavePost(p *Post, tagIDs []int64) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := savePostTx(tx, p, tagIDs); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SavePostWithTaxonomies(p *Post, tagIDs []int64, newCategory string, newTags []string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if newCategory != "" {
		categoryID, err := ensureTaxonomyTx(tx, "categories", newCategory)
		if err != nil {
			return err
		}
		p.CategoryID = &categoryID
	}

	seen := make(map[int64]struct{}, len(tagIDs)+len(newTags))
	allTagIDs := make([]int64, 0, len(tagIDs)+len(newTags))
	for _, id := range tagIDs {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			allTagIDs = append(allTagIDs, id)
		}
	}
	for _, name := range newTags {
		tagID, err := ensureTaxonomyTx(tx, "tags", name)
		if err != nil {
			return err
		}
		if _, ok := seen[tagID]; !ok {
			seen[tagID] = struct{}{}
			allTagIDs = append(allTagIDs, tagID)
		}
	}

	if err := savePostTx(tx, p, allTagIDs); err != nil {
		return err
	}
	return tx.Commit()
}

func ensureTaxonomyTx(tx *sql.Tx, table, name string) (int64, error) {
	if table != "categories" && table != "tags" {
		return 0, errors.New("无效的分类类型")
	}
	name = strings.TrimSpace(name)
	var id int64
	err := tx.QueryRow(fmt.Sprintf("SELECT id FROM %s WHERE name=? COLLATE NOCASE", table), name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	base := Slugify(name)
	if base == "" {
		return 0, errors.New("分类或标签名称无法生成有效 slug")
	}
	slug := base
	for suffix := 2; ; suffix++ {
		var count int
		if err := tx.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE slug=?", table), slug).Scan(&count); err != nil {
			return 0, err
		}
		if count == 0 {
			break
		}
		slug = fmt.Sprintf("%s-%d", base, suffix)
	}
	result, err := tx.Exec(fmt.Sprintf("INSERT INTO %s(name,slug) VALUES(?,?)", table), name, slug)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func savePostTx(tx *sql.Tx, p *Post, tagIDs []int64) error {
	var pub any = p.PublishedAt
	if p.Status == "published" && p.PublishedAt == nil {
		now := time.Now()
		p.PublishedAt = &now
		pub = now
	}
	if p.ID == 0 {
		r, e := tx.Exec(`INSERT INTO posts(title,slug,summary,markdown,html,status,category_id,published_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, p.Title, p.Slug, p.Summary, p.Markdown, p.HTML, p.Status, p.CategoryID, pub, time.Now())
		if e != nil {
			return e
		}
		p.ID, _ = r.LastInsertId()
	} else {
		_, err := tx.Exec(`UPDATE posts SET title=?,slug=?,summary=?,markdown=?,html=?,status=?,category_id=?,published_at=?,updated_at=? WHERE id=?`, p.Title, p.Slug, p.Summary, p.Markdown, p.HTML, p.Status, p.CategoryID, pub, time.Now(), p.ID)
		if err != nil {
			return err
		}
	}
	if _, err := tx.Exec("DELETE FROM post_tags WHERE post_id=?", p.ID); err != nil {
		return err
	}
	for _, tid := range tagIDs {
		if _, err := tx.Exec("INSERT INTO post_tags(post_id,tag_id) VALUES(?,?)", p.ID, tid); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) DeletePost(id int64) error {
	_, err := s.DB.Exec("DELETE FROM posts WHERE id=?", id)
	return err
}
func (s *Store) SlugExists(slug string, except int64) bool {
	var n int
	s.DB.QueryRow("SELECT COUNT(*) FROM posts WHERE slug=? AND id<>?", slug, except).Scan(&n)
	return n > 0
}
func (s *Store) PostTags(id int64) ([]Tag, error) {
	rows, e := s.DB.Query("SELECT t.id,t.name,t.slug FROM tags t JOIN post_tags pt ON pt.tag_id=t.id WHERE pt.post_id=? ORDER BY t.name", id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var v []Tag
	for rows.Next() {
		var x Tag
		if e := rows.Scan(&x.ID, &x.Name, &x.Slug); e != nil {
			return nil, e
		}
		v = append(v, x)
	}
	return v, rows.Err()
}

func (s *Store) Categories() ([]Category, error) {
	rows, e := s.DB.Query(`SELECT c.id,c.name,c.slug,COUNT(p.id) FROM categories c LEFT JOIN posts p ON p.category_id=c.id GROUP BY c.id ORDER BY c.name`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var v []Category
	for rows.Next() {
		var x Category
		if e := rows.Scan(&x.ID, &x.Name, &x.Slug, &x.Count); e != nil {
			return nil, e
		}
		v = append(v, x)
	}
	return v, rows.Err()
}
func (s *Store) Tags() ([]Tag, error) {
	rows, e := s.DB.Query(`SELECT t.id,t.name,t.slug,COUNT(pt.post_id) FROM tags t LEFT JOIN post_tags pt ON pt.tag_id=t.id GROUP BY t.id ORDER BY t.name`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var v []Tag
	for rows.Next() {
		var x Tag
		if e := rows.Scan(&x.ID, &x.Name, &x.Slug, &x.Count); e != nil {
			return nil, e
		}
		v = append(v, x)
	}
	return v, rows.Err()
}

func (s *Store) PublicCategories() ([]Category, error) {
	rows, err := s.DB.Query(`SELECT c.id,c.name,c.slug,COUNT(p.id) FROM categories c LEFT JOIN posts p ON p.category_id=c.id AND p.status='published' GROUP BY c.id ORDER BY c.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []Category
	for rows.Next() {
		var v Category
		if err := rows.Scan(&v.ID, &v.Name, &v.Slug, &v.Count); err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, rows.Err()
}

func (s *Store) PublicTags() ([]Tag, error) {
	rows, err := s.DB.Query(`SELECT t.id,t.name,t.slug,COUNT(p.id) FROM tags t LEFT JOIN post_tags pt ON pt.tag_id=t.id LEFT JOIN posts p ON p.id=pt.post_id AND p.status='published' GROUP BY t.id ORDER BY t.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []Tag
	for rows.Next() {
		var v Tag
		if err := rows.Scan(&v.ID, &v.Name, &v.Slug, &v.Count); err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, rows.Err()
}
func (s *Store) SaveTaxonomy(kind string, id int64, name, slug string) error {
	table := "categories"
	if kind == "tag" {
		table = "tags"
	}
	if id == 0 {
		_, e := s.DB.Exec(fmt.Sprintf("INSERT INTO %s(name,slug) VALUES(?,?)", table), name, slug)
		return e
	}
	_, e := s.DB.Exec(fmt.Sprintf("UPDATE %s SET name=?,slug=? WHERE id=?", table), name, slug, id)
	return e
}
func (s *Store) DeleteTaxonomy(kind string, id int64) error {
	table := "categories"
	if kind == "tag" {
		table = "tags"
	}
	_, e := s.DB.Exec(fmt.Sprintf("DELETE FROM %s WHERE id=?", table), id)
	return e
}
func (s *Store) Stats() (DashboardStats, error) {
	var v DashboardStats
	e := s.DB.QueryRow(`SELECT COUNT(*),COALESCE(SUM(CASE WHEN status='draft' THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN status='published' THEN 1 ELSE 0 END),0) FROM posts`).Scan(&v.Total, &v.Drafts, &v.Published)
	return v, e
}
func (s *Store) PublishedSlugs() ([]string, error) {
	rows, e := s.DB.Query("SELECT slug FROM posts WHERE status='published' ORDER BY published_at DESC")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var v []string
	for rows.Next() {
		var x string
		rows.Scan(&x)
		v = append(v, x)
	}
	sort.Strings(v)
	return v, rows.Err()
}
