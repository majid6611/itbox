package api

import "github.com/microcosm-cc/bluemonday"

// wikiContentPolicy allow-lists exactly the HTML the wiki editor (Tiptap
// StarterKit plus the Link/Image/Underline/Table extensions — see
// frontend/src/views/portal/Wiki.vue's contentExtensions) can actually
// produce, and nothing else.
//
// This is a backstop, not the primary defense: content is only ever
// rendered back through ProseMirror's own schema-constrained parser, never
// v-html/innerHTML (see that file's own comments on why that's what
// actually stops stored XSS today). But that means safety currently
// depends on every future render path remembering to go through
// ProseMirror too — sanitizing here as well means a render path that
// forgets doesn't reopen stored XSS on every wiki page ever saved.
var wikiContentPolicy = newWikiContentPolicy()

func newWikiContentPolicy() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowElements(
		"p", "br", "hr", "blockquote", "pre", "code",
		"strong", "b", "em", "i", "s", "strike", "del", "u",
		"h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "li",
	)
	p.AllowStandardURLs()
	p.AllowAttrs("href").OnElements("a")
	p.AddTargetBlankToFullyQualifiedLinks(true)
	p.AllowImages()
	p.AllowTables()
	return p
}

// sanitizeWikiContent strips anything outside the editor's own schema —
// script tags, event handler attributes, iframes, unknown elements — before
// a revision is ever written to the database.
func sanitizeWikiContent(html string) string {
	return wikiContentPolicy.Sanitize(html)
}
