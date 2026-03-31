package appconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

func cloneRawMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func decodeStrictRawMessage(raw json.RawMessage, target any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return fmt.Errorf("缺少驱动配置")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("驱动配置包含多余的 JSON 内容")
		}
		return err
	}
	return nil
}

func (s Source) WithDriverConfig(value any) (Source, error) {
	s = s.Configured()
	if value == nil {
		s.DriverConfig = nil
		return s, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return Source{}, fmt.Errorf("序列化 source driver config 失败: %w", err)
	}
	s.DriverConfig = cloneRawMessage(raw)
	return s, nil
}

func (s Source) DecodeDriverConfig(target any) error {
	if err := decodeStrictRawMessage(s.DriverConfig, target); err != nil {
		return fmt.Errorf("解析 source %s driver config 失败: %w", s.Name, err)
	}
	return nil
}
