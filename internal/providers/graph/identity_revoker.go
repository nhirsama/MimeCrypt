package graph

import (
	"context"
	"fmt"
	"io"
	"net/http"

	abstractions "github.com/microsoft/kiota-abstractions-go"
	users "github.com/microsoftgraph/msgraph-sdk-go/users"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/provider"
)

const revokeSessionsScope = "https://graph.microsoft.com/User.RevokeSessions.All"

type identityRevoker struct {
	client      *graphClient
	tokenSource provider.TokenSource
	scopes      []string
}

func NewIdentityRevoker(cfg appconfig.Config, tokenSource provider.TokenSource, httpClient *http.Client) (*identityRevoker, error) {
	if tokenSource == nil {
		return nil, fmt.Errorf("token source 不能为空")
	}

	client, err := newGraphClient(cfg.Mail.Client, graphTokenSource{
		tokenSource: tokenSource,
		scopes:      appendRevokeScope(cfg.Auth.GraphScopes),
	}, httpClient)
	if err != nil {
		return nil, err
	}

	return &identityRevoker{
		client:      client,
		tokenSource: tokenSource,
		scopes:      appendRevokeScope(cfg.Auth.GraphScopes),
	}, nil
}

func (r *identityRevoker) Revoke(ctx context.Context, out io.Writer) error {
	if r == nil || r.client == nil {
		return fmt.Errorf("identity revoker 未初始化")
	}
	if err := r.ensureAuthorization(ctx, out); err != nil {
		return err
	}

	requestInfo, err := r.client.newRequest(abstractions.POST, r.client.baseURL+"/me/revokeSignInSessions")
	if err != nil {
		return err
	}
	requestInfo.Headers.Add("Accept", "application/json")

	parsed, err := r.client.doParsable(ctx, requestInfo, users.CreateItemRevokeSignInSessionsPostResponseFromDiscriminatorValue)
	if err != nil {
		return fmt.Errorf("调用 revokeSignInSessions 失败: %w", err)
	}

	resp, ok := parsed.(users.ItemRevokeSignInSessionsPostResponseable)
	if !ok {
		return fmt.Errorf("revokeSignInSessions 响应类型异常: %T", parsed)
	}
	if resp.GetValue() == nil || !*resp.GetValue() {
		return fmt.Errorf("revokeSignInSessions 未确认吊销成功")
	}
	return nil
}

func (r *identityRevoker) ensureAuthorization(ctx context.Context, out io.Writer) error {
	if r == nil || r.tokenSource == nil {
		return fmt.Errorf("identity revoker token source 未初始化")
	}

	type interactiveScopeTokenSource interface {
		EnsureAccessTokenForScopes(context.Context, []string, io.Writer) (string, error)
	}

	if interactive, ok := r.tokenSource.(interactiveScopeTokenSource); ok {
		if _, err := interactive.EnsureAccessTokenForScopes(ctx, r.scopes, out); err != nil {
			return fmt.Errorf("获取远端吊销授权失败: %w", err)
		}
		return nil
	}

	if _, err := r.tokenSource.AccessTokenForScopes(ctx, r.scopes); err != nil {
		return fmt.Errorf("获取远端吊销授权失败: %w", err)
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
