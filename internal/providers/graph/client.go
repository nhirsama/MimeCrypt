package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"mimecrypt/internal/appconfig"
)

type accessTokenSource interface {
	AccessToken(ctx context.Context) (string, error)
}

type graphClient struct {
	httpClient  *http.Client
	tokenSource accessTokenSource
	baseURL     string
}

func newGraphClient(cfg appconfig.MailClientConfig, tokenSource accessTokenSource, httpClient *http.Client) (*graphClient, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if tokenSource == nil {
		return nil, fmt.Errorf("token source 不能为空")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}

	return &graphClient{
		httpClient:  httpClient,
		tokenSource: tokenSource,
		baseURL:     strings.TrimRight(cfg.GraphBaseURL, "/"),
	}, nil
}

func (c *graphClient) newRequest(ctx context.Context, method, endpoint string, body io.Reader) (*http.Request, error) {
	token, err := c.tokenSource.AccessToken(ctx)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("构造 Graph 请求失败: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	return req, nil
}

func (c *graphClient) doJSON(req *http.Request, dst any, expectedStatus int) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("执行 Graph 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != expectedStatus {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
		return fmt.Errorf("Graph 请求失败: status=%s body=%q", resp.Status, string(body))
	}

	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return fmt.Errorf("解析 Graph 响应失败: %w", err)
	}

	return nil
}

func (c *graphClient) doEmpty(req *http.Request, expectedStatus int) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("执行 Graph 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != expectedStatus {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
		return fmt.Errorf("Graph 请求失败: status=%s body=%q", resp.Status, string(body))
	}

	return nil
}

func (c *graphClient) fetchMIMEStream(ctx context.Context, messageID string) (io.ReadCloser, error) {
	endpoint := fmt.Sprintf("%s/me/messages/%s/$value", c.baseURL, url.PathEscape(messageID))

	req, err := c.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Add("Prefer", `IdType="ImmutableId"`)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("执行 Graph MIME 请求失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
		resp.Body.Close()
		return nil, fmt.Errorf("Graph MIME 请求失败: status=%s body=%q", resp.Status, string(body))
	}

	return resp.Body, nil
}

func (c *graphClient) fetchMIMEBytes(ctx context.Context, messageID string) ([]byte, error) {
	stream, err := c.fetchMIMEStream(ctx, messageID)
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	content, err := io.ReadAll(stream)
	if err != nil {
		return nil, fmt.Errorf("读取 Graph MIME 响应失败: %w", err)
	}

	return content, nil
}
