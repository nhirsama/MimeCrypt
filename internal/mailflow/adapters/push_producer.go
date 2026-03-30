package adapters

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/provider"
)

type PushProducer struct {
	Name            string
	Driver          string
	Store           mailflow.StoreRef
	Spool           *PushSpool
	DeleteSemantics provider.DeleteSemantics

	mu       sync.Mutex
	requeued bool
}

func (p *PushProducer) Next(context.Context) (mailflow.MailEnvelope, error) {
	if p == nil {
		return mailflow.MailEnvelope{}, fmt.Errorf("push producer 未配置")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.Spool == nil {
		return mailflow.MailEnvelope{}, fmt.Errorf("push spool 未配置")
	}
	if !p.requeued {
		if err := p.Spool.RequeueProcessing(); err != nil {
			return mailflow.MailEnvelope{}, err
		}
		p.requeued = true
	}

	message, found, err := p.Spool.ClaimNext()
	if err != nil {
		return mailflow.MailEnvelope{}, err
	}
	if !found {
		return mailflow.MailEnvelope{}, mailflow.ErrNoMessages
	}

	return mailflow.MailEnvelope{
		MIME: func() (io.ReadCloser, error) {
			return os.Open(message.MIMEPath)
		},
		Trace: mailflow.MailTrace{
			TransactionKey:    p.transactionKey(message.Meta.DeliveryID),
			SourceName:        strings.TrimSpace(p.Name),
			SourceDriver:      strings.TrimSpace(p.Driver),
			SourceMessageID:   message.Meta.DeliveryID,
			InternetMessageID: message.Meta.InternetMessageID,
			ReceivedAt:        message.Meta.ReceivedAt,
			SourceStore:       p.Store,
			Attributes:        cloneAttributes(message.Meta.Attributes),
		},
		SourceDeleteSemantics: p.DeleteSemantics,
		Source: &pushSourceHandle{
			key:   message.Key,
			spool: p.Spool,
		},
	}, nil
}

func (p *PushProducer) transactionKey(deliveryID string) string {
	sourceName := strings.TrimSpace(p.Name)
	if sourceName == "" {
		sourceName = firstNonEmpty(strings.TrimSpace(p.Driver), "source")
	}
	return sourceName + ":" + strings.TrimSpace(deliveryID)
}

type pushSourceHandle struct {
	key   string
	spool *PushSpool
}

func (h *pushSourceHandle) Acknowledge(context.Context) error {
	if h == nil || h.spool == nil {
		return fmt.Errorf("push source handle 未配置")
	}
	return h.spool.PrepareAck(h.key)
}

func (h *pushSourceHandle) FinalizeAcknowledge(context.Context) error {
	if h == nil || h.spool == nil {
		return fmt.Errorf("push source handle 未配置")
	}
	return h.spool.FinalizeAck(h.key)
}

func (*pushSourceHandle) Delete(context.Context) error {
	return fmt.Errorf("push 来源不支持删除")
}
