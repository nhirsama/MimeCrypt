package flowruntime

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/mail"
	"os"
	"strings"
	"time"

	"mimecrypt/internal/mailflow/adapters"
)

const (
	webhookHeaderDeliveryID = "X-MimeCrypt-Delivery-ID"
	webhookHeaderSignature  = "X-MimeCrypt-Signature"
	webhookHeaderTimestamp  = "X-MimeCrypt-Timestamp"

	defaultWebhookMaxBodyBytes       = 25 << 20
	defaultWebhookTimestampTolerance = 5 * time.Minute
)

type webhookIngress struct {
	sourceName         string
	listenAddr         string
	path               string
	secret             []byte
	maxBodyBytes       int64
	timestampTolerance time.Duration
	spool              *adapters.PushSpool
}

func buildWebhookIngress(run SourceRun, spool *adapters.PushSpool) (pushIngress, error) {
	if run.Source.Webhook == nil {
		return nil, fmt.Errorf("source %s 缺少 webhook 配置", run.Source.Name)
	}
	secretEnv := strings.TrimSpace(run.Source.Webhook.SecretEnv)
	if secretEnv == "" {
		return nil, fmt.Errorf("source %s webhook secret_env 不能为空", run.Source.Name)
	}
	secret := []byte(os.Getenv(secretEnv))
	if len(secret) == 0 {
		return nil, fmt.Errorf("source %s webhook secret_env 未设置: %s", run.Source.Name, secretEnv)
	}
	maxBodyBytes := run.Source.Webhook.MaxBodyBytes
	if maxBodyBytes <= 0 {
		maxBodyBytes = defaultWebhookMaxBodyBytes
	}
	timestampTolerance := run.Source.Webhook.TimestampTolerance
	if timestampTolerance <= 0 {
		timestampTolerance = defaultWebhookTimestampTolerance
	}

	return &webhookIngress{
		sourceName:         firstNonEmpty(strings.TrimSpace(run.Source.Name), "webhook"),
		listenAddr:         strings.TrimSpace(run.Source.Webhook.ListenAddr),
		path:               strings.TrimSpace(run.Source.Webhook.Path),
		secret:             secret,
		maxBodyBytes:       maxBodyBytes,
		timestampTolerance: timestampTolerance,
		spool:              spool,
	}, nil
}

func (w *webhookIngress) Run(ctx context.Context) error {
	if w == nil || w.spool == nil {
		return fmt.Errorf("webhook ingress 未初始化")
	}

	mux := http.NewServeMux()
	mux.HandleFunc(w.path, w.handle)

	server := &http.Server{
		Addr:              w.listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	serveErrCh := make(chan error, 1)
	go func() {
		serveErrCh <- server.ListenAndServe()
	}()

	select {
	case err := <-serveErrCh:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		err := <-serveErrCh
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return ctx.Err()
		}
		return err
	}
}

func (w *webhookIngress) handle(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !w.allowedContentType(req.Header.Get("Content-Type")) {
		http.Error(rw, "unsupported content type", http.StatusUnsupportedMediaType)
		return
	}

	timestamp, err := parseWebhookTimestamp(req.Header.Get(webhookHeaderTimestamp))
	if err != nil {
		http.Error(rw, "invalid timestamp", http.StatusUnauthorized)
		return
	}
	if !withinTolerance(time.Now().UTC(), timestamp, w.timestampTolerance) {
		http.Error(rw, "timestamp outside tolerance window", http.StatusUnauthorized)
		return
	}

	deliveryID := strings.TrimSpace(req.Header.Get(webhookHeaderDeliveryID))
	if deliveryID == "" {
		http.Error(rw, "missing delivery id", http.StatusUnauthorized)
		return
	}

	req.Body = http.MaxBytesReader(rw, req.Body, w.maxBodyBytes)
	body, err := io.ReadAll(req.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			http.Error(rw, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(rw, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(body) == 0 {
		http.Error(rw, "empty request body", http.StatusBadRequest)
		return
	}

	signature := strings.TrimSpace(req.Header.Get(webhookHeaderSignature))
	if !w.validSignature(req.Method, req.URL.Path, timestamp, deliveryID, body, signature) {
		http.Error(rw, "invalid signature", http.StatusUnauthorized)
		return
	}

	duplicate, err := w.spool.Enqueue(adapters.PushMessage{
		DeliveryID:        deliveryID,
		InternetMessageID: extractInternetMessageID(body),
		ReceivedAt:        timestamp,
		MIME:              body,
		Attributes: map[string]string{
			"ingress":     "webhook",
			"delivery_id": deliveryID,
		},
	})
	if err != nil {
		http.Error(rw, "failed to enqueue message", http.StatusInternalServerError)
		return
	}

	rw.WriteHeader(http.StatusAccepted)
	if duplicate {
		_, _ = rw.Write([]byte("duplicate\n"))
		return
	}
	_, _ = rw.Write([]byte("accepted\n"))
}

func (w *webhookIngress) allowedContentType(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "message/rfc822", "application/octet-stream":
		return true
	default:
		return false
	}
}

func (w *webhookIngress) validSignature(method, path string, timestamp time.Time, deliveryID string, body []byte, signature string) bool {
	expected := webhookSignature(w.secret, w.sourceName, method, path, timestamp, deliveryID, body)
	signature = strings.TrimPrefix(strings.TrimSpace(signature), "sha256=")
	if len(signature) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.ToLower(signature)), []byte(expected)) == 1
}

func webhookSignature(secret []byte, sourceName, method, path string, timestamp time.Time, deliveryID string, body []byte) string {
	bodySum := sha256.Sum256(body)
	payload := strings.Join([]string{
		strings.TrimSpace(sourceName),
		strings.ToUpper(strings.TrimSpace(method)),
		strings.TrimSpace(path),
		timestamp.UTC().Format(time.RFC3339),
		strings.TrimSpace(deliveryID),
		hex.EncodeToString(bodySum[:]),
	}, "\n")
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func parseWebhookTimestamp(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("timestamp 不能为空")
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}

func withinTolerance(now, timestamp time.Time, tolerance time.Duration) bool {
	if tolerance <= 0 {
		tolerance = defaultWebhookTimestampTolerance
	}
	if timestamp.After(now) {
		return timestamp.Sub(now) <= tolerance
	}
	return now.Sub(timestamp) <= tolerance
}

func extractInternetMessageID(body []byte) string {
	message, err := mail.ReadMessage(bytes.NewReader(body))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(message.Header.Get("Message-ID"))
}
