// Package store 把各游戏配置与工具设置持久化到本地文件（默认 %APPDATA%\diablo-lazy-macro）。
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/imsxx/diablo-lazy-macro/internal/config"
	"github.com/imsxx/diablo-lazy-macro/internal/games"
)

const (
	// DirName 是默认数据目录名。
	DirName = "diablo-lazy-macro"
	// SettingsFile 是工具级设置文件名。
	SettingsFile = "settings.json"
)

// ConfigFile 返回某款游戏的配置文件名。
func ConfigFile(gameID string) string {
	return "config-" + gameID + ".json"
}

// Snapshot 是一款游戏的持久化配置。
type Snapshot struct {
	Config     config.Raw `json:"config"`
	ProfileIdx int        `json:"profile_index"`
	StartMode  int        `json:"start_mode,omitempty"` // 0: 懒人模式(开关切换), 1: 仅按下时(按住生效)
}

// Settings 是工具级设置（不随分享码分发）。
type Settings struct {
	CurrentGame string            `json:"current_game"`
	Processes   map[string]string `json:"processes,omitempty"` // 各游戏目标进程名的本地覆盖
}

// DefaultDir 返回默认数据目录（%APPDATA%\diablo-lazy-macro）。
func DefaultDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("无法定位用户配置目录：%v", err)
	}
	return filepath.Join(base, DirName), nil
}

// Path 返回数据目录下某文件的完整路径。
func Path(dir, name string) string {
	return filepath.Join(dir, name)
}

// DefaultSettings 返回缺省设置（当前游戏为默认游戏）。
func DefaultSettings() *Settings {
	return &Settings{CurrentGame: games.Default().ID}
}

// Validate 校验设置内容（Fail Fast）。
func (s *Settings) Validate() error {
	if _, ok := games.Lookup(s.CurrentGame); !ok {
		return fmt.Errorf("当前游戏 %q 无效", s.CurrentGame)
	}
	for id := range s.Processes {
		if _, ok := games.Lookup(id); !ok {
			return fmt.Errorf("进程覆盖表包含未知游戏 %q", id)
		}
	}
	return nil
}

// TargetProcess 返回某游戏的目标进程名（含本地覆盖）。
func (s *Settings) TargetProcess(gameID string) (string, error) {
	if p := s.Processes[gameID]; p != "" {
		return p, nil
	}
	g, ok := games.Lookup(gameID)
	if !ok {
		return "", fmt.Errorf("未知游戏 %q", gameID)
	}
	return g.DefaultProcess, nil
}

// Load 读取某游戏配置；文件不存在返回 (nil, nil)，表示尚未保存。
// 存档存在但损坏、或其中配置不合法时返回错误，由调用方决定处置（禁止静默忽略）。
func Load(dir, name string) (*Snapshot, error) {
	path := Path(dir, name)
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

// Save 校验并原子写入某游戏配置。
func Save(dir, name string, s Snapshot) error {
	if _, err := config.Encode(s.Config); err != nil {
		return fmt.Errorf("拒绝保存非法配置：%v", err)
	}
	if s.ProfileIdx < 0 || s.ProfileIdx >= len(s.Config.Profiles) {
		return fmt.Errorf("方案序号越界：%d", s.ProfileIdx)
	}
	data, err := json.MarshalIndent(&s, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化存档失败：%v", err)
	}
	return writeAtomic(dir, name, data)
}

// LoadSettings 读取工具设置；文件不存在返回默认设置。
func LoadSettings(dir string) (*Settings, error) {
	path := Path(dir, SettingsFile)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return DefaultSettings(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败：%v", path, err)
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("解析 %s 失败：%v", path, err)
	}
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("%s 内容不合法：%v", path, err)
	}
	return &s, nil
}

// SaveSettings 校验并原子写入工具设置。
func SaveSettings(dir string, s Settings) error {
	if err := s.Validate(); err != nil {
		return fmt.Errorf("拒绝保存非法设置：%v", err)
	}
	data, err := json.MarshalIndent(&s, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化设置失败：%v", err)
	}
	return writeAtomic(dir, SettingsFile, data)
}

func writeAtomic(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建目录 %s 失败：%v", dir, err)
	}
	path := Path(dir, name)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("写入 %s 失败：%v", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("替换 %s 失败：%v", path, err)
	}
	return nil
}

// Quarantine 把损坏的文件重命名为 .bad-<时间戳> 保留现场，返回新路径。
func Quarantine(dir, name string) (string, error) {
	path := Path(dir, name)
	target := fmt.Sprintf("%s.bad-%s", path, time.Now().Format("20060102-150405"))
	if err := os.Rename(path, target); err != nil {
		return "", fmt.Errorf("重命名损坏文件失败：%v", err)
	}
	return target, nil
}
