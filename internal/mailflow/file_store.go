package mailflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mimecrypt/internal/fileutil"
)

// FileStateStore 将事务状态按 key 持久化到目录中的单独 JSON 文件。
type FileStateStore struct {
	Dir string
}

func (s FileStateStore) Load(_ context.Context, key string) (TxState, bool, error) {
	path, err := s.pathForKey(key)
	if err != nil {
		return TxState{}, false, err
	}

	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return TxState{}, false, nil
		}
		return TxState{}, false, fmt.Errorf("读取事务状态失败: %w", err)
	}

	var state TxState
	if err := json.Unmarshal(content, &state); err != nil {
		return TxState{}, false, fmt.Errorf("解析事务状态失败: %w", err)
	}
	return state, true, nil
}

func (s FileStateStore) Save(_ context.Context, state TxState) error {
	path, err := s.pathForKey(state.Key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("创建事务状态目录失败: %w", err)
	}

	content, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化事务状态失败: %w", err)
	}
	content = append(content, '\n')

	if _, err := fileutil.WriteFileAtomic(path, 0o600, bytes.NewReader(content)); err != nil {
		return fmt.Errorf("保存事务状态失败: %w", err)
	}
	return nil
}

func (s FileStateStore) pathForKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("transaction key 不能为空")
	}
	dir := strings.TrimSpace(s.Dir)
	if dir == "" {
		return "", fmt.Errorf("state dir 不能为空")
	}
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(dir, hex.EncodeToString(sum[:])+".json"), nil
}
