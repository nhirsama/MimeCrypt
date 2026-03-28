package graph

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"mimecrypt/internal/appconfig"
)

func TestReaderDeltaCreatedMessagesFollowsNextLink(t *testing.T) {
	t.Parallel()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1.0/me/mailFolders/inbox/messages/delta":
			if got := r.URL.Query().Get("changeType"); got != "created" {
				t.Fatalf("changeType = %q, want created", got)
			}
			if got := r.Header.Values("Prefer"); len(got) != 2 {
				t.Fatalf("Prefer count = %d, want 2", len(got))
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value":[{"id":"m1"}],"@odata.nextLink":"` + server.URL + `/v1.0/next-page"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1.0/next-page":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value":[{"id":"m2"}],"@odata.deltaLink":"delta-final"}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	reader, err := newReader(appconfig.MailClientConfig{GraphBaseURL: server.URL + "/v1.0"}, fakeTokenSource{}, server.Client())
	if err != nil {
		t.Fatalf("newReader() error = %v", err)
	}

	messages, delta, err := reader.DeltaCreatedMessages(context.Background(), "inbox", "")
	if err != nil {
		t.Fatalf("DeltaCreatedMessages() error = %v", err)
	}
	if len(messages) != 2 || messages[0].ID != "m1" || messages[1].ID != "m2" {
		t.Fatalf("unexpected messages: %+v", messages)
	}
	if delta != "delta-final" {
		t.Fatalf("delta = %q, want delta-final", delta)
	}
}

func TestReaderFetchMIMEReturnsStream(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1.0/me/messages/m1/$value" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		if got := r.Header.Get("Accept"); got != "application/octet-stream" {
			t.Fatalf("Accept = %q, want application/octet-stream", got)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("mime-body"))
	}))
	defer server.Close()

	reader, err := newReader(appconfig.MailClientConfig{GraphBaseURL: server.URL + "/v1.0"}, fakeTokenSource{}, server.Client())
	if err != nil {
		t.Fatalf("newReader() error = %v", err)
	}

	stream, err := reader.FetchMIME(context.Background(), "m1")
	if err != nil {
		t.Fatalf("FetchMIME() error = %v", err)
	}
	defer stream.Close()

	content, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(content) != "mime-body" {
		t.Fatalf("content = %q, want mime-body", string(content))
	}
}
