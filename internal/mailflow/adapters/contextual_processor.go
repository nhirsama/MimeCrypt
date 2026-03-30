package adapters

import (
	"context"
	"fmt"

	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/modules/encrypt"
)

// ContextualProcessor 根据邮件上下文选择具体处理器。
// 当前内置策略为：已加密邮件走 no-op；未加密邮件走 MIME 加密。
type ContextualProcessor struct {
	Encrypting *EncryptingProcessor
	NoOp       *NoOpProcessor
}

func (p *ContextualProcessor) Process(ctx context.Context, mail mailflow.MailEnvelope) (mailflow.ProcessedMail, error) {
	if p == nil {
		return mailflow.ProcessedMail{}, fmt.Errorf("processor 未配置")
	}

	format, encrypted, err := encrypt.DetectFormatFromOpener(encrypt.MIMEOpenFunc(mail.MIME))
	if err != nil {
		return mailflow.ProcessedMail{}, fmt.Errorf("检测 MIME 加密格式失败: %w", err)
	}
	if encrypted {
		trace := cloneTrace(mail.Trace)
		if err := ensureAttributes(&trace); err != nil {
			return mailflow.ProcessedMail{}, err
		}
		trace.Attributes["already_encrypted"] = "true"
		trace.Attributes["encrypted"] = "true"
		trace.Attributes["format"] = format
		return p.noOp().ProcessWithTrace(ctx, mail, trace)
	}
	return p.encrypting().Process(ctx, mail)
}

func (p *ContextualProcessor) encrypting() *EncryptingProcessor {
	if p != nil && p.Encrypting != nil {
		return p.Encrypting
	}
	return &EncryptingProcessor{}
}

func (p *ContextualProcessor) noOp() *NoOpProcessor {
	if p != nil && p.NoOp != nil {
		return p.NoOp
	}
	return &NoOpProcessor{}
}
