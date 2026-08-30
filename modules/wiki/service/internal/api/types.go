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
}

type WikiPage struct {
	ID        int    `json:"id"`
	Path      string `json:"path"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	CanWrite  bool   `json:"can_write"`
	UpdatedAt string `json:"updated_at"`
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
