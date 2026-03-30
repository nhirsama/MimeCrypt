package adapters

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"mimecrypt/internal/mailflow"
)

func TestDiscardConsumerConsumesMail(t *testing.T) {
	t.Parallel()

	consumer := &DiscardConsumer{}
	result, err := consumer.Consume(context.Background(), mailflow.ConsumeRequest{
		Trace: mailflow.MailTrace{TransactionKey: "tx-discard"},
		Target: mailflow.DeliveryTarget{
			Name:     "discard-primary",
			Consumer: "discard",
		},
		Mail: mailflow.MailObject{
			Name: "primary",
			MIME: func() (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("encrypted-mime")), nil
			},
		},
	})
	if err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	if !result.Verified {
		t.Fatalf("Verified = false, want true")
	}
}

func TestDiscardConsumerReturnsOpenError(t *testing.T) {
	t.Parallel()

	consumer := &DiscardConsumer{}
	_, err := consumer.Consume(context.Background(), mailflow.ConsumeRequest{
		Trace: mailflow.MailTrace{TransactionKey: "tx-discard-open"},
		Target: mailflow.DeliveryTarget{
			Name:     "discard-primary",
			Consumer: "discard",
		},
		Mail: mailflow.MailObject{
			Name: "primary",
			MIME: func() (io.ReadCloser, error) {
				return nil, errors.New("open failed")
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "open failed") {
		t.Fatalf("Consume() error = %v, want open failed", err)
	}
}
