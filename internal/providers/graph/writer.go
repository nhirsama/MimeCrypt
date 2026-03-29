package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/mimeutil"
	"mimecrypt/internal/provider"
)

type writer struct {
	*graphClient
}

var _ provider.Deleter = (*writer)(nil)

func newWriter(cfg appconfig.MailClientConfig, tokenSource accessTokenSource, httpClient *http.Client) (*writer, error) {
	client, err := newGraphClient(cfg, tokenSource, httpClient)
	if err != nil {
		return nil, err
	}

	return &writer{graphClient: client}, nil
}

func (w *writer) WriteMessage(ctx context.Context, req provider.WriteRequest) (provider.WriteResult, error) {
	if req.DeleteSource && strings.TrimSpace(req.Source.ID) == "" {
		return provider.WriteResult{}, fmt.Errorf("原邮件 ID 不能为空")
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

	createdDraft, err := w.createDraftMessage(ctx, req.OpenMIME)
	if err != nil {
		return provider.WriteResult{}, err
	}
	created, err := w.moveMessage(ctx, createdDraft.ID, targetFolderID)
	if err != nil {
		return provider.WriteResult{}, w.createdMessageRetainedError(createdDraft.ID, req.Source.ID, fmt.Errorf("移动回写邮件到目标文件夹 %s 失败: %w", targetFolderID, err))
	}
	if err := w.markUnread(ctx, created.ID); err != nil {
		return provider.WriteResult{}, w.createdMessageRetainedError(created.ID, req.Source.ID, fmt.Errorf("将回写邮件 %s 标记为未读失败: %w", created.ID, err))
	}

	if req.Verify {
		if err := w.verifyMessage(ctx, created.ID, targetFolderID); err != nil {
			return provider.WriteResult{}, w.createdMessageRetainedError(created.ID, req.Source.ID, fmt.Errorf("校验新消息 %s 失败: %w", created.ID, err))
		}
	}

	if req.DeleteSource {
		if err := w.deleteOriginalIfExists(ctx, req.Source.ID); err != nil {
			return provider.WriteResult{}, w.createdMessageRetainedError(created.ID, req.Source.ID, fmt.Errorf("删除原邮件 %s 失败: %w", req.Source.ID, err))
		}
	}

	return provider.WriteResult{Verified: req.Verify}, nil
}

func (w *writer) ReconcileMessage(ctx context.Context, req provider.WriteRequest) (provider.WriteResult, bool, error) {
	if req.DeleteSource && strings.TrimSpace(req.Source.ID) == "" {
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

func (w *writer) createDraftMessage(ctx context.Context, open provider.MIMEOpener) (provider.Message, error) {
	endpoint := fmt.Sprintf("%s/me/messages", w.baseURL)

	body, err := newBase64MIMEReader(open)
	if err != nil {
		return provider.Message{}, err
	}
	defer body.Close()

	req, err := w.newRequest(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return provider.Message{}, err
	}
	req.Header.Set("Content-Type", "text/plain")

	var message provider.Message
	if err := w.doJSON(req, &message, http.StatusCreated); err != nil {
		return provider.Message{}, fmt.Errorf("创建回写草稿失败: %w", err)
	}
	if strings.TrimSpace(message.ID) == "" {
		return provider.Message{}, fmt.Errorf("创建回写草稿失败: 响应中缺少消息 ID")
	}

	return message, nil
}

func (w *writer) moveMessage(ctx context.Context, messageID, targetFolderID string) (provider.Message, error) {
	endpoint := fmt.Sprintf("%s/me/messages/%s/move", w.baseURL, url.PathEscape(messageID))
	body, err := json.Marshal(struct {
		DestinationID string `json:"destinationId"`
	}{
		DestinationID: targetFolderID,
	})
	if err != nil {
		return provider.Message{}, fmt.Errorf("序列化移动邮件请求失败: %w", err)
	}

	req, err := w.newRequest(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return provider.Message{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	var message provider.Message
	if err := w.doJSON(req, &message, http.StatusCreated); err != nil {
		return provider.Message{}, fmt.Errorf("移动回写邮件失败: %w", err)
	}
	if strings.TrimSpace(message.ID) == "" {
		return provider.Message{}, fmt.Errorf("移动回写邮件失败: 响应中缺少消息 ID")
	}

	return message, nil
}

func (w *writer) markUnread(ctx context.Context, messageID string) error {
	endpoint := fmt.Sprintf("%s/me/messages/%s", w.baseURL, url.PathEscape(messageID))
	body, err := json.Marshal(struct {
		IsRead bool `json:"isRead"`
	}{
		IsRead: false,
	})
	if err != nil {
		return fmt.Errorf("序列化标记未读请求失败: %w", err)
	}

	req, err := w.newRequest(ctx, http.MethodPatch, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	var message provider.Message
	if err := w.doJSON(req, &message, http.StatusOK); err != nil {
		return fmt.Errorf("更新回写邮件未读状态失败: %w", err)
	}

	return nil
}

func (w *writer) verifyMessage(ctx context.Context, messageID, targetFolderID string) error {
	message, err := w.messageMetadata(ctx, messageID)
	if err != nil {
		return fmt.Errorf("校验回写邮件失败: %w", err)
	}
	if strings.TrimSpace(message.ID) == "" {
		return fmt.Errorf("校验回写邮件失败: 响应中缺少消息 ID")
	}
	if strings.TrimSpace(message.ParentFolderID) != strings.TrimSpace(targetFolderID) {
		return fmt.Errorf("校验回写邮件失败: 目标文件夹不匹配，got=%s want=%s", message.ParentFolderID, targetFolderID)
	}

	ok, err := w.messageHasProcessedEncryptedMIME(ctx, messageID)
	if err != nil {
		return fmt.Errorf("校验回写邮件失败: 读取 MIME 失败: %w", err)
	}
	if !ok {
		return fmt.Errorf("校验回写邮件失败: 缺少 MimeCrypt 处理标记")
	}

	return nil
}

func (w *writer) messageMetadata(ctx context.Context, messageID string) (provider.Message, error) {
	endpoint := fmt.Sprintf(
		"%s/me/messages/%s?$select=id,parentFolderId",
		w.baseURL,
		url.PathEscape(messageID),
	)

	req, err := w.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return provider.Message{}, err
	}

	var message provider.Message
	if err := w.doJSON(req, &message, http.StatusOK); err != nil {
		return provider.Message{}, err
	}

	return message, nil
}

func (w *writer) messageHasProcessedEncryptedMIME(ctx context.Context, messageID string) (bool, error) {
	stream, err := w.fetchMIMEStream(ctx, messageID)
	if err != nil {
		return false, err
	}
	defer stream.Close()

	return mimeutil.IsProcessedEncryptedStream(stream)
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

	if req.DeleteSource {
		if err := w.deleteOriginalIfExists(ctx, req.Source.ID); err != nil {
			return provider.WriteResult{}, false, fmt.Errorf("发现已有加密邮件 %s，但删除原邮件 %s 失败: %w", existing.ID, req.Source.ID, err)
		}
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

		ok, err := w.messageHasProcessedEncryptedMIME(ctx, message.ID)
		if err != nil {
			return provider.Message{}, false, fmt.Errorf("读取候选邮件 %s 的 MIME 失败: %w", message.ID, err)
		}
		if ok {
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

func (w *writer) DeleteMessage(ctx context.Context, source provider.MessageRef) error {
	if strings.TrimSpace(source.ID) == "" {
		return fmt.Errorf("原邮件 ID 不能为空")
	}
	return w.deleteOriginalIfExists(ctx, source.ID)
}

func isGraphNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "status=404")
}

func odataStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
