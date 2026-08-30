package api

type HealthInput struct{}

type HealthOutput struct {
	Body struct {
		OK bool `json:"ok"`
	}
}

type MessageOut struct {
	ID                int64          `json:"id"`
	SenderUsername    string         `json:"sender_username"`
	GroupName         string         `json:"group_name,omitempty"`
	RecipientUsername string         `json:"recipient_username,omitempty"`
	CustomGroupID     int64          `json:"custom_group_id,omitempty"`
	Content           string         `json:"content"`
	CreatedAt         string         `json:"created_at"`
	Attachment        *AttachmentOut `json:"attachment,omitempty"`
}

type AttachmentOut struct {
	ID       int    `json:"id"`
	Filename string `json:"filename"`
	Size     int64  `json:"size_bytes"`
}

type UserOut struct {
	Username string `json:"username"`
	Online   bool   `json:"online"`
}

type ActionOutput struct {
	Body struct {
		Success bool `json:"success"`
	}
}
