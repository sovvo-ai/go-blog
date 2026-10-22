package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// LoadDotEnv 读取 KEY=VALUE 形式的本地环境文件，写入进程环境变量。
//
// 仅用于本地开发：已存在的环境变量优先，不会被文件覆盖。
// 生产环境由容器编排注入环境变量，不读取该文件。
//
// 支持：空行、# 注释、单双引号包裹、export 前缀、值内含 =。
func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("打开环境文件 %s 失败: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")

		key, val, found := strings.Cut(line, "=")
		if !found {
			return fmt.Errorf("环境文件第 %d 行格式非法（缺少 =）", lineNo)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return fmt.Errorf("环境文件第 %d 行 key 为空", lineNo)
		}

		val = strings.TrimSpace(val)
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, val); err != nil {
			return fmt.Errorf("设置环境变量 %s 失败: %w", key, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("读取环境文件失败: %w", err)
	}
	return nil
}
