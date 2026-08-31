package dto

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// OpenAIMemorySummarizeRequest mirrors Codex's unary
// POST /v1/memories/trace_summarize wire contract.
type OpenAIMemorySummarizeRequest struct {
	Model     string                 `json:"model"`
	Traces    []OpenAIRawMemoryTrace `json:"traces"`
	Reasoning *Reasoning             `json:"reasoning,omitempty"`
}

type OpenAIRawMemoryTrace struct {
	ID       string                  `json:"id"`
	Metadata OpenAIRawMemoryMetadata `json:"metadata"`
	Items    []json.RawMessage       `json:"items"`
}

type OpenAIRawMemoryMetadata struct {
	SourcePath string `json:"source_path"`
}

type OpenAIMemorySummarizeOutput struct {
	TraceSummary  string `json:"trace_summary"`
	MemorySummary string `json:"memory_summary"`
}

type OpenAIMemorySummarizeResponse struct {
	Output []OpenAIMemorySummarizeOutput `json:"output"`
	Usage  *Usage                        `json:"usage,omitempty"`
}

func (r *OpenAIMemorySummarizeRequest) GetTokenCountMeta() *types.TokenCountMeta {
	parts := make([]string, 0, len(r.Traces))
	for _, trace := range r.Traces {
		for _, item := range trace.Items {
			parts = append(parts, string(item))
		}
	}
	return &types.TokenCountMeta{CombineText: strings.Join(parts, "\n")}
}

func (r *OpenAIMemorySummarizeRequest) IsStream(c *gin.Context) bool {
	return false
}

func (r *OpenAIMemorySummarizeRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}
