package graph

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	abstractions "github.com/microsoft/kiota-abstractions-go"
	models "github.com/microsoftgraph/msgraph-sdk-go/models"
	users "github.com/microsoftgraph/msgraph-sdk-go/users"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/provider"
)

type reader struct {
	*graphClient
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
	query := url.Values{}
	query.Set("$select", "id,displayName,mail,userPrincipalName")

	requestInfo, err := r.newRequest(abstractions.GET, r.baseURL+"/me?"+query.Encode())
	if err != nil {
		return provider.User{}, err
	}
	requestInfo.Headers.Add("Accept", "application/json")

	parsed, err := r.doParsable(ctx, requestInfo, models.CreateUserFromDiscriminatorValue)
	if err != nil {
		return provider.User{}, err
	}

	user, ok := parsed.(models.Userable)
	if !ok {
		return provider.User{}, fmt.Errorf("Graph 用户响应类型异常: %T", parsed)
	}
	return providerUserFromModel(user), nil
}

// Message 返回指定邮件的基础元数据。
func (r *reader) Message(ctx context.Context, messageID string) (provider.Message, error) {
	query := url.Values{}
	query.Set("$select", "id,subject,receivedDateTime,internetMessageId,parentFolderId")

	requestInfo, err := r.newRequest(
		abstractions.GET,
		fmt.Sprintf("%s/me/messages/%s?%s", r.baseURL, url.PathEscape(messageID), query.Encode()),
	)
	if err != nil {
		return provider.Message{}, err
	}
	requestInfo.Headers.Add("Accept", "application/json")
	requestInfo.Headers.Add("Prefer", `IdType="ImmutableId"`)

	parsed, err := r.doParsable(ctx, requestInfo, models.CreateMessageFromDiscriminatorValue)
	if err != nil {
		return provider.Message{}, err
	}

	message, ok := parsed.(models.Messageable)
	if !ok {
		return provider.Message{}, fmt.Errorf("Graph 邮件响应类型异常: %T", parsed)
	}
	return providerMessageFromModel(message), nil
}

// DeltaCreatedMessages 读取指定文件夹的增量消息列表。
func (r *reader) DeltaCreatedMessages(ctx context.Context, folder, deltaLink string) ([]provider.Message, string, error) {
	endpoint := deltaLink
	if endpoint == "" {
		query := url.Values{}
		query.Set("changeType", "created")
		query.Set("$select", "id,subject,receivedDateTime,internetMessageId,parentFolderId")
		endpoint = fmt.Sprintf("%s/me/mailFolders/%s/messages/delta?%s", r.baseURL, url.PathEscape(folder), query.Encode())
	}

	var (
		allMessages []provider.Message
		finalDelta  string
	)

	for endpoint != "" {
		requestInfo, err := r.newRequest(abstractions.GET, endpoint)
		if err != nil {
			return nil, "", err
		}
		requestInfo.Headers.Add("Accept", "application/json")
		requestInfo.Headers.Add("Prefer", `IdType="ImmutableId"`)
		requestInfo.Headers.Add("Prefer", "odata.maxpagesize=50")

		parsed, err := r.doParsable(ctx, requestInfo, users.CreateItemMailFoldersItemMessagesDeltaGetResponseFromDiscriminatorValue)
		if err != nil {
			return nil, "", err
		}

		page, ok := parsed.(users.ItemMailFoldersItemMessagesDeltaGetResponseable)
		if !ok {
			return nil, "", fmt.Errorf("Graph delta 响应类型异常: %T", parsed)
		}

		allMessages = append(allMessages, providerMessagesFromModels(page.GetValue())...)

		if next := stringValue(page.GetOdataNextLink()); next != "" {
			endpoint = next
			continue
		}

		finalDelta = stringValue(page.GetOdataDeltaLink())
		endpoint = ""
	}

	if finalDelta == "" {
		return nil, "", fmt.Errorf("增量同步结果中缺少 deltaLink")
	}
	return allMessages, finalDelta, nil
}

// FirstMessageInFolder 返回指定文件夹中最新的一封邮件，便于调试直接保存 MIME。
func (r *reader) FirstMessageInFolder(ctx context.Context, folder string) (provider.Message, bool, error) {
	messages, err := r.LatestMessagesInFolder(ctx, folder, 0, 1)
	if err != nil {
		return provider.Message{}, false, err
	}
	if len(messages) == 0 {
		return provider.Message{}, false, nil
	}
	return messages[0], true, nil
}

// LatestMessagesInFolder 返回指定文件夹中最新的一段消息，按接收时间倒序排列。
func (r *reader) LatestMessagesInFolder(ctx context.Context, folder string, skip, limit int) ([]provider.Message, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("limit 必须大于 0")
	}
	if skip < 0 {
		return nil, fmt.Errorf("skip 不能小于 0")
	}

	query := url.Values{}
	query.Set("$top", fmt.Sprintf("%d", limit))
	query.Set("$skip", fmt.Sprintf("%d", skip))
	query.Set("$orderby", "receivedDateTime desc")
	query.Set("$select", "id,subject,receivedDateTime,internetMessageId,parentFolderId")

	endpoint := fmt.Sprintf("%s/me/mailFolders/%s/messages?%s", r.baseURL, url.PathEscape(folder), query.Encode())
	requestInfo, err := r.newRequest(abstractions.GET, endpoint)
	if err != nil {
		return nil, err
	}
	requestInfo.Headers.Add("Accept", "application/json")
	requestInfo.Headers.Add("Prefer", `IdType="ImmutableId"`)

	parsed, err := r.doParsable(ctx, requestInfo, models.CreateMessageCollectionResponseFromDiscriminatorValue)
	if err != nil {
		return nil, err
	}

	collection, ok := parsed.(models.MessageCollectionResponseable)
	if !ok {
		return nil, fmt.Errorf("Graph 列表响应类型异常: %T", parsed)
	}
	return providerMessagesFromModels(collection.GetValue()), nil
}

// FetchMIME 获取指定邮件的 MIME 字节流。
func (r *reader) FetchMIME(ctx context.Context, messageID string) (io.ReadCloser, error) {
	return r.fetchMIMEStream(ctx, messageID)
}
