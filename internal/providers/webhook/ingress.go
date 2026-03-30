package webhook

import (
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
	"path/filepath"
	"strings"
	"time"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/mailflow/adapters"
)

const (
	HeaderDeliveryID = "X-MimeCrypt-Delivery-ID"
	HeaderBodySHA256 = "X-MimeCrypt-Body-SHA256"
	HeaderSignature  = "X-MimeCrypt-Signature"
	HeaderTimestamp  = "X-MimeCrypt-Timestamp"

	defaultMaxBodyBytes       = 25 << 20
	defaultTimestampTolerance = 5 * time.Minute
)

type Ingress struct {
	sourceName         string
	listenAddr         string
	path               string
	secret             []byte
	maxBodyBytes       int64
	timestampTolerance time.Duration
	spool              *adapters.PushSpool
}

func BuildIngress(_ appconfig.Config, _ appconfig.Route, source appconfig.Source, spool *adapters.PushSpool) (*Ingress, error) {
	if err := ValidateSourceConfig(source); err != nil {
		return nil, err
	}

	secretEnv := strings.TrimSpace(source.Webhook.SecretEnv)
	secret := []byte(os.Getenv(secretEnv))
	if len(secret) == 0 {
		return nil, fmt.Errorf("source %s webhook secret_env 未设置: %s", source.Name, secretEnv)
	}

	maxBodyBytes := source.Webhook.MaxBodyBytes
	if maxBodyBytes <= 0 {
		maxBodyBytes = defaultMaxBodyBytes
	}

	timestampTolerance := source.Webhook.TimestampTolerance
	if timestampTolerance <= 0 {
		timestampTolerance = defaultTimestampTolerance
	}

	return &Ingress{
		sourceName:         firstNonEmpty(strings.TrimSpace(source.Name), "webhook"),
		listenAddr:         strings.TrimSpace(source.Webhook.ListenAddr),
		path:               strings.TrimSpace(source.Webhook.Path),
		secret:             secret,
		maxBodyBytes:       maxBodyBytes,
		timestampTolerance: timestampTolerance,
		spool:              spool,
	}, nil
}

func ValidateSourceConfig(source appconfig.Source) error {
	if source.Webhook == nil {
		return fmt.Errorf("source %s 缺少 webhook 配置", source.Name)
	}
	if strings.TrimSpace(source.Webhook.ListenAddr) == "" {
		return fmt.Errorf("source %s webhook listen addr 不能为空", source.Name)
	}
	if path := strings.TrimSpace(source.Webhook.Path); path == "" || !strings.HasPrefix(path, "/") {
		return fmt.Errorf("source %s webhook path 必须以 / 开头", source.Name)
	}
	if strings.TrimSpace(source.Webhook.SecretEnv) == "" {
		return fmt.Errorf("source %s webhook secret_env 不能为空", source.Name)
	}
	if source.Webhook.MaxBodyBytes < 0 {
		return fmt.Errorf("source %s webhook max_body_bytes 不能小于 0", source.Name)
	}
	if source.Webhook.TimestampTolerance < 0 {
		return fmt.Errorf("source %s webhook timestamp_tolerance 不能小于 0", source.Name)
	}
	return nil
}

func (w *Ingress) Run(ctx context.Context) error {
	if w == nil || w.spool == nil {
		return fmt.Errorf("webhook ingress 未初始化")
	}

	mux := http.NewServeMux()
	mux.HandleFunc(w.path, w.Handle)

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

func (w *Ingress) Handle(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !w.allowedContentType(req.Header.Get("Content-Type")) {
		http.Error(rw, "unsupported content type", http.StatusUnsupportedMediaType)
		return
	}

	timestamp, err := parseWebhookTimestamp(req.Header.Get(HeaderTimestamp))
	if err != nil {
		http.Error(rw, "invalid timestamp", http.StatusUnauthorized)
		return
	}
	if !withinTolerance(time.Now().UTC(), timestamp, w.timestampTolerance) {
		http.Error(rw, "timestamp outside tolerance window", http.StatusUnauthorized)
		return
	}

	deliveryID := strings.TrimSpace(req.Header.Get(HeaderDeliveryID))
	if deliveryID == "" {
		http.Error(rw, "missing delivery id", http.StatusUnauthorized)
		return
	}
	signature, ok := normalizeWebhookSignature(req.Header.Get(HeaderSignature))
	if !ok {
		http.Error(rw, "invalid signature", http.StatusUnauthorized)
		return
	}
	bodyHash, ok := normalizeWebhookBodyHash(req.Header.Get(HeaderBodySHA256))
	if !ok {
		http.Error(rw, "invalid body hash", http.StatusUnauthorized)
		return
	}
	if !w.validSignatureForBodyHash(req.Method, req.URL.Path, timestamp, deliveryID, bodyHash, signature) {
		http.Error(rw, "invalid signature", http.StatusUnauthorized)
		return
	}
	if req.ContentLength > 0 && req.ContentLength > w.maxBodyBytes {
		http.Error(rw, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	req.Body = http.MaxBytesReader(rw, req.Body, w.maxBodyBytes)
	bodyPath, actualBodyHash, err := spoolRequestBody(req.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			http.Error(rw, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(rw, "invalid request body", http.StatusBadRequest)
		return
	}
	defer func() {
		_ = os.Remove(bodyPath)
	}()
	if strings.TrimSpace(actualBodyHash) == "" {
		http.Error(rw, "empty request body", http.StatusBadRequest)
		return
	}
	if !sameWebhookDigest(bodyHash, actualBodyHash) {
		http.Error(rw, "body hash mismatch", http.StatusUnauthorized)
		return
	}

	bodyFile, err := os.Open(filepath.Clean(bodyPath))
	if err != nil {
		http.Error(rw, "failed to open request body", http.StatusInternalServerError)
		return
	}
	defer bodyFile.Close()

	duplicate, err := w.spool.EnqueueReader(adapters.PushMessage{
		DeliveryID:        deliveryID,
		InternetMessageID: extractInternetMessageIDFromFile(bodyPath),
		ReceivedAt:        timestamp,
		Attributes: map[string]string{
			"ingress":     "webhook",
			"delivery_id": deliveryID,
		},
	}, bodyFile)
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

func (w *Ingress) allowedContentType(value string) bool {
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

func (w *Ingress) validSignature(method, path string, timestamp time.Time, deliveryID string, body []byte, signature string) bool {
	bodySum := sha256.Sum256(body)
	return w.validSignatureForBodyHash(method, path, timestamp, deliveryID, hex.EncodeToString(bodySum[:]), signature)
}

func (w *Ingress) validSignatureForBodyHash(method, path string, timestamp time.Time, deliveryID, bodyHash, signature string) bool {
	expected := SignatureForBodyHash(w.secret, w.sourceName, method, path, timestamp, deliveryID, bodyHash)
	signature, ok := normalizeWebhookSignature(signature)
	if !ok {
		return false
	}
	if len(signature) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.ToLower(signature)), []byte(expected)) == 1
}

func normalizeWebhookSignature(signature string) (string, bool) {
	return normalizeWebhookHexDigest(signature, sha256.Size)
}

func normalizeWebhookBodyHash(bodyHash string) (string, bool) {
	return normalizeWebhookHexDigest(bodyHash, sha256.Size)
}

func normalizeWebhookHexDigest(value string, size int) (string, bool) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "sha256=")
	if len(value) != size*2 {
		return "", false
	}
	for _, ch := range value {
		switch {
		case ch >= '0' && ch <= '9':
		case ch >= 'a' && ch <= 'f':
		case ch >= 'A' && ch <= 'F':
		default:
			return "", false
		}
	}
	return strings.ToLower(value), true
}

func Signature(secret []byte, sourceName, method, path string, timestamp time.Time, deliveryID string, body []byte) string {
	bodySum := sha256.Sum256(body)
	return SignatureForBodyHash(secret, sourceName, method, path, timestamp, deliveryID, hex.EncodeToString(bodySum[:]))
}

func SignatureForBodyHash(secret []byte, sourceName, method, path string, timestamp time.Time, deliveryID, bodyHash string) string {
	payload := strings.Join([]string{
		strings.TrimSpace(sourceName),
		strings.ToUpper(strings.TrimSpace(method)),
		strings.TrimSpace(path),
		timestamp.UTC().Format(time.RFC3339),
		strings.TrimSpace(deliveryID),
		strings.TrimSpace(bodyHash),
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
		tolerance = defaultTimestampTolerance
	}
	if timestamp.After(now) {
		return timestamp.Sub(now) <= tolerance
	}
	return now.Sub(timestamp) <= tolerance
}

func extractInternetMessageIDFromFile(path string) string {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return ""
	}
	defer file.Close()
	return extractInternetMessageIDFromReader(file)
}

func extractInternetMessageIDFromReader(reader io.Reader) string {
	message, err := mail.ReadMessage(reader)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(message.Header.Get("Message-ID"))
}

func spoolRequestBody(src io.Reader) (string, string, error) {
	if src == nil {
		return "", "", fmt.Errorf("request body 不能为空")
	}

	file, err := os.CreateTemp("", "mimecrypt-webhook-body-*.eml")
	if err != nil {
		return "", "", fmt.Errorf("创建 webhook 临时文件失败: %w", err)
	}

	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}
	if err := file.Chmod(0o600); err != nil {
		cleanup()
		return "", "", fmt.Errorf("设置 webhook 临时文件权限失败: %w", err)
	}

	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, hasher), src)
	if err != nil {
		cleanup()
		return "", "", fmt.Errorf("缓存 webhook 请求体失败: %w", err)
	}
	if written == 0 {
		cleanup()
		return "", "", nil
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(file.Name())
		return "", "", fmt.Errorf("关闭 webhook 临时文件失败: %w", err)
	}

	return file.Name(), hex.EncodeToString(hasher.Sum(nil)), nil
}

func sameWebhookDigest(expected, actual string) bool {
	expected, ok := normalizeWebhookBodyHash(expected)
	if !ok {
		return false
	}
	actual, ok = normalizeWebhookBodyHash(actual)
	if !ok {
		return false
	}
	if len(expected) != len(actual) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
