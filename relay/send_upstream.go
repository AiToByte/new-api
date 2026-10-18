package relay

import (
	"errors"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// AcceptUpstreamStatus reports whether an upstream HTTP status code counts
// as success for one relay attempt.
type AcceptUpstreamStatus func(statusCode int) bool

// AcceptStatusOK accepts only 200. It is the historical rule of every handler
// except alpha_search.
func AcceptStatusOK(statusCode int) bool { return statusCode == http.StatusOK }

// AcceptStatus2xx accepts any 2xx. It preserves the alpha_search rule.
func AcceptStatus2xx(statusCode int) bool { return statusCode >= 200 && statusCode < 300 }

// SendUpstream executes one upstream attempt shared by all protocol handlers:
// DoRequest, success-status check, RelayErrorHandler mapping and
// status_code_mapping reset. It returns a non-nil *types.NewAPIError when the
// attempt fails, otherwise the typed response for the caller to stream.
//
// Behavior preserved from the per-handler code this replaces:
//   - DoRequest transport errors map to ErrorCodeDoRequestFailed/500 exactly
//     as before. (DoApiRequest already logs transport failures centrally, so
//     the former per-handler log lines are not repeated here.)
//   - A nil response with nil error is passed through as (nil, nil); callers
//     forward it to DoResponse exactly like the old `if resp != nil` blocks.
//   - A non-nil response of unexpected type used to panic on `resp.(*http.Response)`.
//     It now returns a classified 500 and logs the actual type server-side;
//     the client-facing message keeps the historical "invalid http response" text.
//
// Deliberately NOT covered (kept at the call sites):
//   - image_handler: Replicate answers 201 and the handler rewrites it to 200
//     before DoResponse sees it; a shared policy must not swallow that rewrite.
//   - websocket.go: the realtime path asserts a *websocket.Conn, sends a nil
//     body and maps DoRequest errors without the OpenAI envelope.
func SendUpstream(c *gin.Context, info *relaycommon.RelayInfo, adaptor channel.Adaptor, requestBody io.Reader, accept AcceptUpstreamStatus) (*http.Response, *types.NewAPIError) {
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	if resp == nil {
		return nil, nil
	}
	httpResp, ok := resp.(*http.Response)
	if !ok || httpResp == nil {
		common.SysLog(common.LogText("unexpected upstream response type: %T", resp))
		return nil, types.NewOpenAIError(errors.New("invalid http response"), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	if !accept(httpResp.StatusCode) {
		newAPIError := service.RelayErrorHandler(c.Request.Context(), httpResp, false)
		// reset status code 重置状态码
		service.ResetStatusCode(newAPIError, c.GetString("status_code_mapping"))
		return nil, newAPIError
	}
	return httpResp, nil
}
