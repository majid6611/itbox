-- Every statement here is IF NOT EXISTS — this module reuses the platform's
-- shared Postgres (see db.go's Migrate, same schema_migrations tracking
-- table core uses) rather than its own database. On a deployment that
-- already had these tables from before this module existed (created by the
-- core backend's old migration 0009_wiki.sql), this is a no-op; on a fresh
-- one, it's what actually creates them. Either way "install creates the
-- tables if they don't exist" holds.

CREATE TABLE IF NOT EXISTS wiki_pages (
    id SERIAL PRIMARY KEY,
    -- e.g. "engineering/onboarding/setup-guide" — the category/sub/page
    -- hierarchy is just this path, split for display; there's no separate
    -- tree structure to keep in sync.
    path TEXT UNIQUE NOT NULL,
    title TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Every save creates a new row here rather than updating wiki_pages in
-- place — that's the entire "change history" feature, no separate
-- versioning mechanism needed. The current version is just the latest row
-- for a page_id.
CREATE TABLE IF NOT EXISTS wiki_revisions (
    id SERIAL PRIMARY KEY,
    page_id INT NOT NULL REFERENCES wiki_pages(id) ON DELETE CASCADE,
    content TEXT NOT NULL, -- HTML, produced by the Tiptap/ProseMirror editor
    author TEXT NOT NULL,  -- LDAP username, or the admin's email
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Per-page access, not per-category: a page with zero rows here is open
-- (read+write) to every logged-in employee, matching "no config needed
-- unless you actually want to restrict something." A page with rows
-- restricts to just the listed groups, at the given access level.
CREATE TABLE IF NOT EXISTS wiki_permissions (
    id SERIAL PRIMARY KEY,
    page_id INT NOT NULL REFERENCES wiki_pages(id) ON DELETE CASCADE,
    group_name TEXT NOT NULL,
    access TEXT NOT NULL, -- 'read' | 'write' (write implies read)
    UNIQUE (page_id, group_name)
);

CREATE TABLE IF NOT EXISTS wiki_attachments (
    id SERIAL PRIMARY KEY,
    page_id INT NOT NULL REFERENCES wiki_pages(id) ON DELETE CASCADE,
    filename TEXT NOT NULL,
    s3_key TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    uploaded_by TEXT NOT NULL,
    uploaded_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
