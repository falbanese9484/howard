package types

type Provider interface {
	Chat(c *BusConnector)
	GenerateRequest(prompt string) *ChatRequest
	RetrieveModels() ([]Model, error)
	SetModel(model string)
	// ResetConversation clears turn history while preserving the system prompt and settings.
	// It returns an error if a turn is still active.
	ResetConversation() error
}
