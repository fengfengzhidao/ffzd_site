package app

import "time"

type Post struct {
	ID                                                     int64
	Title, Slug, Summary, Keywords, Markdown, HTML, Status string
	IsVisible                                              bool
	CategoryID                                             *int64
	CategoryName, CategorySlug                             string
	CoverID                                                *int64
	CoverURL                                               string
	Tags                                                   []Tag
	PublishedAt                                            *time.Time
	CreatedAt, UpdatedAt                                   time.Time
}

type Cover struct {
	ID        int64
	URL       string
	Source    string
	CreatedAt time.Time
}

type Category struct {
	ID         int64
	Name, Slug string
	Count      int
}
type Tag struct {
	ID         int64
	Name, Slug string
	Count      int
}
type Admin struct {
	ID       int64
	Username string
}
type Session struct {
	AdminID        int64
	Username, CSRF string
	ExpiresAt      time.Time
}

type Settings struct {
	SiteTitle, Tagline, Description, Author, SiteURL, SEOKeywords, FooterText, SiteIcon string
	PostsPerPage                                                                        int
}

type DashboardStats struct{ Total, Hidden, Visible int }
