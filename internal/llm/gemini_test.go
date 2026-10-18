package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeminiError(t *testing.T) {
	// what Gemini CLI 0.63 prints for a personal Google account
	ineligible := `Warning: 256-color support not detected.
Error authenticating: IneligibleTierError: This client is no longer supported for Gemini Code Assist for individuals. To continue using Gemini, please migrate to the Antigravity suite of products: https://antigravity.google
    at throwIneligibleOrProjectIdError (file:///x/chunk.js:311295:11)
    at _doSetupUser (file:///x/chunk.js:311284:5)
  ineligibleTiers: [
    {
      reasonCode: 'UNSUPPORTED_CLIENT',
    }
  ]
}`
	got := GeminiError(ineligible)
	if !strings.HasPrefix(got, "This client is no longer supported") || !strings.Contains(got, "GEMINI_API_KEY") || strings.Contains(got, "at _doSetupUser") {
		t.Fatalf("ineligible = %q", got)
	}
	if got := GeminiError("Please set an Auth method in your settings.json\n"); got != "Please set an Auth method in your settings.json" {
		t.Fatalf("plain = %q", got)
	}
}

func TestGeminiAPIModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "k" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"models":[
			{"name":"models/gemini-3.8-flash","supportedGenerationMethods":["generateContent"]},
			{"name":"models/gemini-4-argon","supportedGenerationMethods":["generateContent","countTokens"]},
			{"name":"models/gemini-embedding-001","supportedGenerationMethods":["embedContent"]},
			{"name":"models/imagen-4","supportedGenerationMethods":["predict"]}]}`))
	}))
	defer srv.Close()
	got := geminiAPIModels(context.Background(), srv.URL, "k")
	if strings.Join(got, ",") != "gemini-4-argon,gemini-3.8-flash" {
		t.Fatalf("models = %v", got)
	}
	if got := geminiAPIModels(context.Background(), srv.URL, ""); got != nil {
		t.Fatalf("no key = %v", got)
	}
	if got := geminiAPIModels(context.Background(), srv.URL, "bad"); got != nil {
		t.Fatalf("rejected key = %v", got)
	}
}
