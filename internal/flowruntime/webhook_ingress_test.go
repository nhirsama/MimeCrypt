package flowruntime

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"mimecrypt/internal/mailflow/adapters"
)

func TestWebhookIngressAcceptsSignedMessage(t *testing.T) {
	t.Parallel()

	ingress := testWebhookIngress(t)
	timestamp := time.Now().UTC()
	body := webhookTestBody("<m1@example.com>")

	req := signedWebhookRequest(ingress, timestamp, "delivery-1", body)
	recorder := httptest.NewRecorder()
	ingress.handle(recorder, req)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusAccepted)
	}
	if got := strings.TrimSpace(recorder.Body.String()); got != "accepted" {
		t.Fatalf("response body = %q, want accepted", got)
	}

	claimed, found, err := ingress.spool.ClaimNext()
	if err != nil {
		t.Fatalf("ClaimNext() error = %v", err)
	}
	if !found {
		t.Fatalf("ClaimNext() found = false, want true")
	}
	if claimed.Meta.DeliveryID != "delivery-1" {
		t.Fatalf("DeliveryID = %q, want delivery-1", claimed.Meta.DeliveryID)
	}
	if claimed.Meta.InternetMessageID != "<m1@example.com>" {
		t.Fatalf("InternetMessageID = %q, want <m1@example.com>", claimed.Meta.InternetMessageID)
	}
	content, err := os.ReadFile(claimed.MIMEPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != string(body) {
		t.Fatalf("stored MIME = %q, want %q", string(content), string(body))
	}
}

func TestWebhookIngressReturnsDuplicateForReplay(t *testing.T) {
	t.Parallel()

	ingress := testWebhookIngress(t)
	timestamp := time.Now().UTC()
	body := webhookTestBody("<m2@example.com>")

	first := httptest.NewRecorder()
	ingress.handle(first, signedWebhookRequest(ingress, timestamp, "delivery-2", body))
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status = %d, want %d", first.Code, http.StatusAccepted)
	}

	second := httptest.NewRecorder()
	ingress.handle(second, signedWebhookRequest(ingress, timestamp, "delivery-2", body))
	if second.Code != http.StatusAccepted {
		t.Fatalf("second status = %d, want %d", second.Code, http.StatusAccepted)
	}
	if got := strings.TrimSpace(second.Body.String()); got != "duplicate" {
		t.Fatalf("response body = %q, want duplicate", got)
	}
}

func TestWebhookIngressRejectsInvalidSignature(t *testing.T) {
	t.Parallel()

	ingress := testWebhookIngress(t)
	timestamp := time.Now().UTC()
	body := webhookTestBody("<m3@example.com>")

	req := signedWebhookRequest(ingress, timestamp, "delivery-3", body)
	req.Header.Set(webhookHeaderSignature, "sha256=deadbeef")

	recorder := httptest.NewRecorder()
	ingress.handle(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if _, found, err := ingress.spool.ClaimNext(); err != nil {
		t.Fatalf("ClaimNext() error = %v", err)
	} else if found {
		t.Fatalf("ClaimNext() found = true, want false")
	}
}

func TestWebhookIngressRejectsTimestampOutsideTolerance(t *testing.T) {
	t.Parallel()

	ingress := testWebhookIngress(t)
	timestamp := time.Now().UTC().Add(-ingress.timestampTolerance - time.Second)
	body := webhookTestBody("<m4@example.com>")

	recorder := httptest.NewRecorder()
	ingress.handle(recorder, signedWebhookRequest(ingress, timestamp, "delivery-4", body))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestWebhookIngressRejectsOversizedBody(t *testing.T) {
	t.Parallel()

	ingress := testWebhookIngress(t)
	ingress.maxBodyBytes = 8
	timestamp := time.Now().UTC()
	body := webhookTestBody("<m5@example.com>")

	recorder := httptest.NewRecorder()
	ingress.handle(recorder, signedWebhookRequest(ingress, timestamp, "delivery-5", body))

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestSpoolRequestBodyComputesHashAndPersistsBody(t *testing.T) {
	t.Parallel()

	body := webhookTestBody("<stream@example.com>")
	path, bodyHash, err := spoolRequestBody(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("spoolRequestBody() error = %v", err)
	}
	defer os.Remove(path)

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != string(body) {
		t.Fatalf("stored body = %q, want %q", string(content), string(body))
	}

	sum := sha256.Sum256(body)
	if got, want := bodyHash, hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("body hash = %q, want %q", got, want)
	}
}

func TestSpoolRequestBodyRejectsEmptyBody(t *testing.T) {
	t.Parallel()

	path, bodyHash, err := spoolRequestBody(strings.NewReader(""))
	if err != nil {
		t.Fatalf("spoolRequestBody() error = %v", err)
	}
	defer os.Remove(path)

	if bodyHash != "" {
		t.Fatalf("body hash = %q, want empty", bodyHash)
	}
	if path != "" {
		t.Fatalf("path = %q, want empty", path)
	}
}

func testWebhookIngress(t *testing.T) *webhookIngress {
	t.Helper()

	return &webhookIngress{
		sourceName:         "incoming",
		listenAddr:         "127.0.0.1:0",
		path:               "/mail/incoming",
		secret:             []byte("top-secret"),
		maxBodyBytes:       1 << 20,
		timestampTolerance: 5 * time.Minute,
		spool: &adapters.PushSpool{
			Dir:             t.TempDir(),
			ReplayRetention: 5 * time.Minute,
			Now:             func() time.Time { return time.Now().UTC() },
		},
	}
}

func signedWebhookRequest(ingress *webhookIngress, timestamp time.Time, deliveryID string, body []byte) *http.Request {
	req := httptest.NewRequest(http.MethodPost, ingress.path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "message/rfc822")
	req.Header.Set(webhookHeaderTimestamp, timestamp.UTC().Format(time.RFC3339))
	req.Header.Set(webhookHeaderDeliveryID, deliveryID)
	req.Header.Set(webhookHeaderSignature, "sha256="+webhookSignature(ingress.secret, ingress.sourceName, http.MethodPost, ingress.path, timestamp, deliveryID, body))
	return req
}

func webhookTestBody(messageID string) []byte {
	return []byte("Message-ID: " + messageID + "\r\nSubject: inbound\r\n\r\nhello")
}
