package imap

import (
	"context"
	"fmt"
	"io"
	"strings"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/auth"
	"mimecrypt/internal/provider"
)

type reader struct {
	client *client
}

type writer struct {
	client *client
}

var _ provider.Reader = (*reader)(nil)
var _ provider.Writer = (*writer)(nil)
var _ provider.Reconciler = (*writer)(nil)
var _ provider.Deleter = (*writer)(nil)
var _ provider.DeleteSemanticReporter = (*writer)(nil)

func Build(cfg appconfig.Config) (provider.Clients, error) {
	authCfg := cfg.Auth
	authCfg.GraphScopes = nil
	authCfg.EWSScopes = nil

	session, err := auth.NewSession(authCfg, nil)
	if err != nil {
		return provider.Clients{}, err
	}

	imapClient, err := newClient(cfg.Mail.Client, authCfg, cfg.Mail.Sync.Folder, session, nil)
	if err != nil {
		return provider.Clients{}, err
	}

	return provider.Clients{
		Session: session,
		Reader:  &reader{client: imapClient},
		Writer:  &writer{client: imapClient},
		Deleter: &writer{client: imapClient},
	}, nil
}

func NewWriter(cfg appconfig.Config, tokenSource scopedAccessTokenSource) (provider.Writer, error) {
	authCfg := cfg.Auth
	authCfg.GraphScopes = nil
	authCfg.EWSScopes = nil

	imapClient, err := newClient(cfg.Mail.Client, authCfg, cfg.Mail.Sync.Folder, tokenSource, nil)
	if err != nil {
		return nil, err
	}

	return &writer{client: imapClient}, nil
}

func (r *reader) Me(context.Context) (provider.User, error) {
	return r.client.me(), nil
}

func (r *reader) Message(ctx context.Context, messageID string) (provider.Message, error) {
	return r.client.message(ctx, "", messageID)
}

func (r *reader) FetchMIME(ctx context.Context, messageID string) (io.ReadCloser, error) {
	return r.client.fetchMIME(ctx, "", messageID)
}

func (r *reader) DeltaCreatedMessages(ctx context.Context, folder, deltaLink string) ([]provider.Message, string, error) {
	return r.client.deltaCreatedMessages(ctx, folder, deltaLink)
}

func (r *reader) FirstMessageInFolder(ctx context.Context, folder string) (provider.Message, bool, error) {
	messages, err := r.client.latestMessagesInFolder(ctx, folder, 0, 1)
	if err != nil {
		return provider.Message{}, false, err
	}
	if len(messages) == 0 {
		return provider.Message{}, false, nil
	}
	return messages[0], true, nil
}

func (r *reader) LatestMessagesInFolder(ctx context.Context, folder string, skip, limit int) ([]provider.Message, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("limit 必须大于 0")
	}
	if skip < 0 {
		return nil, fmt.Errorf("skip 不能小于 0")
	}
	return r.client.latestMessagesInFolder(ctx, folder, skip, limit)
}

func (w *writer) WriteMessage(ctx context.Context, req provider.WriteRequest) (provider.WriteResult, error) {
	if req.DeleteSource && strings.TrimSpace(req.Source.ID) == "" {
		return provider.WriteResult{}, fmt.Errorf("原邮件 ID 不能为空")
	}
	if len(req.MIME) == 0 && req.MIMEOpener == nil {
		return provider.WriteResult{}, fmt.Errorf("回写 MIME 不能为空")
	}
	return w.client.writeMessage(ctx, req)
}

func (w *writer) ReconcileMessage(ctx context.Context, req provider.WriteRequest) (provider.WriteResult, bool, error) {
	if req.DeleteSource && strings.TrimSpace(req.Source.ID) == "" {
		return provider.WriteResult{}, false, fmt.Errorf("原邮件 ID 不能为空")
	}
	return w.client.reconcileMessage(ctx, req)
}

func (w *writer) DeleteMessage(ctx context.Context, source provider.MessageRef) error {
	if strings.TrimSpace(source.ID) == "" {
		return fmt.Errorf("原邮件 ID 不能为空")
	}
	return w.client.deleteOriginalIfExists(ctx, source)
}

func (*writer) DeleteSemantics() provider.DeleteSemantics {
	return provider.DeleteSemanticsHard
}

// Keep compiler honest about the auth session interface used by the IMAP client.
type scopedSession interface {
	provider.Session
	AccessTokenForScopes(ctx context.Context, scopes []string) (string, error)
}

var _ scopedSession = (*auth.Session)(nil)
