package mail

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
	"mimecrypt/internal/provider"
)

type TokenSource interface {
	AccessToken(ctx context.Context) (string, error)
}

type Client struct {
	httpClient  *http.Client
	tokenSource TokenSource
	cfg         appconfig.MailConfig
}

type User = provider.User
type Message = provider.Message

type deltaResponse struct {
	Value     []Message `json:"value"`
	NextLink  string    `json:"@odata.nextLink"`
	DeltaLink string    `json:"@odata.deltaLink"`
}

type listMessagesResponse struct {
	Value []Message `json:"value"`
}

var _ provider.Reader = (*Client)(nil)

// NewClient 创建一个只负责邮件读取的 Graph 客户端。
func NewClient(cfg appconfig.MailConfig, tokenSource TokenSource, httpClient *http.Client) (*Client, error) {
	if err := cfg.ValidateClient(); err != nil {
		return nil, err
	}
	if tokenSource == nil {
		return nil, fmt.Errorf("token source 不能为空")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}

	return &Client{
		httpClient:  httpClient,
		tokenSource: tokenSource,
		cfg:         cfg,
	}, nil
}

// Me 返回当前登录用户的基础信息，便于验证登录状态。
func (c *Client) Me(ctx context.Context) (User, error) {
	endpoint := fmt.Sprintf("%s/me?$select=id,displayName,mail,userPrincipalName", strings.TrimRight(c.cfg.GraphBaseURL, "/"))

	req, err := c.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return User{}, err
	}

	var user User
	if err := c.doJSON(req, &user); err != nil {
		return User{}, err
	}

	return user, nil
}

// Message 返回指定邮件的基础元数据。
func (c *Client) Message(ctx context.Context, messageID string) (Message, error) {
	query := url.Values{}
	query.Set("$select", "id,subject,receivedDateTime,internetMessageId,parentFolderId")

	endpoint := fmt.Sprintf(
		"%s/me/messages/%s?%s",
		strings.TrimRight(c.cfg.GraphBaseURL, "/"),
		url.PathEscape(messageID),
		query.Encode(),
	)

	req, err := c.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Message{}, err
	}

	req.Header.Add("Prefer", `IdType="ImmutableId"`)

	var message Message
	if err := c.doJSON(req, &message); err != nil {
		return Message{}, err
	}

	return message, nil
}

// DeltaCreatedMessages 读取指定文件夹的增量消息列表。
func (c *Client) DeltaCreatedMessages(ctx context.Context, folder, deltaLink string) ([]Message, string, error) {
	endpoint := deltaLink
	if endpoint == "" {
		query := url.Values{}
		query.Set("changeType", "created")
		query.Set("$select", "id,subject,receivedDateTime,internetMessageId,parentFolderId")

		endpoint = fmt.Sprintf(
			"%s/me/mailFolders/%s/messages/delta?%s",
			strings.TrimRight(c.cfg.GraphBaseURL, "/"),
			url.PathEscape(folder),
			query.Encode(),
		)
	}

	var (
		allMessages []Message
		finalDelta  string
	)

	for endpoint != "" {
		req, err := c.newRequest(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, "", err
		}

		req.Header.Add("Prefer", `IdType="ImmutableId"`)
		req.Header.Add("Prefer", "odata.maxpagesize=50")

		var page deltaResponse
		if err := c.doJSON(req, &page); err != nil {
			return nil, "", err
		}

		allMessages = append(allMessages, page.Value...)

		if page.NextLink != "" {
			endpoint = page.NextLink
			continue
		}

		finalDelta = page.DeltaLink
		endpoint = ""
	}

	if finalDelta == "" {
		return nil, "", fmt.Errorf("增量同步结果中缺少 deltaLink")
	}

	return allMessages, finalDelta, nil
}

// FirstMessageInFolder 返回指定文件夹中最新的一封邮件，便于调试直接保存 MIME。
func (c *Client) FirstMessageInFolder(ctx context.Context, folder string) (Message, bool, error) {
	query := url.Values{}
	query.Set("$top", "1")
	query.Set("$orderby", "receivedDateTime desc")
	query.Set("$select", "id,subject,receivedDateTime,internetMessageId,parentFolderId")

	endpoint := fmt.Sprintf(
		"%s/me/mailFolders/%s/messages?%s",
		strings.TrimRight(c.cfg.GraphBaseURL, "/"),
		url.PathEscape(folder),
		query.Encode(),
	)

	req, err := c.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Message{}, false, err
	}

	req.Header.Add("Prefer", `IdType="ImmutableId"`)

	var payload listMessagesResponse
	if err := c.doJSON(req, &payload); err != nil {
		return Message{}, false, err
	}

	if len(payload.Value) == 0 {
		return Message{}, false, nil
	}

	return payload.Value[0], true, nil
}

// FetchMIME 获取指定邮件的 MIME 字节流。
func (c *Client) FetchMIME(ctx context.Context, messageID string) (io.ReadCloser, error) {
	endpoint := fmt.Sprintf(
		"%s/me/messages/%s/$value",
		strings.TrimRight(c.cfg.GraphBaseURL, "/"),
		url.PathEscape(messageID),
	)

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

func (c *Client) newRequest(ctx context.Context, method, endpoint string, body io.Reader) (*http.Request, error) {
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

func (c *Client) doJSON(req *http.Request, dst any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("执行 Graph 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
		return fmt.Errorf("Graph 请求失败: status=%s body=%q", resp.Status, string(body))
	}

	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return fmt.Errorf("解析 Graph 响应失败: %w", err)
	}

	return nil
}
