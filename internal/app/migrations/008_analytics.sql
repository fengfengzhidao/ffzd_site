ALTER TABLE posts ADD COLUMN view_count INTEGER NOT NULL DEFAULT 0 CHECK(view_count >= 0);

CREATE TABLE site_view_totals (
  id INTEGER PRIMARY KEY CHECK(id = 1),
  view_count INTEGER NOT NULL DEFAULT 0 CHECK(view_count >= 0)
);
INSERT INTO site_view_totals(id, view_count) VALUES(1, 0);

CREATE TABLE site_daily_views (
  view_date TEXT PRIMARY KEY,
  view_count INTEGER NOT NULL DEFAULT 0 CHECK(view_count >= 0)
);

CREATE TABLE post_daily_views (
  post_id INTEGER NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  view_date TEXT NOT NULL,
  view_count INTEGER NOT NULL DEFAULT 0 CHECK(view_count >= 0),
  PRIMARY KEY(post_id, view_date)
);

CREATE INDEX idx_post_daily_views_date ON post_daily_views(view_date, post_id);
