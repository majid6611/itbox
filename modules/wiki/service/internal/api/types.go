package api

type HealthInput struct{}

type HealthOutput struct {
	Body struct {
		OK bool `json:"ok"`
	}
}

type WikiPageSummary struct {
	ID    int    `json:"id"`
	Path  string `json:"path"`
	Title string `json:"title"`
	// Snippet is set only on search results: the matching text, with the
	// matched words wrapped in << >>.
	Snippet string `json:"snippet,omitempty"`
}

// WikiRecent is one entry of the "recently changed" list.
type WikiRecent struct {
	ID        int    `json:"id"`
	Path      string `json:"path"`
	Title     string `json:"title"`
	Author    string `json:"author"`
	UpdatedAt string `json:"updated_at"`
}

type WikiPage struct {
	ID        int    `json:"id"`
	Path      string `json:"path"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	CanWrite  bool   `json:"can_write"`
	UpdatedAt string `json:"updated_at"`
	UpdatedBy string `json:"updated_by"`
	// RevisionID is the revision this content came from. A save sends it
	// back as base_revision so the server can tell if someone else saved
	// in the meantime.
	RevisionID int `json:"revision_id"`
}

type WikiRevision struct {
	ID        int    `json:"id"`
	Author    string `json:"author"`
	CreatedAt string `json:"created_at"`
}

type WikiAttachment struct {
	ID       int    `json:"id"`
	Filename string `json:"filename"`
	Size     int64  `json:"size_bytes"`
}

type WikiPermissionRule struct {
	Group  string `json:"group"`
	Access string `json:"access"` // "read" or "write"
}

type ActionOutput struct {
	Body struct {
		Success bool `json:"success"`
	}
}
