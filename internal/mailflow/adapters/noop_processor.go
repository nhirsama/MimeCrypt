package adapters

import (
	"context"

	"mimecrypt/internal/mailflow"
	"mimecrypt/internal/modules/encrypt"
)

// NoOpProcessor 原样透传统一邮件对象，适用于无需变换的处理场景。
type NoOpProcessor struct {
	Auditor     Auditor
	WorkDir     string
	StaticPlan  mailflow.ExecutionPlan
	PlanResolve PlanResolver
}

func (p *NoOpProcessor) Process(ctx context.Context, mail mailflow.MailEnvelope) (mailflow.ProcessedMail, error) {
	return p.ProcessWithTrace(ctx, mail, mail.Trace)
}

func (p *NoOpProcessor) ProcessWithTrace(ctx context.Context, mail mailflow.MailEnvelope, trace mailflow.MailTrace) (mailflow.ProcessedMail, error) {
	_ = ctx
	if err := mail.Validate(); err != nil {
		return mailflow.ProcessedMail{}, err
	}

	plan, err := p.resolvePlan(trace)
	if err != nil {
		return mailflow.ProcessedMail{}, err
	}

	trace = cloneTrace(trace)
	if err := ensureAttributes(&trace); err != nil {
		return mailflow.ProcessedMail{}, err
	}
	trace.Attributes["processor"] = "no-op"
	if _, ok := trace.Attributes["format"]; !ok {
		format, _, detectErr := encrypt.DetectFormatFromOpener(encrypt.MIMEOpenFunc(mail.MIME))
		if detectErr == nil && format != "" {
			trace.Attributes["format"] = format
		}
	}
	if _, ok := trace.Attributes["encrypted"]; !ok {
		if trace.Attributes["already_encrypted"] == "true" {
			trace.Attributes["encrypted"] = "true"
		} else if trace.Attributes["format"] == "" || trace.Attributes["format"] == "plain" {
			trace.Attributes["encrypted"] = "false"
		}
	}

	return mailflow.ProcessedMail{
		Trace: trace,
		Plan:  plan,
		Mail: mailflow.MailObject{
			Name:       "mail",
			MIME:       mail.MIME,
			Attributes: cloneTrace(trace).Attributes,
		},
	}, nil
}

func (p *NoOpProcessor) resolvePlan(trace mailflow.MailTrace) (mailflow.ExecutionPlan, error) {
	if p != nil && p.PlanResolve != nil {
		return p.PlanResolve(trace)
	}
	if err := p.StaticPlan.Validate(); err != nil {
		return mailflow.ExecutionPlan{}, err
	}
	return p.StaticPlan, nil
}

func cloneMailAttributes(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
