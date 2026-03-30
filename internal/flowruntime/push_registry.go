package flowruntime

import (
	"context"

	"mimecrypt/internal/mailflow/adapters"
)

type pushIngress interface {
	Run(ctx context.Context) error
}

type pushIngressBuilder func(run SourceRun, spool *adapters.PushSpool) (pushIngress, error)

var pushIngressBuilders = map[string]pushIngressBuilder{
	"webhook": buildWebhookIngress,
}

func lookupPushIngressBuilder(driver string) (pushIngressBuilder, bool) {
	builder, ok := pushIngressBuilders[normalizeDriver(driver, "")]
	return builder, ok
}
