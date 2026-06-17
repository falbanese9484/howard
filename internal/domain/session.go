package domain

import "time"

type Session struct {
	ID        string
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Message struct {
	ID        string
	SessionID string
	Role      string
	Content   string
	Thinking  string
	CreatedAt time.Time
}

type ToolCall struct {
	ID        string
	MessageID string
	Name      string
	Arguments string
	Result    string
	Error     string
	CreatedAt time.Time
}

type ProviderConfig struct {
	Name  string
	Model string
}

type Model struct {
	Name     string
	Provider string
}
