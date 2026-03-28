package graph

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"

	"mimecrypt/internal/provider"
)

const ewsBase64Placeholder = "__MIMECRYPT_BASE64__"

func newBase64MIMEReader(open provider.MIMEOpener) (io.ReadCloser, error) {
	if open == nil {
		return nil, fmt.Errorf("MIME 输入源不能为空")
	}

	src, err := open()
	if err != nil {
		return nil, fmt.Errorf("打开 MIME 输入源失败: %w", err)
	}

	reader, writer := io.Pipe()
	go func() {
		encoder := base64.NewEncoder(base64.StdEncoding, writer)
		_, copyErr := io.Copy(encoder, src)
		closeErr := encoder.Close()
		srcCloseErr := src.Close()
		switch {
		case copyErr != nil:
			_ = writer.CloseWithError(copyErr)
		case closeErr != nil:
			_ = writer.CloseWithError(closeErr)
		case srcCloseErr != nil:
			_ = writer.CloseWithError(srcCloseErr)
		default:
			_ = writer.Close()
		}
	}()

	return reader, nil
}

func buildCreateItemEnvelopeReader(folderID string, open provider.MIMEOpener) (io.ReadCloser, error) {
	envelope, err := buildCreateItemEnvelope(folderID, ewsBase64Placeholder)
	if err != nil {
		return nil, err
	}

	parts := bytes.SplitN(envelope, []byte(ewsBase64Placeholder), 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("序列化 EWS CreateItem 请求失败: 未找到 MIME 占位符")
	}

	src, err := open()
	if err != nil {
		return nil, fmt.Errorf("打开 MIME 输入源失败: %w", err)
	}

	reader, writer := io.Pipe()
	go func() {
		defer src.Close()

		if _, err := writer.Write(parts[0]); err != nil {
			_ = writer.CloseWithError(err)
			return
		}

		encoder := base64.NewEncoder(base64.StdEncoding, writer)
		if _, err := io.Copy(encoder, src); err != nil {
			_ = encoder.Close()
			_ = writer.CloseWithError(err)
			return
		}
		if err := encoder.Close(); err != nil {
			_ = writer.CloseWithError(err)
			return
		}

		if _, err := writer.Write(parts[1]); err != nil {
			_ = writer.CloseWithError(err)
			return
		}

		_ = writer.Close()
	}()

	return reader, nil
}

func buildCreateItemEnvelope(folderID, mimeContent string) ([]byte, error) {
	payload := ewsCreateItemEnvelope{
		XMLNSXSI:  "http://www.w3.org/2001/XMLSchema-instance",
		XMLNSM:    "http://schemas.microsoft.com/exchange/services/2006/messages",
		XMLNST:    "http://schemas.microsoft.com/exchange/services/2006/types",
		XMLNSSoap: "http://schemas.xmlsoap.org/soap/envelope/",
		Header: ewsCreateItemHeader{
			RequestServerVersion: ewsRequestServerVersion{Version: "Exchange2016"},
		},
		Body: ewsCreateItemRequestBody{
			CreateItem: ewsCreateItemRequest{
				MessageDisposition: "SaveOnly",
				SavedItemFolderID: ewsSavedItemFolderID{
					FolderID: ewsFolderID{ID: folderID},
				},
				Items: ewsCreateItemRequestIO{
					Message: ewsCreateMessage{
						MimeContent: ewsMimeContent{
							CharacterSet: "UTF-8",
							Value:        mimeContent,
						},
						IsRead: false,
						ExtendedProperty: ewsExtendedProperty{
							ExtendedFieldURI: ewsExtendedFieldURI{
								PropertyTag:  "0x0E07",
								PropertyType: "Integer",
							},
							Value: "1",
						},
					},
				},
			},
		},
	}

	body, err := xml.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("序列化 EWS CreateItem 请求失败: %w", err)
	}

	return append([]byte(xml.Header), body...), nil
}
