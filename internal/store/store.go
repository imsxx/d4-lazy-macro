// Package store 把当前配置持久化到本地文件（默认 %APPDATA%\d4-lazy-macro\config.json）。
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/imsxx/d4-lazy-macro/internal/config"
)

// FileName 是持久化文件名。
const FileName = "config.json"

// Snapshot 是工具需要持久化的全部状态。
type Snapshot struct {
	Config     config.Raw `json:"config"`
	ProfileIdx int        `json:"profile_index"`
}

// DefaultDir 返回默认数据目录（%APPDATA%\d4-lazy-macro）。
func DefaultDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("无法定位用户配置目录：%v", err)
	}
	return filepath.Join(base, "d4-lazy-macro"), nil
}

// Path 返回存档的完整路径。
func Path(dir string) string {
	return filepath.Join(dir, FileName)
}

// Load 读取存档；文件不存在返回 (nil, nil)，表示尚未保存过配置。
// 存档存在但损坏、或其中配置不合法时返回错误，由调用方决定处置（禁止静默忽略）。
func Load(dir string) (*Snapshot, error) {
	path := Path(dir)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败：%v", path, err)
	}
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("解析 %s 失败：%v", path, err)
	}
	if _, err := config.Encode(s.Config); err != nil {
		return nil, fmt.Errorf("存档中的配置不合法（%s）：%v", path, err)
	}
	if s.ProfileIdx < 0 || s.ProfileIdx >= len(s.Config.Profiles) {
		return nil, fmt.Errorf("存档中的方案序号越界（%s）：%d", path, s.ProfileIdx)
	}
	return &s, nil
}

// Save 校验并原子写入存档。
func Save(dir string, s Snapshot) error {
	if _, err := config.Encode(s.Config); err != nil {
		return fmt.Errorf("拒绝保存非法配置：%v", err)
	}
	if s.ProfileIdx < 0 || s.ProfileIdx >= len(s.Config.Profiles) {
		return fmt.Errorf("方案序号越界：%d", s.ProfileIdx)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建目录 %s 失败：%v", dir, err)
	}
	data, err := json.MarshalIndent(&s, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化存档失败：%v", err)
	}
	path := Path(dir)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("写入 %s 失败：%v", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("替换 %s 失败：%v", path, err)
	}
	return nil
}

// Quarantine 把损坏的存档重命名为 .bad-<时间戳> 保留现场，返回新路径。
func Quarantine(dir string) (string, error) {
	path := Path(dir)
	target := fmt.Sprintf("%s.bad-%s", path, time.Now().Format("20060102-150405"))
	if err := os.Rename(path, target); err != nil {
		return "", fmt.Errorf("重命名损坏存档失败：%v", err)
	}
	return target, nil
}
