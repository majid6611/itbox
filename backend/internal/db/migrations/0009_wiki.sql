-- Employee (LDAP user) sessions — deliberately separate from the admin
-- panel's own `sessions`/`admin_users` tables and its own cookie, per
-- explicit request: employees never share the admin's login at all.
CREATE TABLE employee_sessions (
    token TEXT PRIMARY KEY,
    username TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE wiki_pages (
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
-- versioning mechanism needed. The current version is just the latest
-- row for a page_id.
CREATE TABLE wiki_revisions (
    id SERIAL PRIMARY KEY,
    page_id INT NOT NULL REFERENCES wiki_pages(id) ON DELETE CASCADE,
    content TEXT NOT NULL, -- Markdown
    author TEXT NOT NULL,  -- LDAP username, or the admin's email
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Per-page access, not per-category: a page with zero rows here is open
-- (read+write) to every logged-in employee, matching "no config needed
-- unless you actually want to restrict something." A page with rows
-- restricts to just the listed groups, at the given access level.
CREATE TABLE wiki_permissions (
    id SERIAL PRIMARY KEY,
    page_id INT NOT NULL REFERENCES wiki_pages(id) ON DELETE CASCADE,
    group_name TEXT NOT NULL,
    access TEXT NOT NULL, -- 'read' | 'write' (write implies read)
    UNIQUE (page_id, group_name)
);

CREATE TABLE wiki_attachments (
    id SERIAL PRIMARY KEY,
    page_id INT NOT NULL REFERENCES wiki_pages(id) ON DELETE CASCADE,
    filename TEXT NOT NULL,
    s3_key TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    uploaded_by TEXT NOT NULL,
    uploaded_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
