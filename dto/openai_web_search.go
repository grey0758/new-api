package dto

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// OpenAIWebSearchRequest is the bounded request envelope used by Codex's
// standalone POST /v1/alpha/search endpoint. The nested protocol objects stay
// raw so NewAPI can preserve official search command semantics byte-for-byte.
type OpenAIWebSearchRequest struct {
	ID              string          `json:"id"`
	Model           string          `json:"model"`
	Reasoning       *Reasoning      `json:"reasoning,omitempty"`
	Input           json.RawMessage `json:"input,omitempty"`
	Commands        json.RawMessage `json:"commands,omitempty"`
	Settings        json.RawMessage `json:"settings,omitempty"`
	MaxOutputTokens *uint64         `json:"max_output_tokens,omitempty"`
}

type OpenAIWebSearchResponse struct {
	EncryptedOutput *string `json:"encrypted_output"`
	Output          string  `json:"output"`
}

func (r *OpenAIWebSearchRequest) GetTokenCountMeta() *types.TokenCountMeta {
	parts := make([]string, 0, 4)
	if len(r.Input) > 0 {
		parts = append(parts, string(r.Input))
	}
	if len(r.Commands) > 0 {
		parts = append(parts, string(r.Commands))
	}
	if len(r.Settings) > 0 {
		parts = append(parts, string(r.Settings))
	}
	return &types.TokenCountMeta{CombineText: strings.Join(parts, "\n")}
}

func (r *OpenAIWebSearchRequest) IsStream(c *gin.Context) bool {
	return false
}

func (r *OpenAIWebSearchRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}
