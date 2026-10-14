package engine

import (
	"testing"

	"github.com/imsxx/diablo-lazy-macro/internal/config"
)

// 界面编辑规则时会重建引擎：运行态（开关 / 暂停）必须能被继承，
// 否则「改一个槽位就把宏重新打开」或把暂停状态丢掉。
func TestRuntimeStateRoundTrip(t *testing.T) {
	e, _, _, _ := newTestEngine(t, []config.ParsedRule{{
		Kind: config.ActionTap, Keys: []uint16{0x31}, IntervalMS: 250,
	}})

	e.SetEnabled(false)
	if got := e.RuntimeState(); got.Enabled || got.Paused {
		t.Fatalf("关闭后运行态应为 {false false}，实际 %+v", got)
	}

	e.ApplyRuntimeState(RuntimeState{Enabled: true, Paused: true})
	if !e.Enabled() || !e.Paused() {
		t.Fatalf("继承启用+暂停失败：enabled=%v paused=%v", e.Enabled(), e.Paused())
	}

	e.ApplyRuntimeState(RuntimeState{Enabled: true, Paused: false})
	if e.Paused() {
		t.Fatal("继承非暂停态后不应仍处于暂停")
	}
	if !e.Enabled() {
		t.Fatal("继承启用态失败")
	}

	e.ApplyRuntimeState(RuntimeState{Enabled: false})
	if e.Enabled() {
		t.Fatal("继承关闭态失败")
	}
}
