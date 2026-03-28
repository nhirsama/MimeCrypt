package cli

import "errors"

var ErrRunLocked = errors.New("已有另一个 mimecrypt run 实例在运行")
