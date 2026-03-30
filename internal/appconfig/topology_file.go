package appconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const defaultTopologyFileName = "topology.json"

func DefaultTopologyPath(stateDir string) string {
	return filepath.Join(strings.TrimSpace(stateDir), defaultTopologyFileName)
}

func LoadTopologyFile(path string) (Topology, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Topology{}, fmt.Errorf("topology path 不能为空")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return Topology{}, fmt.Errorf("读取 topology 配置失败: %w", err)
	}

	var topology Topology
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&topology); err != nil {
		return Topology{}, fmt.Errorf("解析 topology 配置失败: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return Topology{}, fmt.Errorf("解析 topology 配置失败: 存在多余的 JSON 内容")
		}
		return Topology{}, fmt.Errorf("解析 topology 配置失败: %w", err)
	}
	return topology.Normalize(), nil
}

func SaveTopologyFile(path string, topology Topology) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("topology path 不能为空")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("创建 topology 目录失败: %w", err)
	}

	content, err := json.MarshalIndent(topology.Normalize(), "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 topology 配置失败: %w", err)
	}
	content = append(content, '\n')
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return fmt.Errorf("写入 topology 配置失败: %w", err)
	}
	return nil
}
