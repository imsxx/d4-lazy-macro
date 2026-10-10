package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/imsxx/d4-lazy-macro/internal/config"
)

func sampleSnapshot() Snapshot {
	return Snapshot{
		Config: config.Raw{
			Version: 1,
			Name:    "测试配置",
			Toggle:  "F1",
			Profiles: []config.RawProfile{
				{Name: "速刷", Rules: []config.RawRule{{Trigger: "t", Action: "tap:4", IntervalMS: 250}}},
				{Name: "攻坚", Rules: []config.RawRule{{Trigger: "h:X1", Action: "seq:2,3", IntervalMS: 200}}},
			},
		},
		ProfileIdx: 1,
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := sampleSnapshot()
	if err := Save(dir, in); err != nil {
		t.Fatalf("Save 失败：%v", err)
	}
	out, err := Load(dir)
	if err != nil {
		t.Fatalf("Load 失败：%v", err)
	}
	if out == nil {
		t.Fatalf("Load 返回空存档")
	}
	if out.ProfileIdx != in.ProfileIdx || out.Config.Name != in.Config.Name || len(out.Config.Profiles) != 2 {
		t.Fatalf("回读内容不符：%+v", out)
	}
	if out.Config.Profiles[1].Rules[0].Action != "seq:2,3" {
		t.Fatalf("回读规则不符：%+v", out.Config.Profiles[1].Rules[0])
	}
}

func TestLoadMissingReturnsNil(t *testing.T) {
	s, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load 缺省目录应无错误：%v", err)
	}
	if s != nil {
		t.Fatalf("缺省目录应返回 nil 存档")
	}
}

func TestLoadCorruptThenQuarantine(t *testing.T) {
	dir := t.TempDir()
	path := Path(dir)
	if err := os.WriteFile(path, []byte("这不是 JSON"), 0o644); err != nil {
		t.Fatalf("准备损坏文件失败：%v", err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatalf("损坏存档应当报错")
	}
	moved, err := Quarantine(dir)
	if err != nil {
		t.Fatalf("Quarantine 失败：%v", err)
	}
	if _, err := os.Stat(moved); err != nil {
		t.Fatalf("隔离后的文件不存在：%v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("原存档应已被移走")
	}
	if s, err := Load(dir); err != nil || s != nil {
		t.Fatalf("隔离后应回到干净状态：%v %v", s, err)
	}
}

func TestSaveRejectsInvalid(t *testing.T) {
	bad := Snapshot{Config: config.Raw{Version: 1, Profiles: []config.RawProfile{
		{Rules: []config.RawRule{{Trigger: "t", Action: "tap:1", IntervalMS: 5}}},
	}}}
	if err := Save(t.TempDir(), bad); err == nil {
		t.Fatalf("非法配置应当拒绝保存")
	}
}

func TestLoadRejectsBadProfileIndex(t *testing.T) {
	dir := t.TempDir()
	content := `{"config":{"v":1,"p":[{"r":[{"tg":"t","a":"tap:1","i":100}]}]},"profile_index":5}`
	if err := os.WriteFile(Path(dir), []byte(content), 0o644); err != nil {
		t.Fatalf("准备文件失败：%v", err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatalf("越界方案序号应当报错")
	}
}

func TestDefaultDirNotEmpty(t *testing.T) {
	dir, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir 失败：%v", err)
	}
	if !filepath.IsAbs(dir) {
		t.Fatalf("DefaultDir 应返回绝对路径：%s", dir)
	}
}
