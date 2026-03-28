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

func TestEWSWriterWriteMessageCreatesNonDraftMessage(t *testing.T) {
	t.Parallel()

	mimeBytes := []byte("From: sender@example.com\r\nMessage-ID: <m1@example.com>\r\n\r\nhello")
	var (
		listCalls int
		soapBody  string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1.0/me/mailFolders/source-folder/messages":
			listCalls++
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			if listCalls == 1 {
				_, _ = w.Write([]byte(`{"value":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"value":[{"id":"encrypted-1","parentFolderId":"source-folder","internetMessageId":"<m1@example.com>"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1.0/me/messages/encrypted-1/$value":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("X-MimeCrypt-Processed: yes\r\nContent-Type: multipart/encrypted; protocol=\"application/pgp-encrypted\"\r\n\r\nbody"))
		case r.Method == http.MethodPost && r.URL.Path == "/v1.0/me/translateExchangeIds":
			var payload struct {
				InputIDs     []string `json:"inputIds"`
				SourceIDType string   `json:"sourceIdType"`
				TargetIDType string   `json:"targetIdType"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if payload.SourceIDType != "restId" || payload.TargetIDType != "ewsId" {
				t.Fatalf("unexpected translate payload: %+v", payload)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"value":[{"sourceId":"source-folder","targetId":"ews-folder-1"}]}`))
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
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <CreateItemResponse xmlns="http://schemas.microsoft.com/exchange/services/2006/messages">
      <ResponseMessages>
        <CreateItemResponseMessage ResponseClass="Success">
          <ResponseCode>NoError</ResponseCode>
          <Items />
        </CreateItemResponseMessage>
      </ResponseMessages>
    </CreateItemResponse>
  </soap:Body>
</soap:Envelope>`))
		case r.Method == http.MethodPatch && r.URL.Path == "/v1.0/me/messages/encrypted-1":
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
			_, _ = w.Write([]byte(`{"id":"encrypted-1","parentFolderId":"source-folder"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1.0/me/messages/encrypted-1":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"encrypted-1","parentFolderId":"source-folder"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1.0/me/messages/original-1":
			w.WriteHeader(http.StatusNoContent)
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

	result, err := writer.WriteMessage(context.Background(), provider.WriteRequest{
		Source: provider.MessageRef{
			ID:                "original-1",
			InternetMessageID: "<m1@example.com>",
			FolderID:          "source-folder",
		},
		MIME:   mimeBytes,
		Verify: true,
	})
	if err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}
	if !result.Verified {
		t.Fatalf("Verified = false, want true")
	}
	if !strings.Contains(soapBody, `MessageDisposition="SaveOnly"`) {
		t.Fatalf("soap body missing SaveOnly: %s", soapBody)
	}
	if !strings.Contains(soapBody, `FolderId Id="ews-folder-1"`) {
		t.Fatalf("soap body missing translated folder id: %s", soapBody)
	}
	if !strings.Contains(soapBody, "<t:IsRead>false</t:IsRead>") {
		t.Fatalf("soap body missing unread flag: %s", soapBody)
	}
	if !strings.Contains(soapBody, `PropertyTag="0x0E07" PropertyType="Integer"`) {
		t.Fatalf("soap body missing message flags property: %s", soapBody)
	}
	if !strings.Contains(soapBody, base64.StdEncoding.EncodeToString(mimeBytes)) {
		t.Fatalf("soap body missing MIME payload")
	}
}

func TestEWSWriterWriteMessageFallsBackToTranslateCreatedItemID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1.0/me/translateExchangeIds":
			var payload struct {
				InputIDs     []string `json:"inputIds"`
				SourceIDType string   `json:"sourceIdType"`
				TargetIDType string   `json:"targetIdType"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			switch {
			case payload.SourceIDType == "restId" && payload.TargetIDType == "ewsId":
				_, _ = w.Write([]byte(`{"value":[{"sourceId":"source-folder","targetId":"ews-folder-2"}]}`))
			case payload.SourceIDType == "ewsId" && payload.TargetIDType == "restId":
				_, _ = w.Write([]byte(`{"value":[{"sourceId":"ews-item-2","targetId":"graph-item-2"}]}`))
			default:
				t.Fatalf("unexpected translate payload: %+v", payload)
			}
		case r.Method == http.MethodPost && r.URL.Path == "/EWS/Exchange.asmx":
			w.Header().Set("Content-Type", "text/xml; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <CreateItemResponse xmlns="http://schemas.microsoft.com/exchange/services/2006/messages">
      <ResponseMessages>
        <CreateItemResponseMessage ResponseClass="Success">
          <ResponseCode>NoError</ResponseCode>
          <Items>
            <Message xmlns="http://schemas.microsoft.com/exchange/services/2006/types">
              <ItemId Id="ews-item-2" />
            </Message>
          </Items>
        </CreateItemResponseMessage>
      </ResponseMessages>
    </CreateItemResponse>
  </soap:Body>
</soap:Envelope>`))
		case r.Method == http.MethodPatch && r.URL.Path == "/v1.0/me/messages/graph-item-2":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"graph-item-2","parentFolderId":"source-folder"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1.0/me/messages/original-2":
			w.WriteHeader(http.StatusNoContent)
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

	result, err := writer.WriteMessage(context.Background(), provider.WriteRequest{
		Source: provider.MessageRef{
			ID:       "original-2",
			FolderID: "source-folder",
		},
		MIME: []byte("encrypted"),
	})
	if err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}
	if result.Verified {
		t.Fatalf("Verified = true, want false")
	}
}
