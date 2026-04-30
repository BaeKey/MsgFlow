package wecom

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchAccessTokenRejectsMalformedSuccess(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name:    "empty token",
			body:    `{"errcode":0,"errmsg":"ok","access_token":"","expires_in":7200}`,
			wantErr: "empty access_token",
		},
		{
			name:    "invalid ttl",
			body:    `{"errcode":0,"errmsg":"ok","access_token":"token","expires_in":0}`,
			wantErr: "invalid expires_in",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			notifier := &WeComNotifier{}
			_, _, err := notifier.fetchAccessToken(context.Background(), "corp", "secret", srv.URL)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}
