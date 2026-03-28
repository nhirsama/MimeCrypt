package list

import (
	"context"
	"fmt"
	"strings"

	"mimecrypt/internal/provider"
)

type Reader interface {
	LatestMessagesInFolder(ctx context.Context, folder string, skip, limit int) ([]provider.Message, error)
}

type Service struct {
	Client Reader
}

type Request struct {
	Folder string
	Start  int
	End    int
}

type Result struct {
	Folder   string
	Start    int
	End      int
	Messages []provider.Message
}

// Run 列出指定文件夹中按时间倒序排列的一段消息。
func (s *Service) Run(ctx context.Context, req Request) (Result, error) {
	if s.Client == nil {
		return Result{}, fmt.Errorf("mail client 不能为空")
	}
	if strings.TrimSpace(req.Folder) == "" {
		return Result{}, fmt.Errorf("folder 不能为空")
	}
	if req.Start < 0 {
		return Result{}, fmt.Errorf("start 不能小于 0")
	}
	if req.End <= req.Start {
		return Result{}, fmt.Errorf("end 必须大于 start")
	}

	messages, err := s.Client.LatestMessagesInFolder(ctx, req.Folder, req.Start, req.End-req.Start)
	if err != nil {
		return Result{}, fmt.Errorf("获取最新邮件列表失败: %w", err)
	}

	return Result{
		Folder:   req.Folder,
		Start:    req.Start,
		End:      req.End,
		Messages: messages,
	}, nil
}
