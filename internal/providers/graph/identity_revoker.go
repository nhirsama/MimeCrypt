package graph

import (
	"context"
	"fmt"
	"net/http"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/provider"
)

const revokeSessionsScope = "https://graph.microsoft.com/User.RevokeSessions.All"

type identityRevoker struct {
	client *graphClient
}

func NewIdentityRevoker(cfg appconfig.Config, session provider.Session, httpClient *http.Client) (*identityRevoker, error) {
	if session == nil {
		return nil, fmt.Errorf("session 不能为空")
	}

	client, err := newGraphClient(cfg.Mail.Client, graphTokenSource{
		session: session,
		scopes:  appendRevokeScope(cfg.Auth.GraphScopes),
	}, httpClient)
	if err != nil {
		return nil, err
	}

	return &identityRevoker{client: client}, nil
}

func (r *identityRevoker) Revoke(ctx context.Context) error {
	if r == nil || r.client == nil {
		return fmt.Errorf("identity revoker 未初始化")
	}

	req, err := r.client.newRequest(ctx, http.MethodPost, r.client.baseURL+"/me/revokeSignInSessions", nil)
	if err != nil {
		return err
	}

	var resp struct {
		Value bool `json:"value"`
	}
	if err := r.client.doJSON(req, &resp, http.StatusOK); err != nil {
		return fmt.Errorf("调用 revokeSignInSessions 失败: %w", err)
	}
	if !resp.Value {
		return fmt.Errorf("revokeSignInSessions 未确认吊销成功")
	}
	return nil
}

func appendRevokeScope(scopes []string) []string {
	result := make([]string, 0, len(scopes)+1)
	seen := make(map[string]struct{}, len(scopes)+1)

	for _, scope := range scopes {
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		result = append(result, scope)
	}
	if _, ok := seen[revokeSessionsScope]; !ok {
		result = append(result, revokeSessionsScope)
	}
	return result
}
