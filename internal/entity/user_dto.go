package entity

type UserDTO struct {
	ID uint `json:"id"`
	// UID 是面向用户的业务编号（门牌号），可公开，用于展示与分享。
	UID           uint64 `json:"uid"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	Link          string `json:"link"`
	BadgeName     string `json:"badge_name"`
	BadgeColor    string `json:"badge_color"`
	IsAdmin       bool   `json:"is_admin"`
	IsAIAssistant bool   `json:"is_ai_assistant,omitempty"`
	HasPassword   bool   `json:"has_password"`
	ReceiveEmail  bool   `json:"receive_email"`
	// Points 只在本人登录查询时填充，匿名查询恒为 0。
	Points int64 `json:"points"`
}
