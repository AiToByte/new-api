package relay

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// mockUpstreamOptions configures the shared perf-test upstream. Zero values
// select single-JSON mode with standard chat-completion payload; set Stream
// for SSE output.
type mockUpstreamOptions struct {
	StreamChunks int
	ChunkLatency time.Duration
	Stream       bool
	StatusCode   int
}

type capturedUpstreamRequest struct {
	path        string
	body        []byte
	contentType string
}

func (o mockUpstreamOptions) withDefaults() mockUpstreamOptions {
	if o.StreamChunks <= 0 {
		o.StreamChunks = 5
	}
	if o.ChunkLatency <= 0 {
		o.ChunkLatency = 10 * time.Millisecond
	}
	if o.StatusCode == 0 {
		o.StatusCode = http.StatusOK
	}
	return o
}

// newMockUpstream starts an in-process upstream that answers OpenAI-style
// chat completions, either streamed (SSE) or as a single JSON object. Every
// request body is captured on the returned channel (buffered) so tests can
// assert what the gateway forwarded without any network flakiness.
func newMockUpstream(t *testing.T, opts mockUpstreamOptions) (*httptest.Server, <-chan capturedUpstreamRequest) {
	t.Helper()
	opts = opts.withDefaults()
	captured := make(chan capturedUpstreamRequest, 1024)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured <- capturedUpstreamRequest{
			path:        r.URL.Path,
			body:        body,
			contentType: r.Header.Get("Content-Type"),
		}
		if opts.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.WriteHeader(opts.StatusCode)
			flusher, _ := w.(http.Flusher)
			for i := 0; i < opts.StreamChunks; i++ {
				_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-mock\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"chunk-%d \"}}]}\n\n", i)
				if flusher != nil {
					flusher.Flush()
				}
				time.Sleep(opts.ChunkLatency)
			}
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(opts.StatusCode)
		_, _ = w.Write([]byte(`{"id":"chatcmpl-mock","object":"chat.completion","model":"mock","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`))
	}))
	t.Cleanup(server.Close)
	return server, captured
}

func TestNewMockUpstreamServesStreamAndJSON(t *testing.T) {
	streamServer, streamCaptured := newMockUpstream(t, mockUpstreamOptions{Stream: true})
	resp, err := http.Post(streamServer.URL+"/v1/chat/completions", "application/json", nil)
	require.NoError(t, err)
	streamBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Contains(t, string(streamBody), "data: [DONE]")

	select {
	case req := <-streamCaptured:
		require.Equal(t, "/v1/chat/completions", req.path)
	default:
		t.Fatal("expected the streamed request to be captured")
	}

	jsonServer, jsonCaptured := newMockUpstream(t, mockUpstreamOptions{Stream: false})
	jsonResp, err := http.Post(jsonServer.URL+"/v1/chat/completions", "application/json", nil)
	require.NoError(t, err)
	jsonBody, err := io.ReadAll(jsonResp.Body)
	require.NoError(t, err)
	require.NoError(t, jsonResp.Body.Close())
	require.Contains(t, string(jsonBody), `"finish_reason":"stop"`)

	select {
	case req := <-jsonCaptured:
		require.Equal(t, "application/json", req.contentType)
	default:
		t.Fatal("expected the JSON request to be captured")
	}
}
