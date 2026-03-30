package adapters

import (
	"context"

	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/modules/writeback"
	"mimecrypt/internal/provider"
)

// WritebackConsumer 使用现有 writeback.Service 作为邮件消费端。
type WritebackConsumer struct {
	Service             *writeback.Service
	DestinationFolderID string
	Verify              bool
	Store               mailflow.StoreRef
}

func (c *WritebackConsumer) Consume(ctx context.Context, req mailflow.ConsumeRequest) (mailflow.DeliveryReceipt, error) {
	result, err := c.service().Run(ctx, writeback.Request{
		Source:              toMessageRef(req.Trace),
		MIMEOpener:          provider.MIMEOpener(req.Artifact.MIME),
		DestinationFolderID: c.DestinationFolderID,
		Verify:              c.Verify,
	})
	if err != nil {
		return mailflow.DeliveryReceipt{}, err
	}

	return mailflow.DeliveryReceipt{
		Target:   req.Target.Key(),
		Consumer: req.Target.Consumer,
		Store:    c.Store,
		Verified: result.Verified,
	}, nil
}

func (c *WritebackConsumer) service() *writeback.Service {
	if c != nil && c.Service != nil {
		return c.Service
	}
	return &writeback.Service{}
}

func toMessageRef(trace mailflow.MailTrace) provider.MessageRef {
	return provider.MessageRef{
		ID:                trace.SourceMessageID,
		InternetMessageID: trace.InternetMessageID,
		FolderID:          trace.SourceFolderID,
		ReceivedDateTime:  trace.ReceivedAt,
	}
}
