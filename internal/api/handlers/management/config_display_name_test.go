package management

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestPatchStaticProviderDisplayNameForEveryFamily(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*config.Config)
		patch func(*Handler, *gin.Context)
		get   func(*config.Config) string
	}{
		{name: "gemini", setup: func(cfg *config.Config) { cfg.GeminiKey = []config.GeminiKey{{APIKey: "key"}} }, patch: (*Handler).PatchGeminiKey, get: func(cfg *config.Config) string { return cfg.GeminiKey[0].DisplayName }},
		{name: "interactions", setup: func(cfg *config.Config) { cfg.InteractionsKey = []config.GeminiKey{{APIKey: "key"}} }, patch: (*Handler).PatchInteractionsKey, get: func(cfg *config.Config) string { return cfg.InteractionsKey[0].DisplayName }},
		{name: "claude", setup: func(cfg *config.Config) { cfg.ClaudeKey = []config.ClaudeKey{{APIKey: "key"}} }, patch: (*Handler).PatchClaudeKey, get: func(cfg *config.Config) string { return cfg.ClaudeKey[0].DisplayName }},
		{name: "vertex", setup: func(cfg *config.Config) {
			cfg.VertexCompatAPIKey = []config.VertexCompatKey{{APIKey: "key", BaseURL: "https://example.com"}}
		}, patch: (*Handler).PatchVertexCompatKey, get: func(cfg *config.Config) string { return cfg.VertexCompatAPIKey[0].DisplayName }},
		{name: "codex", setup: func(cfg *config.Config) {
			cfg.CodexKey = []config.CodexKey{{APIKey: "key", BaseURL: "https://example.com"}}
		}, patch: (*Handler).PatchCodexKey, get: func(cfg *config.Config) string { return cfg.CodexKey[0].DisplayName }},
		{name: "xai", setup: func(cfg *config.Config) {
			cfg.XAIKey = []config.XAIKey{{APIKey: "key", BaseURL: "https://example.com"}}
		}, patch: (*Handler).PatchXAIKey, get: func(cfg *config.Config) string { return cfg.XAIKey[0].DisplayName }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := &config.Config{}
			test.setup(cfg)
			h := &Handler{cfg: cfg, configFilePath: writeTestConfigFile(t)}

			rec := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(rec)
			ctx.Request = httptest.NewRequest(http.MethodPatch, "/v0/management/key", strings.NewReader(`{"index":0,"value":{"display-name":"  Work account  "}}`))
			ctx.Request.Header.Set("Content-Type", "application/json")
			test.patch(h, ctx)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			if got := test.get(cfg); got != "Work account" {
				t.Fatalf("display name = %q, want %q", got, "Work account")
			}
		})
	}
}
