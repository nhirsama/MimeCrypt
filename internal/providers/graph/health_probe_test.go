package graph

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mimecrypt/internal/appconfig"
)

func TestGraphWriterHealthCheckUsesGraphAPI(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("Authorization = %q, want Bearer token", got)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1.0/me":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"mail":"user@example.com"}`)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	writer, err := newWriter(appconfig.MailClientConfig{GraphBaseURL: server.URL + "/v1.0"}, fakeTokenSource{}, server.Client())
	if err != nil {
		t.Fatalf("newWriter() error = %v", err)
	}

	detail, err := writer.HealthCheck(context.Background())
	if err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}
	if detail != "user@example.com" {
		t.Fatalf("detail = %q, want user@example.com", detail)
	}
}

func TestEWSWriterHealthCheckUsesEWSAPI(t *testing.T) {
	t.Parallel()

	var soapBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/EWS/Exchange.asmx":
			if got := r.Header.Get("Authorization"); got != "Bearer token" {
				t.Fatalf("Authorization = %q, want Bearer token", got)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("ReadAll() error = %v", err)
			}
			soapBody = string(body)
			w.Header().Set("Content-Type", "text/xml; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <GetFolderResponse xmlns="http://schemas.microsoft.com/exchange/services/2006/messages">
      <ResponseMessages>
        <GetFolderResponseMessage ResponseClass="Success">
          <ResponseCode>NoError</ResponseCode>
        </GetFolderResponseMessage>
      </ResponseMessages>
    </GetFolderResponse>
  </soap:Body>
</soap:Envelope>`)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	cfg := appconfig.Config{
		Auth: appconfig.AuthConfig{
			EWSScopes: []string{"scope-ews"},
		},
		Mail: appconfig.MailConfig{
			Client: appconfig.MailClientConfig{
				GraphBaseURL: server.URL + "/v1.0",
				EWSBaseURL:   server.URL + "/EWS/Exchange.asmx",
			},
		},
	}
	writer, err := newEWSWriter(cfg, fakeTokenSource{}, server.Client())
	if err != nil {
		t.Fatalf("newEWSWriter() error = %v", err)
	}

	detail, err := writer.HealthCheck(context.Background())
	if err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}
	if detail != "ews inbox" {
		t.Fatalf("detail = %q, want ews inbox", detail)
	}
	if !strings.Contains(soapBody, "<m:GetFolder>") {
		t.Fatalf("soap body missing GetFolder: %s", soapBody)
	}
	if !strings.Contains(soapBody, `DistinguishedFolderId Id="inbox"`) {
		t.Fatalf("soap body missing inbox probe: %s", soapBody)
	}
}
