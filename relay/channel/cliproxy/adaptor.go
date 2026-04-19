package cliproxy

import "github.com/QuantumNous/new-api/relay/channel/openai"

const ChannelName = "cliproxy"

// Adaptor intentionally reuses the OpenAI-compatible request/response path.
// CLIProxy sits behind an OpenAI-style API surface, so quota, pricing, and
// usage extraction should stay on the existing OpenAI billing pipeline.
type Adaptor struct {
	openai.Adaptor
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
