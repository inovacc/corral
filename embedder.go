package agents

import "context"

// Embedder converts text strings into fixed-dimension float32 vectors. It is the
// seam the runtime uses for optional semantic recall; the host application
// supplies an implementation (for example OpenAIEmbedder, or its own).
type Embedder interface {
	// Embed converts a slice of text strings to their vector representations,
	// preserving input order.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Dim returns the dimensionality of the embeddings.
	Dim() int
	// Name returns the name of the embedder (e.g. "openai:text-embedding-3-small").
	Name() string
}
