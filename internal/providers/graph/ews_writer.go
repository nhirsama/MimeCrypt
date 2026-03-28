package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/provider"
)

type scopedAccessTokenSource interface {
	accessTokenSource
	AccessTokenForScopes(ctx context.Context, scopes []string) (string, error)
}

type ewsWriter struct {
	graphHelper *writer
	ewsClient   *ewsClient
}

type ewsClient struct {
	httpClient  *http.Client
	tokenSource scopedAccessTokenSource
	baseURL     string
	scopes      []string
}

type ewsEnvelope struct {
	Body ewsBody `xml:"Body"`
}

type ewsBody struct {
	Fault              *ewsFault              `xml:"Fault"`
	CreateItemResponse *ewsCreateItemResponse `xml:"CreateItemResponse"`
}

type ewsFault struct {
	FaultCode   string `xml:"faultcode"`
	FaultString string `xml:"faultstring"`
}

type ewsCreateItemResponse struct {
	ResponseMessages ewsResponseMessages `xml:"ResponseMessages"`
}

type ewsResponseMessages struct {
	Messages []ewsCreateItemResponseMessage `xml:"CreateItemResponseMessage"`
}

type ewsCreateItemResponseMessage struct {
	ResponseClass string   `xml:"ResponseClass,attr"`
	ResponseCode  string   `xml:"ResponseCode"`
	MessageText   string   `xml:"MessageText"`
	Items         ewsItems `xml:"Items"`
}

type ewsItems struct {
	Message *ewsMessage `xml:"Message"`
}

type ewsMessage struct {
	ItemID ewsItemID `xml:"ItemId"`
}

type ewsItemID struct {
	ID string `xml:"Id,attr"`
}

type ewsCreateItemEnvelope struct {
	XMLName   xml.Name                 `xml:"soap:Envelope"`
	XMLNSXSI  string                   `xml:"xmlns:xsi,attr"`
	XMLNSM    string                   `xml:"xmlns:m,attr"`
	XMLNST    string                   `xml:"xmlns:t,attr"`
	XMLNSSoap string                   `xml:"xmlns:soap,attr"`
	Header    ewsCreateItemHeader      `xml:"soap:Header"`
	Body      ewsCreateItemRequestBody `xml:"soap:Body"`
}

type ewsCreateItemHeader struct {
	RequestServerVersion ewsRequestServerVersion `xml:"t:RequestServerVersion"`
}

type ewsRequestServerVersion struct {
	Version string `xml:"Version,attr"`
}

type ewsCreateItemRequestBody struct {
	CreateItem ewsCreateItemRequest `xml:"m:CreateItem"`
}

type ewsCreateItemRequest struct {
	MessageDisposition string                 `xml:"MessageDisposition,attr"`
	SavedItemFolderID  ewsSavedItemFolderID   `xml:"m:SavedItemFolderId"`
	Items              ewsCreateItemRequestIO `xml:"m:Items"`
}

type ewsSavedItemFolderID struct {
	FolderID ewsFolderID `xml:"t:FolderId"`
}

type ewsFolderID struct {
	ID string `xml:"Id,attr"`
}

type ewsCreateItemRequestIO struct {
	Message ewsCreateMessage `xml:"t:Message"`
}

type ewsCreateMessage struct {
	MimeContent      ewsMimeContent      `xml:"t:MimeContent"`
	IsRead           bool                `xml:"t:IsRead"`
	ExtendedProperty ewsExtendedProperty `xml:"t:ExtendedProperty"`
}

type ewsMimeContent struct {
	CharacterSet string `xml:"CharacterSet,attr"`
	Value        string `xml:",chardata"`
}

type ewsExtendedProperty struct {
	ExtendedFieldURI ewsExtendedFieldURI `xml:"t:ExtendedFieldURI"`
	Value            string              `xml:"t:Value"`
}

type ewsExtendedFieldURI struct {
	PropertyTag  string `xml:"PropertyTag,attr"`
	PropertyType string `xml:"PropertyType,attr"`
}

func newEWSWriter(cfg appconfig.Config, tokenSource scopedAccessTokenSource, httpClient *http.Client) (*ewsWriter, error) {
	if tokenSource == nil {
		return nil, fmt.Errorf("token source 不能为空")
	}
	if len(cfg.Auth.EWSScopes) == 0 {
		return nil, fmt.Errorf("ews scopes 不能为空")
	}
	if err := cfg.Mail.Client.ValidateEWS(); err != nil {
		return nil, err
	}

	graphHelper, err := newWriter(cfg.Mail.Client, tokenSource, httpClient)
	if err != nil {
		return nil, err
	}

	ewsClient, err := newEWSClient(cfg.Mail.Client, cfg.Auth.EWSScopes, tokenSource, httpClient)
	if err != nil {
		return nil, err
	}

	return &ewsWriter{
		graphHelper: graphHelper,
		ewsClient:   ewsClient,
	}, nil
}

func newEWSClient(cfg appconfig.MailClientConfig, scopes []string, tokenSource scopedAccessTokenSource, httpClient *http.Client) (*ewsClient, error) {
	if err := cfg.ValidateEWS(); err != nil {
		return nil, err
	}
	if tokenSource == nil {
		return nil, fmt.Errorf("token source 不能为空")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}

	return &ewsClient{
		httpClient:  httpClient,
		tokenSource: tokenSource,
		baseURL:     strings.TrimRight(cfg.EWSBaseURL, "/"),
		scopes:      append([]string(nil), scopes...),
	}, nil
}

func (w *ewsWriter) WriteMessage(ctx context.Context, req provider.WriteRequest) (provider.WriteResult, error) {
	if strings.TrimSpace(req.Source.ID) == "" {
		return provider.WriteResult{}, fmt.Errorf("原邮件 ID 不能为空")
	}

	targetFolderID, err := w.graphHelper.targetFolderID(ctx, req)
	if err != nil {
		return provider.WriteResult{}, err
	}

	if result, found, err := w.graphHelper.reconcileInTarget(ctx, req, targetFolderID); err != nil {
		return provider.WriteResult{}, err
	} else if found {
		return result, nil
	}

	targetFolderEWSID, err := w.graphHelper.translateExchangeID(ctx, targetFolderID, "restId", "ewsId")
	if err != nil {
		return provider.WriteResult{}, fmt.Errorf("转换目标文件夹 ID 失败: %w", err)
	}

	createdEWSID, err := w.ewsClient.createMessage(ctx, targetFolderEWSID, req.OpenMIME)
	if err != nil {
		return provider.WriteResult{}, err
	}

	createdGraphID, err := w.resolveCreatedGraphID(ctx, req, targetFolderID, createdEWSID)
	if err != nil {
		createdIDForHint := createdGraphID
		if strings.TrimSpace(createdIDForHint) == "" {
			createdIDForHint = createdEWSID
		}
		return provider.WriteResult{}, w.graphHelper.createdMessageRetainedError(createdIDForHint, req.Source.ID, err)
	}

	if err := w.graphHelper.markUnread(ctx, createdGraphID); err != nil {
		return provider.WriteResult{}, w.graphHelper.createdMessageRetainedError(createdGraphID, req.Source.ID, fmt.Errorf("将回写邮件 %s 标记为未读失败: %w", createdGraphID, err))
	}

	if req.Verify {
		if err := w.graphHelper.verifyMessage(ctx, createdGraphID, targetFolderID); err != nil {
			return provider.WriteResult{}, w.graphHelper.createdMessageRetainedError(createdGraphID, req.Source.ID, fmt.Errorf("校验新消息 %s 失败: %w", createdGraphID, err))
		}
	}

	if err := w.graphHelper.deleteOriginalIfExists(ctx, req.Source.ID); err != nil {
		return provider.WriteResult{}, w.graphHelper.createdMessageRetainedError(createdGraphID, req.Source.ID, fmt.Errorf("删除原邮件 %s 失败: %w", req.Source.ID, err))
	}

	return provider.WriteResult{Verified: req.Verify}, nil
}

func (w *ewsWriter) ReconcileMessage(ctx context.Context, req provider.WriteRequest) (provider.WriteResult, bool, error) {
	targetFolderID, err := w.graphHelper.targetFolderID(ctx, req)
	if err != nil {
		return provider.WriteResult{}, false, err
	}

	return w.graphHelper.reconcileInTarget(ctx, req, targetFolderID)
}

func (w *ewsWriter) resolveCreatedGraphID(ctx context.Context, req provider.WriteRequest, targetFolderID, createdEWSID string) (string, error) {
	if strings.TrimSpace(req.Source.InternetMessageID) != "" {
		message, found, err := w.graphHelper.findProcessedMessage(ctx, targetFolderID, req.Source.InternetMessageID, req.Source.ID)
		if err != nil {
			return "", fmt.Errorf("查找新建回写邮件失败: %w", err)
		}
		if found && strings.TrimSpace(message.ID) != "" {
			return message.ID, nil
		}
	}

	if strings.TrimSpace(createdEWSID) == "" {
		return "", fmt.Errorf("回写完成但无法定位新消息")
	}

	createdGraphID, err := w.graphHelper.translateExchangeID(ctx, createdEWSID, "ewsId", "restId")
	if err != nil {
		return "", fmt.Errorf("转换回写邮件 ID 失败: %w", err)
	}

	return createdGraphID, nil
}

func (c *ewsClient) createMessage(ctx context.Context, folderID string, open provider.MIMEOpener) (string, error) {
	envelope, err := buildCreateItemEnvelopeReader(folderID, open)
	if err != nil {
		return "", err
	}
	defer envelope.Close()

	req, err := c.newRequest(ctx, envelope)
	if err != nil {
		return "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("执行 EWS 请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("读取 EWS 响应失败: %w", err)
	}

	itemID, parseErr := parseCreateItemResponse(body)
	if parseErr != nil {
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("创建回写邮件失败: status=%s body=%q", resp.Status, string(body))
		}
		return "", parseErr
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("创建回写邮件失败: status=%s", resp.Status)
	}

	return itemID, nil
}

func (c *ewsClient) newRequest(ctx context.Context, body io.Reader) (*http.Request, error) {
	token, err := c.tokenSource.AccessTokenForScopes(ctx, c.scopes)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, body)
	if err != nil {
		return nil, fmt.Errorf("构造 EWS 请求失败: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("Accept", "text/xml")

	return req, nil
}

func (w *writer) translateExchangeID(ctx context.Context, id, sourceIDType, targetIDType string) (string, error) {
	endpoint := fmt.Sprintf("%s/me/translateExchangeIds", w.baseURL)
	body, err := json.Marshal(struct {
		InputIDs     []string `json:"inputIds"`
		SourceIDType string   `json:"sourceIdType"`
		TargetIDType string   `json:"targetIdType"`
	}{
		InputIDs:     []string{id},
		SourceIDType: sourceIDType,
		TargetIDType: targetIDType,
	})
	if err != nil {
		return "", fmt.Errorf("序列化 ID 转换请求失败: %w", err)
	}

	req, err := w.newRequest(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	var payload struct {
		Value []struct {
			SourceID     string `json:"sourceId"`
			TargetID     string `json:"targetId"`
			ErrorDetails struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"errorDetails"`
		} `json:"value"`
	}
	if err := w.doJSON(req, &payload, http.StatusOK); err != nil {
		return "", fmt.Errorf("调用 translateExchangeIds 失败: %w", err)
	}
	if len(payload.Value) == 0 {
		return "", fmt.Errorf("translateExchangeIds 响应为空")
	}
	result := payload.Value[0]
	if strings.TrimSpace(result.TargetID) == "" {
		if strings.TrimSpace(result.ErrorDetails.Code) != "" || strings.TrimSpace(result.ErrorDetails.Message) != "" {
			return "", fmt.Errorf("%s: %s", result.ErrorDetails.Code, result.ErrorDetails.Message)
		}
		return "", fmt.Errorf("未返回 targetId")
	}

	return result.TargetID, nil
}

func parseCreateItemResponse(body []byte) (string, error) {
	var envelope ewsEnvelope
	if err := xml.Unmarshal(body, &envelope); err != nil {
		return "", fmt.Errorf("解析 EWS 响应失败: %w", err)
	}
	if envelope.Body.Fault != nil {
		fault := strings.TrimSpace(envelope.Body.Fault.FaultString)
		if fault == "" {
			fault = strings.TrimSpace(envelope.Body.Fault.FaultCode)
		}
		if fault == "" {
			fault = "EWS 返回 SOAP Fault"
		}
		return "", fmt.Errorf("创建回写邮件失败: %s", fault)
	}
	if envelope.Body.CreateItemResponse == nil {
		return "", fmt.Errorf("创建回写邮件失败: 响应中缺少 CreateItemResponse")
	}
	if len(envelope.Body.CreateItemResponse.ResponseMessages.Messages) == 0 {
		return "", fmt.Errorf("创建回写邮件失败: 响应中缺少 ResponseMessage")
	}

	message := envelope.Body.CreateItemResponse.ResponseMessages.Messages[0]
	if !strings.EqualFold(strings.TrimSpace(message.ResponseClass), "Success") {
		text := strings.TrimSpace(message.MessageText)
		if text == "" {
			text = strings.TrimSpace(message.ResponseCode)
		}
		if text == "" {
			text = "EWS 返回失败"
		}
		return "", fmt.Errorf("创建回写邮件失败: %s", text)
	}
	if message.Items.Message == nil || strings.TrimSpace(message.Items.Message.ItemID.ID) == "" {
		return "", nil
	}

	return strings.TrimSpace(message.Items.Message.ItemID.ID), nil
}
