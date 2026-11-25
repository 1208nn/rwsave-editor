package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileResult 是批量修复里单个文件的结果。
type FileResult struct {
	File   string
	Status string
	Log    []string
}

// WriteWithBackup 先留 <file>.bak（已存在则保留），再写回原文件。
func WriteWithBackup(path string, data []byte) (string, error) {
	bak := path + ".bak"
	if _, err := os.Stat(bak); os.IsNotExist(err) {
		orig, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(bak, orig, 0o644); err != nil {
			return "", fmt.Errorf("写备份失败：%w", err)
		}
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("写入存档失败：%w", err)
	}
	return bak, nil
}

// FixFile 按固定规则修复单个存档：名字像人机却存成人类的队伍改成 AI。
// dry 为 true 时只计算不写入。
func FixFile(path string, dry bool) FileResult {
	res := FileResult{File: filepath.Base(path)}
	data, err := os.ReadFile(path)
	if err != nil {
		res.Status, res.Log = "失败", []string{err.Error()}
		return res
	}
	s, err := Load(data)
	if err != nil {
		res.Status, res.Log = "跳过", []string{err.Error()}
		return res
	}
	targets := s.AutoTargets()
	if len(targets) == 0 {
		res.Status = "无需修改"
		return res
	}
	out, logs, err := s.Apply(targets)
	if err != nil {
		res.Status, res.Log = "部分失败", append(logs, err.Error())
		return res
	}
	if dry {
		res.Status, res.Log = "可修复", logs
		return res
	}
	if _, err := WriteWithBackup(path, out); err != nil {
		res.Status, res.Log = "失败", []string{err.Error()}
		return res
	}
	res.Status, res.Log = "已修复", logs
	return res
}

// FixDir 处理目录下所有 .rwsave；dry 为 true 时只预览。
func FixDir(dir string, dry bool) ([]FileResult, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []FileResult
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".rwsave") {
			continue
		}
		out = append(out, FixFile(filepath.Join(dir, e.Name()), dry))
	}
	return out, nil
}
