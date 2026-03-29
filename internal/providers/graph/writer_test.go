package graph

import (
	"context"
	"encoding/base64"
	"encoding/json"
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

func (fakeTokenSource) AccessTokenForScopes(context.Context, []string) (string, error) {
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
		case r.Method == http.MethodPost && r.URL.Path == "/v1.0/me/messages":
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
			_, _ = w.Write([]byte(`{"id":"draft-1","parentFolderId":"drafts"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1.0/me/messages/draft-1/move":
			if got := r.Header.Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", got)
			}
			var payload struct {
				DestinationID string `json:"destinationId"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if payload.DestinationID != "source-folder" {
				t.Fatalf("destinationId = %q, want source-folder", payload.DestinationID)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"new-1","parentFolderId":"source-folder"}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/v1.0/me/messages/new-1":
			var payload struct {
				IsRead bool `json:"isRead"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if payload.IsRead {
				t.Fatalf("isRead = true, want false")
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
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
		MIME:         mimeBytes,
		DeleteSource: true,
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
		case r.Method == http.MethodPost && r.URL.Path == "/v1.0/me/messages":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"draft-2","parentFolderId":"drafts"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1.0/me/messages/draft-2/move":
			var payload struct {
				DestinationID string `json:"destinationId"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if payload.DestinationID != "folder-archive" {
				t.Fatalf("destinationId = %q, want folder-archive", payload.DestinationID)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"new-2","parentFolderId":"folder-archive"}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/v1.0/me/messages/new-2":
			var payload struct {
				IsRead bool `json:"isRead"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if payload.IsRead {
				t.Fatalf("isRead = true, want false")
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"new-2","parentFolderId":"folder-archive"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1.0/me/messages/new-2":
			if got := r.URL.Query().Get("$select"); got != "id,parentFolderId" {
				t.Fatalf("$select = %q, want id,parentFolderId", got)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"new-2","parentFolderId":"folder-archive"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1.0/me/messages/new-2/$value":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("X-MimeCrypt-Processed: yes\r\nContent-Type: multipart/encrypted; protocol=\"application/pgp-encrypted\"\r\n\r\nbody"))
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
		DeleteSource:        true,
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
		case r.Method == http.MethodPost && r.URL.Path == "/v1.0/me/messages":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"draft-keep","parentFolderId":"drafts"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1.0/me/messages/draft-keep/move":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"new-keep","parentFolderId":"source-folder"}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/v1.0/me/messages/new-keep":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
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
		MIME:         []byte("encrypted"),
		DeleteSource: true,
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

func TestWriterWriteMessageKeepsBothWhenVerifyFails(t *testing.T) {
	t.Parallel()

	var deleteOriginalCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1.0/me/messages":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"draft-verify","parentFolderId":"drafts"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1.0/me/messages/draft-verify/move":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"new-verify","parentFolderId":"source-folder"}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/v1.0/me/messages/new-verify":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"new-verify","parentFolderId":"source-folder"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1.0/me/messages/new-verify":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"new-verify","parentFolderId":"source-folder"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1.0/me/messages/new-verify/$value":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Content-Type: text/plain\r\n\r\nbody"))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1.0/me/messages/original-verify":
			deleteOriginalCalled = true
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
			ID:       "original-verify",
			FolderID: "source-folder",
		},
		MIME:         []byte("encrypted"),
		Verify:       true,
		DeleteSource: true,
	})
	if err == nil {
		t.Fatalf("expected verify failure")
	}
	if deleteOriginalCalled {
		t.Fatalf("unexpected delete of original after verify failure")
	}
	if got := err.Error(); !strings.Contains(got, "缺少 MimeCrypt 处理标记") {
		t.Fatalf("error = %q, want processed marker failure", got)
	}
	if got := err.Error(); !strings.Contains(got, "已保留新加密邮件 new-verify 和原邮件 original-verify") {
		t.Fatalf("error = %q, want keep-both hint", got)
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
		case r.Method == http.MethodPost && r.URL.Path == "/v1.0/me/messages":
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
		MIME:         []byte("encrypted"),
		DeleteSource: true,
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
		Verify:       true,
		DeleteSource: true,
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

func TestWriterWriteMessageSkipsDeletingOriginalWhenDeleteDisabled(t *testing.T) {
	t.Parallel()

	var deleteOriginalCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1.0/me/messages":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"draft-no-delete","parentFolderId":"drafts"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1.0/me/messages/draft-no-delete/move":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"new-no-delete","parentFolderId":"source-folder"}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/v1.0/me/messages/new-no-delete":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"new-no-delete","parentFolderId":"source-folder"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1.0/me/messages/original-no-delete":
			deleteOriginalCalled = true
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

	if _, err := writer.WriteMessage(context.Background(), provider.WriteRequest{
		Source: provider.MessageRef{
			ID:       "original-no-delete",
			FolderID: "source-folder",
		},
		MIME: []byte("encrypted"),
	}); err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}
	if deleteOriginalCalled {
		t.Fatalf("unexpected delete of original message")
	}
}
