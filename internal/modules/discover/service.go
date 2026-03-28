package discover

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"mimecrypt/internal/modules/encrypt"
	"mimecrypt/internal/modules/process"
	"mimecrypt/internal/provider"
)

type Processor interface {
	Run(ctx context.Context, req process.Request) (process.Result, error)
}

type Service struct {
	Client    provider.Reader
	Processor Processor
}

type Request struct {
	Folder          string
	StatePath       string
	IncludeExisting bool
	Process         process.Request
}

type Result struct {
	Bootstrapped bool
	Skipped      int
	Processed    int
	DeltaLink    string
}

type DebugResult struct {
	Found   bool
	Process process.Result
}

type syncState struct {
	DeltaLink       string             `json:"delta_link"`
	NextDeltaLink   string             `json:"next_delta_link,omitempty"`
	PendingMessages []provider.Message `json:"pending_messages,omitempty"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

type syncStateStore struct {
	path string
}

// RunCycle 发现新增邮件并交给处理模块执行。
func (s *Service) RunCycle(ctx context.Context, req Request) (Result, error) {
	store := syncStateStore{path: req.StatePath}

	state, err := store.load()
	if err != nil {
		return Result{}, err
	}

	result := Result{
		Bootstrapped: state.DeltaLink == "" && len(state.PendingMessages) == 0,
	}

	messages := append([]provider.Message(nil), state.PendingMessages...)
	nextDelta := state.NextDeltaLink

	if len(messages) == 0 {
		messages, nextDelta, err = s.Client.DeltaCreatedMessages(ctx, req.Folder, state.DeltaLink)
		if err != nil {
			return Result{}, err
		}
		result.DeltaLink = nextDelta
	} else if nextDelta == "" {
		return Result{}, fmt.Errorf("同步状态损坏：存在待处理邮件但缺少 next delta link")
	} else {
		result.DeltaLink = nextDelta
	}

	if result.Bootstrapped && !req.IncludeExisting {
		result.Skipped = len(messages)
		if err := store.save(syncState{DeltaLink: nextDelta}); err != nil {
			return Result{}, err
		}
		return result, nil
	}

	if len(messages) == 0 {
		if err := store.save(syncState{DeltaLink: nextDelta}); err != nil {
			return Result{}, err
		}
		return result, nil
	}

	if len(state.PendingMessages) == 0 {
		if err := store.save(syncState{
			DeltaLink:       state.DeltaLink,
			NextDeltaLink:   nextDelta,
			PendingMessages: messages,
		}); err != nil {
			return Result{}, err
		}
	}

	for i, message := range messages {
		processReq := req.Process
		processReq.Source = message.Ref().WithFallbackFolder(req.Folder)
		_, procErr := s.Processor.Run(ctx, processReq)
		if procErr != nil && !errors.Is(procErr, encrypt.ErrAlreadyEncrypted) {
			return Result{}, fmt.Errorf("处理邮件 %s 失败: %w", message.ID, procErr)
		}

		remaining := messages[i+1:]
		nextState := syncState{DeltaLink: nextDelta}
		if len(remaining) > 0 {
			nextState = syncState{
				DeltaLink:       state.DeltaLink,
				NextDeltaLink:   nextDelta,
				PendingMessages: remaining,
			}
		}
		if err := store.save(nextState); err != nil {
			return Result{}, err
		}

		if !errors.Is(procErr, encrypt.ErrAlreadyEncrypted) {
			result.Processed++
		}
	}

	return result, nil
}

// DebugFirst 在调试模式下抓取当前文件夹中的第一封邮件并交给处理模块。
func (s *Service) DebugFirst(ctx context.Context, req Request) (DebugResult, error) {
	message, found, err := s.Client.FirstMessageInFolder(ctx, req.Folder)
	if err != nil {
		return DebugResult{}, err
	}
	if !found {
		return DebugResult{Found: false}, nil
	}

	processReq := req.Process
	processReq.Source = message.Ref().WithFallbackFolder(req.Folder)

	processResult, err := s.Processor.Run(ctx, processReq)
	if err != nil {
		return DebugResult{}, err
	}

	return DebugResult{
		Found:   true,
		Process: processResult,
	}, nil
}

func (s syncStateStore) load() (syncState, error) {
	if s.path == "" {
		return syncState{}, fmt.Errorf("同步状态路径不能为空")
	}

	content, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return syncState{}, nil
		}
		return syncState{}, fmt.Errorf("读取同步状态失败: %w", err)
	}

	var state syncState
	if err := json.Unmarshal(content, &state); err != nil {
		return syncState{}, fmt.Errorf("解析同步状态失败: %w", err)
	}

	return state, nil
}

func (s syncStateStore) save(state syncState) error {
	if s.path == "" {
		return fmt.Errorf("同步状态路径不能为空")
	}

	state.UpdatedAt = time.Now()
	return writeJSONFile(s.path, state, 0o600)
}

func writeJSONFile(path string, value any, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("创建状态目录失败: %w", err)
	}

	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化状态失败: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, content, mode); err != nil {
		return fmt.Errorf("写入临时状态文件失败: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("替换状态文件失败: %w", err)
	}

	return nil
}
