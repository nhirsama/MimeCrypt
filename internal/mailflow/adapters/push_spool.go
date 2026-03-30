package adapters

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mimecrypt/internal/fileutil"
)

const (
	pushSpoolDirPending    = "pending"
	pushSpoolDirProcessing = "processing"
	pushSpoolDirSeen       = "seen"
	pushSpoolDirStaging    = "staging"
	pushSpoolMetaFile      = "meta.json"
	pushSpoolMIMEFile      = "message.eml"
)

type PushMessage struct {
	DeliveryID        string
	InternetMessageID string
	ReceivedAt        time.Time
	MIME              []byte
	Attributes        map[string]string
}

type PushMessageMeta struct {
	DeliveryID        string            `json:"delivery_id"`
	InternetMessageID string            `json:"internet_message_id,omitempty"`
	ReceivedAt        time.Time         `json:"received_at"`
	Attributes        map[string]string `json:"attributes,omitempty"`
}

type ClaimedPushMessage struct {
	Key      string
	Meta     PushMessageMeta
	MIMEPath string
}

type PushSpool struct {
	Dir             string
	ReplayRetention time.Duration
	Now             func() time.Time
}

type pushSeenMarker struct {
	DeliveryID string    `json:"delivery_id"`
	SeenAt     time.Time `json:"seen_at"`
}

func (s *PushSpool) Enqueue(message PushMessage) (bool, error) {
	return s.EnqueueReader(message, bytes.NewReader(message.MIME))
}

func (s *PushSpool) EnqueueReader(message PushMessage, mime io.Reader) (bool, error) {
	if strings.TrimSpace(message.DeliveryID) == "" {
		return false, fmt.Errorf("delivery id 不能为空")
	}
	if mime == nil {
		return false, fmt.Errorf("push MIME 不能为空")
	}
	if message.ReceivedAt.IsZero() {
		message.ReceivedAt = s.now().UTC()
	} else {
		message.ReceivedAt = message.ReceivedAt.UTC()
	}
	if err := s.ensureDirs(); err != nil {
		return false, err
	}
	if err := s.cleanupSeen(); err != nil {
		return false, err
	}

	key := s.messageKey(message.DeliveryID)
	fresh, err := s.hasFreshSeenMarker(key)
	if err != nil {
		return false, err
	}
	if fresh || s.hasQueuedMessage(key) {
		return true, nil
	}

	stagingDir, err := os.MkdirTemp(s.stagingDir(), key+"-*")
	if err != nil {
		return false, fmt.Errorf("创建 push staging 目录失败: %w", err)
	}

	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(stagingDir)
		}
	}()

	if err := os.Chmod(stagingDir, 0o700); err != nil {
		return false, fmt.Errorf("设置 push staging 目录权限失败: %w", err)
	}

	meta := PushMessageMeta{
		DeliveryID:        message.DeliveryID,
		InternetMessageID: strings.TrimSpace(message.InternetMessageID),
		ReceivedAt:        message.ReceivedAt,
		Attributes:        cloneAttributes(message.Attributes),
	}
	if err := writeJSONFile(filepath.Join(stagingDir, pushSpoolMetaFile), meta); err != nil {
		return false, err
	}
	written, err := fileutil.WriteFileAtomic(filepath.Join(stagingDir, pushSpoolMIMEFile), 0o600, mime)
	if err != nil {
		return false, fmt.Errorf("写入 push MIME 失败: %w", err)
	}
	if written == 0 {
		return false, fmt.Errorf("push MIME 不能为空")
	}

	if err := os.Rename(stagingDir, s.pendingPath(key)); err != nil {
		if os.IsExist(err) {
			return true, nil
		}
		return false, fmt.Errorf("提交 push spool 消息失败: %w", err)
	}

	cleanup = false
	return false, nil
}

func (s *PushSpool) ClaimNext() (ClaimedPushMessage, bool, error) {
	if err := s.ensureDirs(); err != nil {
		return ClaimedPushMessage{}, false, err
	}

	type pendingCandidate struct {
		key  string
		meta PushMessageMeta
	}

	entries, err := os.ReadDir(s.pendingDir())
	if err != nil {
		return ClaimedPushMessage{}, false, fmt.Errorf("读取 push pending 目录失败: %w", err)
	}

	candidates := make([]pendingCandidate, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		key := entry.Name()
		meta, err := s.readMeta(filepath.Join(s.pendingDir(), key))
		if err != nil {
			return ClaimedPushMessage{}, false, err
		}
		candidates = append(candidates, pendingCandidate{key: key, meta: meta})
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].meta.ReceivedAt.Equal(candidates[j].meta.ReceivedAt) {
			return candidates[i].key < candidates[j].key
		}
		return candidates[i].meta.ReceivedAt.Before(candidates[j].meta.ReceivedAt)
	})

	for _, candidate := range candidates {
		pendingPath := s.pendingPath(candidate.key)
		processingPath := s.processingPath(candidate.key)
		if err := os.Rename(pendingPath, processingPath); err != nil {
			if os.IsNotExist(err) || os.IsExist(err) {
				continue
			}
			return ClaimedPushMessage{}, false, fmt.Errorf("切换 push 消息到 processing 失败: %w", err)
		}
		meta, err := s.readMeta(processingPath)
		if err != nil {
			return ClaimedPushMessage{}, false, err
		}
		return ClaimedPushMessage{
			Key:      candidate.key,
			Meta:     meta,
			MIMEPath: filepath.Join(processingPath, pushSpoolMIMEFile),
		}, true, nil
	}

	return ClaimedPushMessage{}, false, nil
}

func (s *PushSpool) Ack(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("push key 不能为空")
	}

	if err := s.ensureDirs(); err != nil {
		return err
	}

	processingPath := s.processingPath(key)
	if _, err := os.Stat(processingPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取 push processing 消息失败: %w", err)
	}

	meta, err := s.readMeta(processingPath)
	if err != nil {
		return err
	}
	if err := s.writeSeenMarker(key, pushSeenMarker{
		DeliveryID: meta.DeliveryID,
		SeenAt:     s.now().UTC(),
	}); err != nil && !os.IsExist(err) {
		return fmt.Errorf("写入 push seen marker 失败: %w", err)
	}

	if err := os.RemoveAll(processingPath); err != nil {
		return fmt.Errorf("删除 push processing 消息失败: %w", err)
	}
	return nil
}

func (s *PushSpool) RequeueProcessing() error {
	if err := s.ensureDirs(); err != nil {
		return err
	}
	entries, err := os.ReadDir(s.processingDir())
	if err != nil {
		return fmt.Errorf("读取 push processing 目录失败: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		key := entry.Name()
		processingPath := s.processingPath(key)
		pendingPath := s.pendingPath(key)
		if _, err := os.Stat(pendingPath); err == nil {
			continue
		}
		if err := os.Rename(processingPath, pendingPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("回收 processing push 消息失败: %w", err)
		}
	}
	return nil
}

func (s *PushSpool) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *PushSpool) ensureDirs() error {
	if strings.TrimSpace(s.Dir) == "" {
		return fmt.Errorf("push spool dir 不能为空")
	}
	for _, dir := range []string{s.pendingDir(), s.processingDir(), s.seenDir(), s.stagingDir()} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("创建 push spool 目录失败: %w", err)
		}
	}
	return nil
}

func (s *PushSpool) cleanupSeen() error {
	retention := s.ReplayRetention
	if retention <= 0 {
		return nil
	}
	entries, err := os.ReadDir(s.seenDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取 push seen 目录失败: %w", err)
	}
	cutoff := s.now().UTC().Add(-retention)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(s.seenDir(), entry.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("读取 push seen marker 失败: %w", err)
		}
		var marker pushSeenMarker
		if err := json.Unmarshal(content, &marker); err != nil {
			return fmt.Errorf("解析 push seen marker 失败: %w", err)
		}
		key := strings.TrimSuffix(entry.Name(), ".json")
		if marker.SeenAt.After(cutoff) || s.hasQueuedMessage(key) {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("删除过期 push seen marker 失败: %w", err)
		}
	}
	return nil
}

func (s *PushSpool) hasFreshSeenMarker(key string) (bool, error) {
	content, err := os.ReadFile(s.seenPath(key))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("读取 push seen marker 失败: %w", err)
	}
	var marker pushSeenMarker
	if err := json.Unmarshal(content, &marker); err != nil {
		return false, fmt.Errorf("解析 push seen marker 失败: %w", err)
	}
	if s.ReplayRetention <= 0 {
		return true, nil
	}
	return marker.SeenAt.After(s.now().UTC().Add(-s.ReplayRetention)), nil
}

func (s *PushSpool) writeSeenMarker(key string, marker pushSeenMarker) error {
	content, err := json.Marshal(marker)
	if err != nil {
		return fmt.Errorf("序列化 push seen marker 失败: %w", err)
	}
	file, err := os.OpenFile(s.seenPath(key), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(content, '\n')); err != nil {
		_ = file.Close()
		return fmt.Errorf("写入 push seen marker 失败: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("关闭 push seen marker 失败: %w", err)
	}
	return nil
}

func (s *PushSpool) hasQueuedMessage(key string) bool {
	for _, path := range []string{s.pendingPath(key), s.processingPath(key)} {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

func (s *PushSpool) readMeta(dir string) (PushMessageMeta, error) {
	content, err := os.ReadFile(filepath.Join(dir, pushSpoolMetaFile))
	if err != nil {
		return PushMessageMeta{}, fmt.Errorf("读取 push 元数据失败: %w", err)
	}
	var meta PushMessageMeta
	if err := json.Unmarshal(content, &meta); err != nil {
		return PushMessageMeta{}, fmt.Errorf("解析 push 元数据失败: %w", err)
	}
	return meta, nil
}

func (s *PushSpool) pendingDir() string {
	return filepath.Join(strings.TrimSpace(s.Dir), pushSpoolDirPending)
}

func (s *PushSpool) processingDir() string {
	return filepath.Join(strings.TrimSpace(s.Dir), pushSpoolDirProcessing)
}

func (s *PushSpool) seenDir() string {
	return filepath.Join(strings.TrimSpace(s.Dir), pushSpoolDirSeen)
}

func (s *PushSpool) stagingDir() string {
	return filepath.Join(strings.TrimSpace(s.Dir), pushSpoolDirStaging)
}

func (s *PushSpool) pendingPath(key string) string {
	return filepath.Join(s.pendingDir(), key)
}

func (s *PushSpool) processingPath(key string) string {
	return filepath.Join(s.processingDir(), key)
}

func (s *PushSpool) seenPath(key string) string {
	return filepath.Join(s.seenDir(), key+".json")
}

func (s *PushSpool) messageKey(deliveryID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(deliveryID)))
	return hex.EncodeToString(sum[:])
}

func writeJSONFile(path string, value any) error {
	content, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("序列化 JSON 失败: %w", err)
	}
	if _, err := fileutil.WriteFileAtomic(path, 0o600, bytes.NewReader(append(content, '\n'))); err != nil {
		return fmt.Errorf("写入 JSON 文件失败: %w", err)
	}
	return nil
}

func cloneAttributes(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(src))
	for key, value := range src {
		cloned[key] = value
	}
	return cloned
}
