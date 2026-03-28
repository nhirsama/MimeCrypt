package graph

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/provider"
)

type reader struct {
	*graphClient
}

type deltaResponse struct {
	Value     []provider.Message `json:"value"`
	NextLink  string             `json:"@odata.nextLink"`
	DeltaLink string             `json:"@odata.deltaLink"`
}

type listMessagesResponse struct {
	Value []provider.Message `json:"value"`
}

var _ provider.Reader = (*reader)(nil)

func newReader(cfg appconfig.MailClientConfig, tokenSource accessTokenSource, httpClient *http.Client) (*reader, error) {
	client, err := newGraphClient(cfg, tokenSource, httpClient)
	if err != nil {
		return nil, err
	}

	return &reader{graphClient: client}, nil
}

// Me 返回当前登录用户的基础信息，便于验证登录状态。
func (r *reader) Me(ctx context.Context) (provider.User, error) {
	endpoint := fmt.Sprintf("%s/me?$select=id,displayName,mail,userPrincipalName", r.baseURL)

	req, err := r.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return provider.User{}, err
	}

	var user provider.User
	if err := r.doJSON(req, &user, http.StatusOK); err != nil {
		return provider.User{}, err
	}

	return user, nil
}

// Message 返回指定邮件的基础元数据。
func (r *reader) Message(ctx context.Context, messageID string) (provider.Message, error) {
	query := url.Values{}
	query.Set("$select", "id,subject,receivedDateTime,internetMessageId,parentFolderId")

	endpoint := fmt.Sprintf(
		"%s/me/messages/%s?%s",
		r.baseURL,
		url.PathEscape(messageID),
		query.Encode(),
	)

	req, err := r.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return provider.Message{}, err
	}

	req.Header.Add("Prefer", `IdType="ImmutableId"`)

	var message provider.Message
	if err := r.doJSON(req, &message, http.StatusOK); err != nil {
		return provider.Message{}, err
	}

	return message, nil
}

// DeltaCreatedMessages 读取指定文件夹的增量消息列表。
func (r *reader) DeltaCreatedMessages(ctx context.Context, folder, deltaLink string) ([]provider.Message, string, error) {
	endpoint := deltaLink
	if endpoint == "" {
		query := url.Values{}
		query.Set("changeType", "created")
		query.Set("$select", "id,subject,receivedDateTime,internetMessageId,parentFolderId")

		endpoint = fmt.Sprintf(
			"%s/me/mailFolders/%s/messages/delta?%s",
			r.baseURL,
			url.PathEscape(folder),
			query.Encode(),
		)
	}

	var (
		allMessages []provider.Message
		finalDelta  string
	)

	for endpoint != "" {
		req, err := r.newRequest(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, "", err
		}

		req.Header.Add("Prefer", `IdType="ImmutableId"`)
		req.Header.Add("Prefer", "odata.maxpagesize=50")

		var page deltaResponse
		if err := r.doJSON(req, &page, http.StatusOK); err != nil {
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
func (r *reader) FirstMessageInFolder(ctx context.Context, folder string) (provider.Message, bool, error) {
	payload, err := r.latestMessagesInFolder(ctx, folder, 0, 1)
	if err != nil {
		return provider.Message{}, false, err
	}
	if len(payload.Value) == 0 {
		return provider.Message{}, false, nil
	}

	return payload.Value[0], true, nil
}

// LatestMessagesInFolder 返回指定文件夹中最新的一段消息，按接收时间倒序排列。
func (r *reader) LatestMessagesInFolder(ctx context.Context, folder string, skip, limit int) ([]provider.Message, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("limit 必须大于 0")
	}
	if skip < 0 {
		return nil, fmt.Errorf("skip 不能小于 0")
	}

	payload, err := r.latestMessagesInFolder(ctx, folder, skip, limit)
	if err != nil {
		return nil, err
	}
	return payload.Value, nil
}

func (r *reader) latestMessagesInFolder(ctx context.Context, folder string, skip, limit int) (listMessagesResponse, error) {
	query := url.Values{}
	query.Set("$top", fmt.Sprintf("%d", limit))
	query.Set("$skip", fmt.Sprintf("%d", skip))
	query.Set("$orderby", "receivedDateTime desc")
	query.Set("$select", "id,subject,receivedDateTime,internetMessageId,parentFolderId")

	endpoint := fmt.Sprintf(
		"%s/me/mailFolders/%s/messages?%s",
		r.baseURL,
		url.PathEscape(folder),
		query.Encode(),
	)

	req, err := r.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return listMessagesResponse{}, err
	}

	req.Header.Add("Prefer", `IdType="ImmutableId"`)

	var payload listMessagesResponse
	if err := r.doJSON(req, &payload, http.StatusOK); err != nil {
		return listMessagesResponse{}, err
	}

	return payload, nil
}

// FetchMIME 获取指定邮件的 MIME 字节流。
func (r *reader) FetchMIME(ctx context.Context, messageID string) (io.ReadCloser, error) {
	return r.fetchMIMEStream(ctx, messageID)
}
