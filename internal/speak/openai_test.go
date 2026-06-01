package speak

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAISynthSendsOpenAIShape(t *testing.T) {
	var got map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/speech" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		w.Write([]byte("AUDIO"))
	}))
	defer srv.Close()

	s := NewOpenAISpeaker(OpenAIOptions{BaseURL: srv.URL + "/v1", APIKey: "sk-test", Voice: "af_heart", Model: "kokoro", Format: "mp3"})
	audio, err := s.synth(context.Background(), "hello")
	if err != nil {
		t.Fatalf("synth: %v", err)
	}
	if string(audio) != "AUDIO" {
		t.Errorf("audio = %q, want AUDIO", audio)
	}
	if got["input"] != "hello" || got["voice"] != "af_heart" || got["model"] != "kokoro" || got["response_format"] != "mp3" {
		t.Errorf("request body = %+v", got)
	}
	if auth != "Bearer sk-test" {
		t.Errorf("auth header = %q", auth)
	}
}

func TestOpenAIDefaultsAndEndpoint(t *testing.T) {
	s := NewOpenAISpeaker(OpenAIOptions{BaseURL: "http://localhost:8880/v1/"})
	if s.Opts.Voice != "af_heart" || s.Opts.Model != "kokoro" || s.Opts.Format != "mp3" {
		t.Errorf("defaults not applied: %+v", s.Opts)
	}
	if got := s.endpoint("/audio/speech"); got != "http://localhost:8880/v1/audio/speech" {
		t.Errorf("endpoint = %q (trailing slash should be trimmed)", got)
	}
}

func TestOpenAISynthErrorsOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()
	s := NewOpenAISpeaker(OpenAIOptions{BaseURL: srv.URL})
	if _, err := s.synth(context.Background(), "hi"); err == nil {
		t.Fatal("expected error on non-200")
	}
}
