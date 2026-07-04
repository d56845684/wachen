// Package envutil 收攏各 cmd 重複的環境變數讀取
package envutil

import (
	"log/slog"
	"os"
)

// Must 讀取必要環境變數，缺少即終止程序
func Must(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("missing required env", "key", key)
		os.Exit(1)
	}
	return v
}

// Or 讀取環境變數，未設定時回傳預設值
func Or(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
