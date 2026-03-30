package graph

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	azpolicy "github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	abstractions "github.com/microsoft/kiota-abstractions-go"
	serialization "github.com/microsoft/kiota-abstractions-go/serialization"
	msgraphauth "github.com/microsoftgraph/msgraph-sdk-go-core/authentication"
	msgraphsdk "github.com/microsoftgraph/msgraph-sdk-go"
	models "github.com/microsoftgraph/msgraph-sdk-go/models"
	odataerrors "github.com/microsoftgraph/msgraph-sdk-go/models/odataerrors"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/provider"
)

type accessTokenSource interface {
	AccessToken(ctx context.Context) (string, error)
}

type parsableFactory = serialization.ParsableFactory

type graphClient struct {
	adapter    abstractions.RequestAdapter
	client     *msgraphsdk.GraphServiceClient
	httpClient *http.Client
	baseURL    string
}

func newGraphClient(cfg appconfig.MailClientConfig, tokenSource accessTokenSource, httpClient *http.Client) (*graphClient, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if tokenSource == nil {
		return nil, fmt.Errorf("token source 不能为空")
	}

	validHosts, err := graphValidHosts(cfg.GraphBaseURL)
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}

	authProvider, err := msgraphauth.NewAzureIdentityAuthenticationProviderWithScopesAndValidHosts(
		&sessionTokenCredential{source: tokenSource},
		nil,
		validHosts,
	)
	if err != nil {
		return nil, fmt.Errorf("初始化 Graph 认证 provider 失败: %w", err)
	}

	adapter, err := msgraphsdk.NewGraphRequestAdapterWithParseNodeFactoryAndSerializationWriterFactoryAndHttpClient(
		authProvider,
		nil,
		nil,
		wrapGraphHTTPClient(httpClient),
	)
	if err != nil {
		return nil, fmt.Errorf("初始化 Graph request adapter 失败: %w", err)
	}

	baseURL := strings.TrimRight(cfg.GraphBaseURL, "/")
	adapter.SetBaseUrl(baseURL)

	return &graphClient{
		adapter:    adapter,
		client:     msgraphsdk.NewGraphServiceClient(adapter),
		httpClient: httpClient,
		baseURL:    baseURL,
	}, nil
}

func graphValidHosts(rawBaseURL string) ([]string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawBaseURL))
	if err != nil {
		return nil, fmt.Errorf("解析 Graph Base URL 失败: %w", err)
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return nil, fmt.Errorf("Graph Base URL 缺少 host")
	}
	return []string{host}, nil
}

func wrapGraphHTTPClient(httpClient *http.Client) *http.Client {
	return httpClient
}

type sessionTokenCredential struct {
	source accessTokenSource
}

func (c *sessionTokenCredential) GetToken(ctx context.Context, _ azpolicy.TokenRequestOptions) (azcore.AccessToken, error) {
	if c == nil || c.source == nil {
		return azcore.AccessToken{}, fmt.Errorf("token source 未初始化")
	}

	token, err := c.source.AccessToken(ctx)
	if err != nil {
		return azcore.AccessToken{}, err
	}

	return azcore.AccessToken{
		Token:     token,
		ExpiresOn: time.Now().Add(5 * time.Minute),
	}, nil
}

func (c *graphClient) newRequest(method abstractions.HttpMethod, endpoint string) (*abstractions.RequestInformation, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return nil, fmt.Errorf("构造 Graph 请求失败: %w", err)
	}

	requestInfo := abstractions.NewRequestInformation()
	requestInfo.Method = method
	requestInfo.SetUri(*parsed)
	return requestInfo, nil
}

func graphErrorMappings() abstractions.ErrorMappings {
	return abstractions.ErrorMappings{
		"4XX": odataerrors.CreateODataErrorFromDiscriminatorValue,
		"5XX": odataerrors.CreateODataErrorFromDiscriminatorValue,
	}
}

func (c *graphClient) doParsable(ctx context.Context, requestInfo *abstractions.RequestInformation, factory parsableFactory) (serialization.Parsable, error) {
	if c == nil || c.adapter == nil {
		return nil, fmt.Errorf("graph client 未初始化")
	}
	if requestInfo == nil {
		return nil, fmt.Errorf("Graph 请求不能为空")
	}
	if factory == nil {
		return nil, fmt.Errorf("Graph 解析器不能为空")
	}

	result, err := c.adapter.Send(ctx, requestInfo, factory, graphErrorMappings())
	if err != nil {
		return nil, fmt.Errorf("执行 Graph 请求失败: %w", err)
	}
	if result == nil {
		return nil, fmt.Errorf("Graph 响应为空")
	}
	return result, nil
}

func (c *graphClient) doBytes(ctx context.Context, requestInfo *abstractions.RequestInformation) ([]byte, error) {
	if c == nil || c.adapter == nil {
		return nil, fmt.Errorf("graph client 未初始化")
	}
	if requestInfo == nil {
		return nil, fmt.Errorf("Graph 请求不能为空")
	}

	result, err := c.adapter.SendPrimitive(ctx, requestInfo, "[]byte", graphErrorMappings())
	if err != nil {
		return nil, fmt.Errorf("执行 Graph 请求失败: %w", err)
	}
	if result == nil {
		return nil, fmt.Errorf("Graph 响应为空")
	}

	content, ok := result.([]byte)
	if !ok {
		return nil, fmt.Errorf("Graph 响应类型异常: %T", result)
	}
	return content, nil
}

func (c *graphClient) doNoContent(ctx context.Context, requestInfo *abstractions.RequestInformation) error {
	if c == nil || c.adapter == nil || c.httpClient == nil {
		return fmt.Errorf("graph client 未初始化")
	}
	if requestInfo == nil {
		return fmt.Errorf("Graph 请求不能为空")
	}

	native, err := c.adapter.ConvertToNativeRequest(ctx, requestInfo)
	if err != nil {
		return fmt.Errorf("执行 Graph 请求失败: %w", err)
	}
	req, ok := native.(*http.Request)
	if !ok {
		return fmt.Errorf("Graph 原生请求类型异常: %T", native)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("执行 Graph 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
		return fmt.Errorf("执行 Graph 请求失败: status=%s body=%q", resp.Status, string(body))
	}
	return nil
}

func (c *graphClient) fetchMIMEStream(ctx context.Context, messageID string) (io.ReadCloser, error) {
	content, err := c.fetchMIMEBytes(ctx, messageID)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(content)), nil
}

func (c *graphClient) fetchMIMEBytes(ctx context.Context, messageID string) ([]byte, error) {
	endpoint := fmt.Sprintf("%s/me/messages/%s/$value", c.baseURL, url.PathEscape(messageID))
	requestInfo, err := c.newRequest(abstractions.GET, endpoint)
	if err != nil {
		return nil, err
	}
	requestInfo.Headers.Add("Accept", "application/octet-stream")
	requestInfo.Headers.Add("Prefer", `IdType="ImmutableId"`)
	return c.doBytes(ctx, requestInfo)
}

func providerUserFromModel(user models.Userable) provider.User {
	if user == nil {
		return provider.User{}
	}
	return provider.User{
		ID:                stringValue(user.GetId()),
		DisplayName:       stringValue(user.GetDisplayName()),
		Mail:              stringValue(user.GetMail()),
		UserPrincipalName: stringValue(user.GetUserPrincipalName()),
	}
}

func providerMessageFromModel(message models.Messageable) provider.Message {
	if message == nil {
		return provider.Message{}
	}
	return provider.Message{
		ID:                stringValue(message.GetId()),
		Subject:           stringValue(message.GetSubject()),
		InternetMessageID: stringValue(message.GetInternetMessageId()),
		ParentFolderID:    stringValue(message.GetParentFolderId()),
		ReceivedDateTime:  timeValue(message.GetReceivedDateTime()),
	}
}

func providerMessagesFromModels(messages []models.Messageable) []provider.Message {
	if len(messages) == 0 {
		return nil
	}
	result := make([]provider.Message, 0, len(messages))
	for _, message := range messages {
		result = append(result, providerMessageFromModel(message))
	}
	return result
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func timeValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}
