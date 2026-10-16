// Command mock-upstream is a zero-dependency OpenAI-compatible upstream for
// gateway load testing. It answers /v1/chat/completions in streaming (SSE)
// and non-streaming modes with a small simulated inference delay, so load
// profiles measure gateway overhead instead of upstream latency.
//
// Usage: go run ./perf/mock-upstream  (listens on 127.0.0.1:8081)
// The gateway channel must point at http://host.docker.internal:8081 with
// any bearer key (checked only for presence when MOCK_REQUIRE_AUTH=1).
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

type chatRequest struct {
	Model    string `json:"model"`
	Stream   bool   `json:"stream"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", handleChatCompletions)
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"mock-gpt","object":"model"}]}`))
	})
	server := &http.Server{
		Addr:              "127.0.0.1:8081",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	fmt.Println("mock upstream listening on 127.0.0.1:8081")
	if err := server.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, "mock upstream exited:", err)
		os.Exit(1)
	}
}

func handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("MOCK_REQUIRE_AUTH") == "1" && r.Header.Get("Authorization") == "" {
		http.Error(w, `{"error":{"message":"missing authorization","type":"invalid_request_error"}}`, http.StatusUnauthorized)
		return
	}
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":{"message":"invalid json","type":"invalid_request_error"}}`, http.StatusBadRequest)
		return
	}
	model := req.Model
	if model == "" {
		model = "mock-gpt"
	}
	if req.Stream {
		serveStream(w, model)
		return
	}
	time.Sleep(5 * time.Millisecond)
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"id":"chatcmpl-mock","object":"chat.completion","model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`, model)
}

func serveStream(w http.ResponseWriter, model string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)
	for i := 0; i < 5; i++ {
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-mock\",\"object\":\"chat.completion.chunk\",\"model\":%q,\"choices\":[{\"index\":0,\"delta\":{\"content\":\"chunk-%d \"}}]}\n\n", model, i)
		if flusher != nil {
			flusher.Flush()
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}
