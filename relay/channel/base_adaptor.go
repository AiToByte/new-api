package channel

import (
	"errors"
	"io"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
)

// BaseAdaptor provides the default Adaptor methods that are identical across
// providers. Embed it in a provider adaptor and implement only the methods
// that carry provider-specific behavior:
//
//	type Adaptor struct {
//		channel.BaseAdaptor
//	}
//
// A deleted method below keeps its exact previous behavior; the defaults are
// byte-for-byte equivalent to the stubs they replace.
//
// Deliberately NOT defaulted (a silent default here would be a footgun):
//   - ConvertOpenAIRequest / GetRequestURL / SetupRequestHeader / DoRequest /
//     DoResponse: core transport and auth paths. The compiler must force every
//     provider to write them explicitly; inheriting an OpenAI-shaped default
//     by forgetting would misroute traffic or drop auth headers.
//   - ConvertRerankRequest / ConvertEmbeddingRequest /
//     ConvertOpenAIResponsesRequest: providers disagree on the trivial form
//     (nil vs request passthrough vs not-implemented), so no single default
//     is behavior-preserving.
//   - GetModelList / GetChannelName: one line returning a provider-specific
//     package symbol; keeping them explicit costs nothing.
type BaseAdaptor struct{}

// Init implements Adaptor.
func (a *BaseAdaptor) Init(_ *relaycommon.RelayInfo) {}

// ConvertAudioRequest implements Adaptor.
func (a *BaseAdaptor) ConvertAudioRequest(_ *gin.Context, _ *relaycommon.RelayInfo, _ dto.AudioRequest) (io.Reader, error) {
	return nil, errors.New("not implemented")
}

// ConvertImageRequest implements Adaptor.
func (a *BaseAdaptor) ConvertImageRequest(_ *gin.Context, _ *relaycommon.RelayInfo, _ dto.ImageRequest) (any, error) {
	return nil, errors.New("not implemented")
}

// ConvertClaudeRequest implements Adaptor.
func (a *BaseAdaptor) ConvertClaudeRequest(_ *gin.Context, _ *relaycommon.RelayInfo, _ *dto.ClaudeRequest) (any, error) {
	return nil, errors.New("not implemented")
}

// ConvertGeminiRequest implements Adaptor.
func (a *BaseAdaptor) ConvertGeminiRequest(_ *gin.Context, _ *relaycommon.RelayInfo, _ *dto.GeminiChatRequest) (any, error) {
	return nil, errors.New("not implemented")
}
