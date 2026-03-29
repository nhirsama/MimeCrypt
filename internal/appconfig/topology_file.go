package appconfig

import (
	"encoding/json"
	"fmt"
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
	if err := json.Unmarshal(content, &topology); err != nil {
		return Topology{}, fmt.Errorf("解析 topology 配置失败: %w", err)
	}
	return topology.Normalize(), nil
}
