ALTER TABLE posts ADD COLUMN is_visible INTEGER NOT NULL DEFAULT 1 CHECK(is_visible IN (0,1));
UPDATE posts SET is_visible = CASE WHEN status = 'published' THEN 1 ELSE 0 END;
UPDATE posts SET status = 'published';
