package adapters

import (
	"context"
	"fmt"
	"io"

	"mimecrypt/internal/mailflow"
)

// DiscardConsumer 用于显式消费处理产物但不持久化到任何外部系统。
type DiscardConsumer struct{}

func (c *DiscardConsumer) Consume(_ context.Context, req mailflow.ConsumeRequest) (mailflow.DeliveryReceipt, error) {
	reader, err := req.Artifact.MIME()
	if err != nil {
		return mailflow.DeliveryReceipt{}, fmt.Errorf("打开产物 MIME 失败: %w", err)
	}
	defer reader.Close()

	if _, err := io.Copy(io.Discard, reader); err != nil {
		return mailflow.DeliveryReceipt{}, fmt.Errorf("读取产物 MIME 失败: %w", err)
	}

	return mailflow.DeliveryReceipt{
		Target:   req.Target.Key(),
		Consumer: req.Target.Consumer,
		Verified: true,
	}, nil
}
