package audit

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Event struct {
	Timestamp         time.Time `json:"timestamp"`
	Event             string    `json:"event"`
	MessageID         string    `json:"message_id,omitempty"`
	InternetMessageID string    `json:"internet_message_id,omitempty"`
	Format            string    `json:"format,omitempty"`
	Encrypted         bool      `json:"encrypted,omitempty"`
	AlreadyEncrypted  bool      `json:"already_encrypted,omitempty"`
	BackupPath        string    `json:"backup_path,omitempty"`
	SourceFolderID    string    `json:"source_folder_id,omitempty"`
	DestinationFolder string    `json:"destination_folder,omitempty"`
	WroteBack         bool      `json:"wrote_back,omitempty"`
	Verified          bool      `json:"verified,omitempty"`
	Error             string    `json:"error,omitempty"`
}

type Service struct {
	Path   string
	Stdout bool
	Writer io.Writer
	Now    func() time.Time
}

func (s *Service) Record(event Event) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = s.now().UTC()
	}

	content, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("序列化审计日志失败: %w", err)
	}
	content = append(content, '\n')

	if !s.hasOutput() {
		return fmt.Errorf("审计输出不能为空")
	}
	if path := strings.TrimSpace(s.Path); path != "" {
		if err := writeAuditFile(path, content); err != nil {
			return err
		}
	}
	if s.Stdout {
		if _, err := s.stdoutWriter().Write(content); err != nil {
			return fmt.Errorf("写入 stdout 审计日志失败: %w", err)
		}
	}

	return nil
}

func writeAuditFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("创建审计日志目录失败: %w", err)
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("打开审计日志失败: %w", err)
	}

	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return fmt.Errorf("写入审计日志失败: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("关闭审计日志失败: %w", err)
	}
	return nil
}

func (s *Service) hasOutput() bool {
	return strings.TrimSpace(s.Path) != "" || s.Stdout
}

func (s *Service) stdoutWriter() io.Writer {
	if s != nil && s.Writer != nil {
		return s.Writer
	}
	return os.Stdout
}

func (s *Service) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
