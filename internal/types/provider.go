package types

type Provider interface {
	Chat(c *BusConnector)
	GenerateRequest(prompt string) *ChatRequest
	RetrieveModels() ([]Model, error)
	SetModel(model string)
}
