package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mimecrypt/internal/fileutil"
	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/provider"
)

// PollingProducer 基于 provider.Reader 的增量接口逐封产出邮件。
type PollingProducer struct {
	Name            string
	Driver          string
	Folder          string
	StatePath       string
	IncludeExisting bool
	Store           mailflow.StoreRef
	Reader          provider.Reader
	Deleter         provider.Deleter

	mu sync.Mutex
}

type pollingState struct {
	DeltaLink       string             `json:"delta_link"`
	NextDeltaLink   string             `json:"next_delta_link,omitempty"`
	PendingMessages []provider.Message `json:"pending_messages,omitempty"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

func (p *PollingProducer) Next(ctx context.Context) (mailflow.MailEnvelope, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p == nil || p.Reader == nil {
		return mailflow.MailEnvelope{}, fmt.Errorf("reader 未配置")
	}
	if strings.TrimSpace(p.StatePath) == "" {
		return mailflow.MailEnvelope{}, fmt.Errorf("state path 不能为空")
	}

	state, err := p.loadState()
	if err != nil {
		return mailflow.MailEnvelope{}, err
	}

	bootstrapped := state.DeltaLink == "" && len(state.PendingMessages) == 0
	messages := append([]provider.Message(nil), state.PendingMessages...)
	nextDelta := state.NextDeltaLink

	if len(messages) == 0 {
		messages, nextDelta, err = p.Reader.DeltaCreatedMessages(ctx, p.Folder, state.DeltaLink)
		if err != nil {
			return mailflow.MailEnvelope{}, err
		}
	} else if nextDelta == "" {
		return mailflow.MailEnvelope{}, fmt.Errorf("producer 状态损坏：存在待处理邮件但缺少 next delta link")
	}

	if bootstrapped && !p.IncludeExisting {
		if err := p.saveState(pollingState{DeltaLink: nextDelta}); err != nil {
			return mailflow.MailEnvelope{}, err
		}
		return mailflow.MailEnvelope{}, mailflow.ErrNoMessages
	}

	if len(messages) == 0 {
		if err := p.saveState(pollingState{DeltaLink: nextDelta}); err != nil {
			return mailflow.MailEnvelope{}, err
		}
		return mailflow.MailEnvelope{}, mailflow.ErrNoMessages
	}

	if len(state.PendingMessages) == 0 {
		if err := p.saveState(pollingState{
			DeltaLink:       state.DeltaLink,
			NextDeltaLink:   nextDelta,
			PendingMessages: messages,
		}); err != nil {
			return mailflow.MailEnvelope{}, err
		}
	}

	message := messages[0]
	source := &pollingSourceHandle{
		statePath: p.StatePath,
		message:   message.Ref().WithFallbackFolder(p.Folder),
		deleter:   p.Deleter,
	}

	return mailflow.MailEnvelope{
		MIME: func() (io.ReadCloser, error) {
			return p.Reader.FetchMIME(ctx, message.ID)
		},
		Trace: mailflow.MailTrace{
			TransactionKey:    p.transactionKey(message),
			SourceName:        strings.TrimSpace(p.Name),
			SourceDriver:      strings.TrimSpace(p.Driver),
			SourceMessageID:   message.ID,
			SourceFolderID:    firstNonEmpty(message.ParentFolderID, p.Folder),
			InternetMessageID: message.InternetMessageID,
			ReceivedAt:        message.ReceivedDateTime,
			SourceStore:       p.Store,
		},
		Source: source,
	}, nil
}

func (p *PollingProducer) transactionKey(message provider.Message) string {
	sourceName := strings.TrimSpace(p.Name)
	if sourceName == "" {
		sourceName = firstNonEmpty(strings.TrimSpace(p.Driver), "source")
	}
	return sourceName + ":" + strings.TrimSpace(message.ID)
}

func (p *PollingProducer) loadState() (pollingState, error) {
	content, err := os.ReadFile(p.StatePath)
	if err != nil {
		if os.IsNotExist(err) {
			return pollingState{}, nil
		}
		return pollingState{}, fmt.Errorf("读取 producer 状态失败: %w", err)
	}

	var state pollingState
	if err := json.Unmarshal(content, &state); err != nil {
		return pollingState{}, fmt.Errorf("解析 producer 状态失败: %w", err)
	}
	return state, nil
}

func (p *PollingProducer) saveState(state pollingState) error {
	state.UpdatedAt = time.Now().UTC()
	if err := os.MkdirAll(filepath.Dir(p.StatePath), 0o700); err != nil {
		return fmt.Errorf("创建 producer 状态目录失败: %w", err)
	}
	content, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 producer 状态失败: %w", err)
	}
	content = append(content, '\n')
	if _, err := fileutil.WriteFileAtomic(p.StatePath, 0o600, bytes.NewReader(content)); err != nil {
		return fmt.Errorf("保存 producer 状态失败: %w", err)
	}
	return nil
}

type pollingSourceHandle struct {
	statePath string
	message   provider.MessageRef
	deleter   provider.Deleter
}

func (h *pollingSourceHandle) Acknowledge(context.Context) error {
	state, err := h.loadState()
	if err != nil {
		return err
	}
	if len(state.PendingMessages) == 0 {
		return nil
	}

	remaining := make([]provider.Message, 0, len(state.PendingMessages))
	removed := false
	for _, message := range state.PendingMessages {
		if !removed && message.ID == h.message.ID {
			removed = true
			continue
		}
		remaining = append(remaining, message)
	}
	if !removed {
		return nil
	}

	nextState := pollingState{DeltaLink: state.NextDeltaLink}
	if len(remaining) > 0 {
		nextState = pollingState{
			DeltaLink:       state.DeltaLink,
			NextDeltaLink:   state.NextDeltaLink,
			PendingMessages: remaining,
		}
	}
	return h.saveState(nextState)
}

func (h *pollingSourceHandle) Delete(ctx context.Context) error {
	if h.deleter == nil {
		return fmt.Errorf("来源不支持删除")
	}
	return h.deleter.DeleteMessage(ctx, h.message)
}

func (h *pollingSourceHandle) loadState() (pollingState, error) {
	content, err := os.ReadFile(h.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return pollingState{}, nil
		}
		return pollingState{}, fmt.Errorf("读取 producer 状态失败: %w", err)
	}
	var state pollingState
	if err := json.Unmarshal(content, &state); err != nil {
		return pollingState{}, fmt.Errorf("解析 producer 状态失败: %w", err)
	}
	return state, nil
}

func (h *pollingSourceHandle) saveState(state pollingState) error {
	state.UpdatedAt = time.Now().UTC()
	if err := os.MkdirAll(filepath.Dir(h.statePath), 0o700); err != nil {
		return fmt.Errorf("创建 producer 状态目录失败: %w", err)
	}
	content, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 producer 状态失败: %w", err)
	}
	content = append(content, '\n')
	if _, err := fileutil.WriteFileAtomic(h.statePath, 0o600, bytes.NewReader(content)); err != nil {
		return fmt.Errorf("保存 producer 状态失败: %w", err)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
