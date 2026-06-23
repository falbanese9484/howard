package types

type Provider interface {
	Chat(c *BusConnector)
	GenerateRequest(prompt string) *ChatRequest
	RetrieveModels() ([]Model, error)
	SetModel(model string)
	// Reset clears the conversation history, restoring the provider to its
	// initial state (system prompt only).
	Reset()
}
