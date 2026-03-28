package graph

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/provider"
)

type writer struct {
	*graphClient
}

func newWriter(cfg appconfig.MailConfig, tokenSource accessTokenSource, httpClient *http.Client) (*writer, error) {
	client, err := newGraphClient(cfg, tokenSource, httpClient)
	if err != nil {
		return nil, err
	}

	return &writer{graphClient: client}, nil
}

func (w *writer) WriteMessage(ctx context.Context, req provider.WriteRequest) (provider.WriteResult, error) {
	if strings.TrimSpace(req.Source.ID) == "" {
		return provider.WriteResult{}, fmt.Errorf("原邮件 ID 不能为空")
	}
	if len(req.MIME) == 0 {
		return provider.WriteResult{}, fmt.Errorf("回写 MIME 不能为空")
	}

	targetFolderID, err := w.targetFolderID(ctx, req)
	if err != nil {
		return provider.WriteResult{}, err
	}

	if result, found, err := w.reconcileInTarget(ctx, req, targetFolderID); err != nil {
		return provider.WriteResult{}, err
	} else if found {
		return result, nil
	}

	created, err := w.createMessage(ctx, targetFolderID, req.MIME)
	if err != nil {
		return provider.WriteResult{}, err
	}

	if req.Verify {
		if err := w.verifyMessage(ctx, created.ID, targetFolderID); err != nil {
			return provider.WriteResult{}, w.createdMessageRetainedError(created.ID, req.Source.ID, fmt.Errorf("校验新消息 %s 失败: %w", created.ID, err))
		}
	}

	if err := w.deleteOriginalIfExists(ctx, req.Source.ID); err != nil {
		return provider.WriteResult{}, w.createdMessageRetainedError(created.ID, req.Source.ID, fmt.Errorf("删除原邮件 %s 失败: %w", req.Source.ID, err))
	}

	return provider.WriteResult{Verified: req.Verify}, nil
}

func (w *writer) ReconcileMessage(ctx context.Context, req provider.WriteRequest) (provider.WriteResult, bool, error) {
	if strings.TrimSpace(req.Source.ID) == "" {
		return provider.WriteResult{}, false, fmt.Errorf("原邮件 ID 不能为空")
	}

	targetFolderID, err := w.targetFolderID(ctx, req)
	if err != nil {
		return provider.WriteResult{}, false, err
	}

	return w.reconcileInTarget(ctx, req, targetFolderID)
}

func (w *writer) targetFolderID(ctx context.Context, req provider.WriteRequest) (string, error) {
	if destination := strings.TrimSpace(req.DestinationFolderID); destination != "" {
		return w.resolveFolderID(ctx, destination)
	}

	source := strings.TrimSpace(req.Source.FolderID)
	if source == "" {
		return "", fmt.Errorf("缺少原邮件所在文件夹，无法决定回写位置")
	}

	return source, nil
}

func (w *writer) resolveFolderID(ctx context.Context, folder string) (string, error) {
	endpoint := fmt.Sprintf("%s/me/mailFolders/%s?$select=id", w.baseURL, url.PathEscape(folder))

	req, err := w.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}

	var payload struct {
		ID string `json:"id"`
	}
	if err := w.doJSON(req, &payload, http.StatusOK); err != nil {
		return "", fmt.Errorf("解析回写目标文件夹失败: %w", err)
	}
	if strings.TrimSpace(payload.ID) == "" {
		return "", fmt.Errorf("回写目标文件夹不存在: %s", folder)
	}

	return payload.ID, nil
}

func (w *writer) createMessage(ctx context.Context, folderID string, mimeBytes []byte) (provider.Message, error) {
	endpoint := fmt.Sprintf("%s/me/mailFolders/%s/messages", w.baseURL, url.PathEscape(folderID))

	req, err := w.newRequest(ctx, http.MethodPost, endpoint, bytes.NewReader([]byte(base64.StdEncoding.EncodeToString(mimeBytes))))
	if err != nil {
		return provider.Message{}, err
	}
	req.Header.Set("Content-Type", "text/plain")

	var message provider.Message
	if err := w.doJSON(req, &message, http.StatusCreated); err != nil {
		return provider.Message{}, fmt.Errorf("创建回写邮件失败: %w", err)
	}
	if strings.TrimSpace(message.ID) == "" {
		return provider.Message{}, fmt.Errorf("创建回写邮件失败: 响应中缺少消息 ID")
	}

	return message, nil
}

func (w *writer) verifyMessage(ctx context.Context, messageID, targetFolderID string) error {
	endpoint := fmt.Sprintf(
		"%s/me/messages/%s?$select=id,parentFolderId",
		w.baseURL,
		url.PathEscape(messageID),
	)

	req, err := w.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}

	var message provider.Message
	if err := w.doJSON(req, &message, http.StatusOK); err != nil {
		return fmt.Errorf("校验回写邮件失败: %w", err)
	}
	if strings.TrimSpace(message.ID) == "" {
		return fmt.Errorf("校验回写邮件失败: 响应中缺少消息 ID")
	}
	if strings.TrimSpace(message.ParentFolderID) != strings.TrimSpace(targetFolderID) {
		return fmt.Errorf("校验回写邮件失败: 目标文件夹不匹配，got=%s want=%s", message.ParentFolderID, targetFolderID)
	}

	return nil
}

func (w *writer) deleteMessage(ctx context.Context, messageID string) error {
	endpoint := fmt.Sprintf("%s/me/messages/%s", w.baseURL, url.PathEscape(messageID))

	req, err := w.newRequest(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}

	return w.doEmpty(req, http.StatusNoContent)
}

func (w *writer) reconcileInTarget(ctx context.Context, req provider.WriteRequest, targetFolderID string) (provider.WriteResult, bool, error) {
	existing, found, err := w.findProcessedMessage(ctx, targetFolderID, req.Source.InternetMessageID, req.Source.ID)
	if err != nil {
		return provider.WriteResult{}, false, err
	}
	if !found {
		return provider.WriteResult{}, false, nil
	}

	if req.Verify {
		if err := w.verifyMessage(ctx, existing.ID, targetFolderID); err != nil {
			return provider.WriteResult{}, false, fmt.Errorf("发现已有加密邮件 %s，但校验失败: %w", existing.ID, err)
		}
	}

	if err := w.deleteOriginalIfExists(ctx, req.Source.ID); err != nil {
		return provider.WriteResult{}, false, fmt.Errorf("发现已有加密邮件 %s，但删除原邮件 %s 失败: %w", existing.ID, req.Source.ID, err)
	}

	return provider.WriteResult{Verified: req.Verify}, true, nil
}

func (w *writer) findProcessedMessage(ctx context.Context, folderID, internetMessageID, originalMessageID string) (provider.Message, bool, error) {
	internetMessageID = strings.TrimSpace(internetMessageID)
	if internetMessageID == "" {
		return provider.Message{}, false, nil
	}

	query := url.Values{}
	query.Set("$select", "id,parentFolderId,internetMessageId")
	query.Set("$filter", "internetMessageId eq "+odataStringLiteral(internetMessageID))
	query.Set("$top", "10")

	endpoint := fmt.Sprintf(
		"%s/me/mailFolders/%s/messages?%s",
		w.baseURL,
		url.PathEscape(folderID),
		query.Encode(),
	)

	req, err := w.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return provider.Message{}, false, err
	}
	req.Header.Add("Prefer", `IdType="ImmutableId"`)

	var payload struct {
		Value []provider.Message `json:"value"`
	}
	if err := w.doJSON(req, &payload, http.StatusOK); err != nil {
		return provider.Message{}, false, fmt.Errorf("查询目标文件夹中的已处理邮件失败: %w", err)
	}

	for _, message := range payload.Value {
		if strings.TrimSpace(message.ID) == "" || message.ID == originalMessageID {
			continue
		}

		mimeBytes, err := w.fetchMIMEBytes(ctx, message.ID)
		if err != nil {
			return provider.Message{}, false, fmt.Errorf("读取候选邮件 %s 的 MIME 失败: %w", message.ID, err)
		}
		if isProcessedEncryptedMIME(mimeBytes) {
			return message, true, nil
		}
	}

	return provider.Message{}, false, nil
}

func (w *writer) deleteOriginalIfExists(ctx context.Context, messageID string) error {
	if err := w.deleteMessage(ctx, messageID); err != nil {
		if isGraphNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

func (w *writer) createdMessageRetainedError(createdMessageID, originalMessageID string, cause error) error {
	return fmt.Errorf("%w；已保留新加密邮件 %s 和原邮件 %s，后续重试会继续收敛", cause, createdMessageID, originalMessageID)
}

func isGraphNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "status=404")
}

func odataStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func isProcessedEncryptedMIME(mimeBytes []byte) bool {
	header := strings.ToLower(string(extractHeaderBlock(mimeBytes)))
	return strings.Contains(header, "x-mimecrypt-processed: yes") &&
		strings.Contains(header, "content-type: multipart/encrypted") &&
		strings.Contains(header, "application/pgp-encrypted")
}

func extractHeaderBlock(mimeBytes []byte) []byte {
	if idx := bytes.Index(mimeBytes, []byte("\r\n\r\n")); idx >= 0 {
		return mimeBytes[:idx]
	}
	if idx := bytes.Index(mimeBytes, []byte("\n\n")); idx >= 0 {
		return mimeBytes[:idx]
	}
	return mimeBytes
}
