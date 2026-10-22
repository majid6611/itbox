package api

type HealthInput struct{}

type HealthOutput struct {
	Body struct {
		OK bool `json:"ok"`
	}
}

type Extension struct {
	Number      string  `json:"number"`
	Name        string  `json:"name"`
	Username    *string `json:"username"`
	Password    string  `json:"password"`
	InMainGroup bool    `json:"in_main_group"`
	// State is "online", "in_call", "offline", or "unknown" (PBX not
	// answering, so there's no way to tell).
	State string `json:"state"`
}

type Settings struct {
	TrunkEnabled       bool   `json:"trunk_enabled"`
	TrunkHost          string `json:"trunk_host"`
	TrunkUsername      string `json:"trunk_username"`
	TrunkPasswordSet   bool   `json:"trunk_password_set"`
	TrunkCallerID      string `json:"trunk_caller_id"`
	AllowInternational bool   `json:"allow_international"`
	// TrunkStatus is "off", "connected", or "not_connected".
	TrunkStatus string `json:"trunk_status"`
}

type Call struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Start       string `json:"start"`
	TalkSeconds int    `json:"talk_seconds"`
	// Result is "answered", "voicemail", "missed", "busy", or "failed".
	Result string `json:"result"`
}

type DirectoryEntry struct {
	Number string `json:"number"`
	Name   string `json:"name"`
}

type ActionOutput struct {
	Body struct {
		Success bool `json:"success"`
	}
}
