package graph

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/provider"
)

type fakeTokenSource struct{}

func (fakeTokenSource) AccessToken(context.Context) (string, error) {
	return "token", nil
}

func TestWriterWriteMessageUsesSourceFolderByDefault(t *testing.T) {
	t.Parallel()

	mimeBytes := []byte("From: sender@example.com\r\n\r\nhello")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("Authorization = %q, want Bearer token", got)
		}

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1.0/me/mailFolders/source-folder/messages":
			if got := r.Header.Get("Content-Type"); got != "text/plain" {
				t.Fatalf("Content-Type = %q, want text/plain", got)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("ReadAll() error = %v", err)
			}
			wantBody := base64.StdEncoding.EncodeToString(mimeBytes)
			if string(body) != wantBody {
				t.Fatalf("request body = %q, want %q", string(body), wantBody)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"new-1","parentFolderId":"source-folder"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1.0/me/messages/original-1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	writer, err := newWriter(appconfig.MailClientConfig{GraphBaseURL: server.URL + "/v1.0"}, fakeTokenSource{}, server.Client())
	if err != nil {
		t.Fatalf("newWriter() error = %v", err)
	}

	result, err := writer.WriteMessage(context.Background(), provider.WriteRequest{
		Source: provider.MessageRef{
			ID:       "original-1",
			FolderID: "source-folder",
		},
		MIME: mimeBytes,
	})
	if err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}
	if result.Verified {
		t.Fatalf("Verified = true, want false")
	}
}

func TestWriterWriteMessageUsesExplicitDestinationAndVerify(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("Authorization = %q, want Bearer token", got)
		}

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1.0/me/mailFolders/archive":
			if got := r.URL.Query().Get("$select"); got != "id" {
				t.Fatalf("$select = %q, want id", got)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"folder-archive"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1.0/me/mailFolders/folder-archive/messages":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"new-2","parentFolderId":"folder-archive"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1.0/me/messages/new-2":
			if got := r.URL.Query().Get("$select"); got != "id,parentFolderId" {
				t.Fatalf("$select = %q, want id,parentFolderId", got)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"new-2","parentFolderId":"folder-archive"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1.0/me/messages/original-2":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	writer, err := newWriter(appconfig.MailClientConfig{GraphBaseURL: server.URL + "/v1.0"}, fakeTokenSource{}, server.Client())
	if err != nil {
		t.Fatalf("newWriter() error = %v", err)
	}

	result, err := writer.WriteMessage(context.Background(), provider.WriteRequest{
		Source: provider.MessageRef{
			ID:       "original-2",
			FolderID: "source-folder",
		},
		MIME:                []byte("encrypted"),
		DestinationFolderID: "archive",
		Verify:              true,
	})
	if err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}
	if !result.Verified {
		t.Fatalf("Verified = false, want true")
	}
}

func TestWriterWriteMessageKeepsCreatedMessageWhenDeletingOriginalFails(t *testing.T) {
	t.Parallel()

	var rollbackCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1.0/me/mailFolders/source-folder/messages":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"new-keep","parentFolderId":"source-folder"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1.0/me/messages/original-rollback":
			http.Error(w, "delete failed", http.StatusInternalServerError)
		case r.Method == http.MethodDelete && r.URL.Path == "/v1.0/me/messages/new-keep":
			rollbackCalled = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	writer, err := newWriter(appconfig.MailClientConfig{GraphBaseURL: server.URL + "/v1.0"}, fakeTokenSource{}, server.Client())
	if err != nil {
		t.Fatalf("newWriter() error = %v", err)
	}

	_, err = writer.WriteMessage(context.Background(), provider.WriteRequest{
		Source: provider.MessageRef{
			ID:       "original-rollback",
			FolderID: "source-folder",
		},
		MIME: []byte("encrypted"),
	})
	if err == nil {
		t.Fatalf("expected delete failure")
	}
	if rollbackCalled {
		t.Fatalf("unexpected rollback delete of created message")
	}
	if got := err.Error(); !strings.Contains(got, "已保留新加密邮件 new-keep 和原邮件 original-rollback") {
		t.Fatalf("expected keep-both hint in error, got %q", got)
	}
}

func TestWriterWriteMessageReusesExistingProcessedMessageBeforeCreatingDuplicate(t *testing.T) {
	t.Parallel()

	var createCalled bool
	var deleteCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1.0/me/mailFolders/source-folder/messages":
			if got := r.URL.Query().Get("$filter"); got != "internetMessageId eq '<m3@example.com>'" {
				t.Fatalf("$filter = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value":[{"id":"original-3","parentFolderId":"source-folder","internetMessageId":"<m3@example.com>"},{"id":"encrypted-3","parentFolderId":"source-folder","internetMessageId":"<m3@example.com>"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1.0/me/messages/encrypted-3/$value":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("X-MimeCrypt-Processed: yes\r\nContent-Type: multipart/encrypted; protocol=\"application/pgp-encrypted\"\r\n\r\nbody"))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1.0/me/messages/original-3":
			deleteCalled = true
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/v1.0/me/mailFolders/source-folder/messages":
			createCalled = true
			t.Fatalf("unexpected duplicate create request")
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	writer, err := newWriter(appconfig.MailClientConfig{GraphBaseURL: server.URL + "/v1.0"}, fakeTokenSource{}, server.Client())
	if err != nil {
		t.Fatalf("newWriter() error = %v", err)
	}

	result, err := writer.WriteMessage(context.Background(), provider.WriteRequest{
		Source: provider.MessageRef{
			ID:                "original-3",
			InternetMessageID: "<m3@example.com>",
			FolderID:          "source-folder",
		},
		MIME: []byte("encrypted"),
	})
	if err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}
	if createCalled {
		t.Fatalf("expected existing processed message to prevent duplicate create")
	}
	if !deleteCalled {
		t.Fatalf("expected original message delete to be retried")
	}
	if result.Verified {
		t.Fatalf("Verified = true, want false")
	}
}

func TestWriterReconcileMessageTreatsMissingOriginalAsSuccessWhenProcessedCopyExists(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1.0/me/mailFolders/source-folder/messages":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value":[{"id":"encrypted-4","parentFolderId":"source-folder","internetMessageId":"<m4@example.com>"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1.0/me/messages/encrypted-4/$value":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("X-MimeCrypt-Processed: yes\r\nContent-Type: multipart/encrypted; protocol=\"application/pgp-encrypted\"\r\n\r\nbody"))
		case r.Method == http.MethodGet && r.URL.Path == "/v1.0/me/messages/encrypted-4":
			if got := r.URL.Query().Get("$select"); got != "id,parentFolderId" {
				t.Fatalf("$select = %q, want id,parentFolderId", got)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"encrypted-4","parentFolderId":"source-folder"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1.0/me/messages/original-4":
			http.Error(w, "not found", http.StatusNotFound)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	writer, err := newWriter(appconfig.MailClientConfig{GraphBaseURL: server.URL + "/v1.0"}, fakeTokenSource{}, server.Client())
	if err != nil {
		t.Fatalf("newWriter() error = %v", err)
	}

	result, found, err := writer.ReconcileMessage(context.Background(), provider.WriteRequest{
		Source: provider.MessageRef{
			ID:                "original-4",
			InternetMessageID: "<m4@example.com>",
			FolderID:          "source-folder",
		},
		Verify: true,
	})
	if err != nil {
		t.Fatalf("ReconcileMessage() error = %v", err)
	}
	if !found {
		t.Fatalf("found = false, want true")
	}
	if !result.Verified {
		t.Fatalf("Verified = false, want true")
	}
}
