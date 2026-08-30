-- Employee (LDAP user) sessions — deliberately separate from the admin
-- panel's own `sessions`/`admin_users` tables and its own cookie, per
-- explicit request: employees never share the admin's login at all.
--
-- This file originally also created the wiki_pages/wiki_revisions/
-- wiki_permissions/wiki_attachments tables, back when the wiki was a
-- built-in core feature. It's since been extracted into its own module
-- (modules/wiki/service) that owns those tables and creates them itself
-- on install — see that module's own migrations. Trimming this file to
-- just what core still actually owns (employee identity) is safe despite
-- normally never editing an applied migration: every real deployment that
-- already ran the original version already has all five tables from back
-- then, and this filename is already recorded as applied there, so this
-- edit only changes what a genuinely fresh install gets — it won't create
-- wiki tables on a system that never installs the wiki module.
CREATE TABLE employee_sessions (
    token TEXT PRIMARY KEY,
    username TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);
