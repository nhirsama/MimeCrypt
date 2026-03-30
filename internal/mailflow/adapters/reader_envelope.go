package adapters

import (
	"context"
	"fmt"
	"io"
	"strings"

	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/provider"
)

// ReaderEnvelopeBuilder 将 provider.Reader 暴露的单封邮件元数据转换为 mailflow.MailEnvelope。
type ReaderEnvelopeBuilder struct {
	Name            string
	Driver          string
	Folder          string
	Store           mailflow.StoreRef
	Reader          provider.Reader
	Deleter         provider.Deleter
	DeleteSemantics provider.DeleteSemantics
}

func (b *ReaderEnvelopeBuilder) EnvelopeForMessage(ctx context.Context, message provider.Message) (mailflow.MailEnvelope, error) {
	if b == nil || b.Reader == nil {
		return mailflow.MailEnvelope{}, fmt.Errorf("reader 未配置")
	}
	if strings.TrimSpace(message.ID) == "" {
		return mailflow.MailEnvelope{}, fmt.Errorf("message id 不能为空")
	}

	ref := message.Ref().WithFallbackFolder(b.Folder)
	return mailflow.MailEnvelope{
		MIME: func() (io.ReadCloser, error) {
			return b.Reader.FetchMIME(ctx, message.ID)
		},
		Trace: mailflow.MailTrace{
			TransactionKey:    b.transactionKey(message),
			SourceName:        strings.TrimSpace(b.Name),
			SourceDriver:      strings.TrimSpace(b.Driver),
			SourceMessageID:   message.ID,
			SourceFolderID:    ref.FolderID,
			InternetMessageID: message.InternetMessageID,
			ReceivedAt:        message.ReceivedDateTime,
			SourceStore:       b.Store,
		},
		SourceDeleteSemantics: b.DeleteSemantics,
		Source: &singleMessageSourceHandle{
			message: ref,
			deleter: b.Deleter,
		},
	}, nil
}

func (b *ReaderEnvelopeBuilder) EnvelopeForID(ctx context.Context, messageID string, fallbackFolder string) (mailflow.MailEnvelope, error) {
	if b == nil || b.Reader == nil {
		return mailflow.MailEnvelope{}, fmt.Errorf("reader 未配置")
	}
	message, err := b.Reader.Message(ctx, messageID)
	if err != nil {
		return mailflow.MailEnvelope{}, err
	}
	if strings.TrimSpace(message.ParentFolderID) == "" {
		message.ParentFolderID = firstNonEmpty(fallbackFolder, b.Folder)
	}
	return b.EnvelopeForMessage(ctx, message)
}

func (b *ReaderEnvelopeBuilder) transactionKey(message provider.Message) string {
	sourceName := strings.TrimSpace(b.Name)
	if sourceName == "" {
		sourceName = firstNonEmpty(strings.TrimSpace(b.Driver), "source")
	}
	return sourceName + ":" + strings.TrimSpace(message.ID)
}

type singleMessageSourceHandle struct {
	message provider.MessageRef
	deleter provider.Deleter
}

func (h *singleMessageSourceHandle) Acknowledge(context.Context) error {
	return nil
}

func (h *singleMessageSourceHandle) Delete(ctx context.Context) error {
	if h.deleter == nil {
		return fmt.Errorf("来源不支持删除")
	}
	return h.deleter.DeleteMessage(ctx, h.message)
}
