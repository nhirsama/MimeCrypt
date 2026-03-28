package imap

import (
	"context"
	"fmt"

	"mimecrypt/internal/provider"
)

var _ provider.HealthProber = (*writer)(nil)

func (w *writer) HealthCheck(ctx context.Context) (string, error) {
	if w == nil || w.client == nil {
		return "", fmt.Errorf("imap writer 未初始化")
	}

	_, cleanup, err := w.client.connectClient(ctx)
	if err != nil {
		return "", err
	}
	defer cleanup()

	return "imap addr=" + w.client.addr, nil
}
