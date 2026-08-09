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
	ViewCount                                              int64
	PublishedAt                                            *time.Time
	CreatedAt, UpdatedAt                                   time.Time
}

type Cover struct {
	ID        int64
	URL       string
	Source    string
	CreatedAt time.Time
}

type Topic struct {
	ID            int64
	Name, Slug    string
	CoverID       *int64
	CoverURL      string
	DocumentCount int
	ViewCount     int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type TopicNode struct {
	ID, TopicID                int64
	ParentID, PostID           *int64
	Title, PostTitle, PostPath string
	SortOrder, Depth           int
	Post                       *Post
	CreatedAt, UpdatedAt       time.Time
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

type Feedback struct {
	ID                     int64
	Name, Contact, Content string
	IsRead                 bool
	CreatedAt              time.Time
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

type ViewSummary struct {
	Total, Today, Last7, Last30 int64
}

type DailyView struct {
	Date, Label string
	Count       int64
	Percent     int
}

type PostViewStats struct {
	ID                  int64
	Title, Status       string
	IsVisible           bool
	Today, Last7, Total int64
}

type TopicViewStats struct {
	ID                  int64
	Name                string
	DocumentCount       int
	Today, Last7, Total int64
}

type AnalyticsStats struct {
	Summary ViewSummary
	Daily   []DailyView
	Posts   []PostViewStats
	Topics  []TopicViewStats
}
