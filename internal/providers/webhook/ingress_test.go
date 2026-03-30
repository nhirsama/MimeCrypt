package webhook

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"mimecrypt/internal/mailflow/adapters"
)

type countingReadCloser struct {
	reader io.Reader
	reads  int
}

func (r *countingReadCloser) Read(p []byte) (int, error) {
	r.reads++
	return r.reader.Read(p)
}

func (r *countingReadCloser) Close() error {
	return nil
}

func TestWebhookIngressRejectsInvalidSignatureWithoutReadingBody(t *testing.T) {
	t.Parallel()

	body := "Message-ID: <m1@example.com>\r\n\r\nhello"
	bodyHash := sha256hex([]byte(body))
	reader := &countingReadCloser{reader: strings.NewReader(body)}
	ingress := &Ingress{
		sourceName:         "incoming",
		path:               "/mail/incoming",
		secret:             []byte("secret"),
		maxBodyBytes:       1024,
		timestampTolerance: time.Minute,
		spool: &adapters.PushSpool{
			Dir:             t.TempDir(),
			ReplayRetention: time.Hour,
		},
	}

	req := httptest.NewRequest(http.MethodPost, "http://example.com/mail/incoming", reader)
	req.Header.Set("Content-Type", "message/rfc822")
	req.Header.Set(HeaderTimestamp, time.Now().UTC().Format(time.RFC3339))
	req.Header.Set(HeaderDeliveryID, "delivery-1")
	req.Header.Set(HeaderBodySHA256, bodyHash)
	req.Header.Set(HeaderSignature, strings.Repeat("0", sha256.Size*2))

	recorder := httptest.NewRecorder()
	ingress.Handle(recorder, req)

	if got, want := recorder.Code, http.StatusUnauthorized; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if reader.reads != 0 {
		t.Fatalf("body reads = %d, want 0", reader.reads)
	}
}

func TestWebhookIngressAcceptsValidSignedRequest(t *testing.T) {
	t.Parallel()

	timestamp := time.Now().UTC().Truncate(time.Second)
	body := []byte("Message-ID: <m2@example.com>\r\n\r\nhello")
	bodyHash := sha256hex(body)
	spool := &adapters.PushSpool{
		Dir:             t.TempDir(),
		ReplayRetention: time.Hour,
	}
	ingress := &Ingress{
		sourceName:         "incoming",
		path:               "/mail/incoming",
		secret:             []byte("secret"),
		maxBodyBytes:       1024,
		timestampTolerance: time.Minute,
		spool:              spool,
	}

	req := httptest.NewRequest(http.MethodPost, "http://example.com/mail/incoming", io.NopCloser(strings.NewReader(string(body))))
	req.Header.Set("Content-Type", "message/rfc822")
	req.Header.Set(HeaderTimestamp, timestamp.Format(time.RFC3339))
	req.Header.Set(HeaderDeliveryID, "delivery-2")
	req.Header.Set(HeaderBodySHA256, bodyHash)
	req.Header.Set(HeaderSignature, SignatureForBodyHash(ingress.secret, ingress.sourceName, req.Method, ingress.path, timestamp, "delivery-2", bodyHash))

	recorder := httptest.NewRecorder()
	ingress.Handle(recorder, req)

	if got, want := recorder.Code, http.StatusAccepted; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}

	message, found, err := spool.ClaimNext()
	if err != nil {
		t.Fatalf("ClaimNext() error = %v", err)
	}
	if !found {
		t.Fatalf("ClaimNext() found = false, want true")
	}
	if message.Meta.DeliveryID != "delivery-2" {
		t.Fatalf("DeliveryID = %q, want delivery-2", message.Meta.DeliveryID)
	}
	if message.Meta.InternetMessageID != "<m2@example.com>" {
		t.Fatalf("InternetMessageID = %q, want <m2@example.com>", message.Meta.InternetMessageID)
	}

	content, err := os.ReadFile(message.MIMEPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != string(body) {
		t.Fatalf("MIME = %q, want %q", string(content), string(body))
	}
}

func sha256hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
