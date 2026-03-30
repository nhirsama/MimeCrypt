package graph

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"

	abstractions "github.com/microsoft/kiota-abstractions-go"
	models "github.com/microsoftgraph/msgraph-sdk-go/models"

	"mimecrypt/internal/provider"
)

var _ provider.HealthProber = (*writer)(nil)
var _ provider.HealthProber = (*ewsWriter)(nil)

func (w *writer) HealthCheck(ctx context.Context) (string, error) {
	if w == nil || w.graphClient == nil {
		return "", fmt.Errorf("graph writer 未初始化")
	}
	user, err := w.currentUser(ctx)
	if err != nil {
		return "", err
	}
	account := strings.TrimSpace(user.Account())
	if account == "" {
		account = "graph"
	}
	return account, nil
}

func (w *ewsWriter) HealthCheck(ctx context.Context) (string, error) {
	if w == nil || w.ewsClient == nil {
		return "", fmt.Errorf("ews writer 未初始化")
	}
	return w.ewsClient.probe(ctx)
}

func (w *writer) currentUser(ctx context.Context) (provider.User, error) {
	endpoint := fmt.Sprintf("%s/me?$select=id,mail,userPrincipalName", w.baseURL)

	requestInfo, err := w.newRequest(abstractions.GET, endpoint)
	if err != nil {
		return provider.User{}, err
	}
	requestInfo.Headers.Add("Accept", "application/json")

	parsed, err := w.doParsable(ctx, requestInfo, models.CreateUserFromDiscriminatorValue)
	if err != nil {
		return provider.User{}, fmt.Errorf("Graph 健康探测失败: %w", err)
	}
	user, ok := parsed.(models.Userable)
	if !ok {
		return provider.User{}, fmt.Errorf("Graph 健康探测响应类型异常: %T", parsed)
	}
	return providerUserFromModel(user), nil
}

type ewsGetFolderEnvelope struct {
	XMLName   xml.Name                `xml:"soap:Envelope"`
	XMLNSXSI  string                  `xml:"xmlns:xsi,attr"`
	XMLNSM    string                  `xml:"xmlns:m,attr"`
	XMLNST    string                  `xml:"xmlns:t,attr"`
	XMLNSSoap string                  `xml:"xmlns:soap,attr"`
	Header    ewsCreateItemHeader     `xml:"soap:Header"`
	Body      ewsGetFolderRequestBody `xml:"soap:Body"`
}

type ewsGetFolderRequestBody struct {
	GetFolder ewsGetFolderRequest `xml:"m:GetFolder"`
}

type ewsGetFolderRequest struct {
	FolderShape ewsFolderShape `xml:"m:FolderShape"`
	FolderIDs   ewsFolderIDs   `xml:"m:FolderIds"`
}

type ewsFolderShape struct {
	BaseShape string `xml:"t:BaseShape"`
}

type ewsFolderIDs struct {
	DistinguishedFolderID ewsDistinguishedFolderID `xml:"t:DistinguishedFolderId"`
}

type ewsDistinguishedFolderID struct {
	ID string `xml:"Id,attr"`
}

type ewsGetFolderResponseEnvelope struct {
	Body ewsGetFolderResponseBody `xml:"Body"`
}

type ewsGetFolderResponseBody struct {
	Fault             *ewsFault             `xml:"Fault"`
	GetFolderResponse *ewsGetFolderResponse `xml:"GetFolderResponse"`
}

type ewsGetFolderResponse struct {
	ResponseMessages ewsGetFolderResponseMessages `xml:"ResponseMessages"`
}

type ewsGetFolderResponseMessages struct {
	Messages []ewsGetFolderResponseMessage `xml:"GetFolderResponseMessage"`
}

type ewsGetFolderResponseMessage struct {
	ResponseClass string `xml:"ResponseClass,attr"`
	ResponseCode  string `xml:"ResponseCode"`
	MessageText   string `xml:"MessageText"`
}

func (c *ewsClient) probe(ctx context.Context) (string, error) {
	reqBody, err := buildGetFolderProbeEnvelope()
	if err != nil {
		return "", err
	}

	req, err := c.newRequest(ctx, bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("执行 EWS 健康探测失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("读取 EWS 健康探测响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("EWS 健康探测失败: status=%s body=%q", resp.Status, string(body))
	}

	result, err := parseGetFolderProbeResponse(body)
	if err != nil {
		return "", err
	}
	return result, nil
}

func buildGetFolderProbeEnvelope() ([]byte, error) {
	payload := ewsGetFolderEnvelope{
		XMLNSXSI:  "http://www.w3.org/2001/XMLSchema-instance",
		XMLNSM:    "http://schemas.microsoft.com/exchange/services/2006/messages",
		XMLNST:    "http://schemas.microsoft.com/exchange/services/2006/types",
		XMLNSSoap: "http://schemas.xmlsoap.org/soap/envelope/",
		Header: ewsCreateItemHeader{
			RequestServerVersion: ewsRequestServerVersion{Version: "Exchange2016"},
		},
		Body: ewsGetFolderRequestBody{
			GetFolder: ewsGetFolderRequest{
				FolderShape: ewsFolderShape{BaseShape: "IdOnly"},
				FolderIDs: ewsFolderIDs{
					DistinguishedFolderID: ewsDistinguishedFolderID{ID: "inbox"},
				},
			},
		},
	}

	body, err := xml.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("序列化 EWS 健康探测请求失败: %w", err)
	}
	return append([]byte(xml.Header), body...), nil
}

func parseGetFolderProbeResponse(body []byte) (string, error) {
	var envelope ewsGetFolderResponseEnvelope
	if err := xml.Unmarshal(body, &envelope); err != nil {
		return "", fmt.Errorf("解析 EWS 健康探测响应失败: %w", err)
	}
	if envelope.Body.Fault != nil {
		return "", fmt.Errorf("EWS 健康探测失败: %s: %s", strings.TrimSpace(envelope.Body.Fault.FaultCode), strings.TrimSpace(envelope.Body.Fault.FaultString))
	}
	if envelope.Body.GetFolderResponse == nil {
		return "", fmt.Errorf("EWS 健康探测失败: 响应中缺少 GetFolderResponse")
	}
	if len(envelope.Body.GetFolderResponse.ResponseMessages.Messages) == 0 {
		return "", fmt.Errorf("EWS 健康探测失败: 响应中缺少 GetFolderResponseMessage")
	}
	message := envelope.Body.GetFolderResponse.ResponseMessages.Messages[0]
	if !strings.EqualFold(strings.TrimSpace(message.ResponseClass), "Success") || !strings.EqualFold(strings.TrimSpace(message.ResponseCode), "NoError") {
		text := strings.TrimSpace(message.MessageText)
		if text == "" {
			text = strings.TrimSpace(message.ResponseCode)
		}
		return "", fmt.Errorf("EWS 健康探测失败: %s", text)
	}
	return "ews inbox", nil
}
