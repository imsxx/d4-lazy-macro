package engine

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/imsxx/diablo-lazy-macro/internal/config"
	"github.com/imsxx/diablo-lazy-macro/internal/keys"
)

type fakeSender struct {
	events []string
	fail   bool
}

func (f *fakeSender) record(op string, vk uint16) error {
	if f.fail {
		return fmt.Errorf("模拟发送失败")
	}
	f.events = append(f.events, fmt.Sprintf("%s:%s", op, keys.Label(vk)))
	return nil
}

func (f *fakeSender) Tap(vk uint16) error     { return f.record("tap", vk) }
func (f *fakeSender) KeyDown(vk uint16) error { return f.record("down", vk) }
func (f *fakeSender) KeyUp(vk uint16) error   { return f.record("up", vk) }

type fakeKeys map[uint16]bool

func (f fakeKeys) Pressed(vk uint16) bool { return f[vk] }

var base = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

func newTestEngine(t *testing.T, profiles ...[]config.ParsedRule) (*Engine, *fakeSender, fakeKeys, *[]string) {
	t.Helper()
	sender := &fakeSender{}
	state := fakeKeys{}
	var errs []string
	var ps []config.ParsedProfile
	for _, rules := range profiles {
		ps = append(ps, config.ParsedProfile{Rules: rules})
	}
	e, err := New(sender, state, &config.Parsed{ToggleVK: 0x70, Profiles: ps}, func(err error) {
		errs = append(errs, err.Error())
	})
	if err != nil {
		t.Fatalf("New 失败：%v", err)
	}
	return e, sender, state, &errs
}

func expectEvents(t *testing.T, sender *fakeSender, want ...string) {
	t.Helper()
	if got := strings.Join(sender.events, ","); got != strings.Join(want, ",") {
		t.Fatalf("事件序列不符\n实际：%s\n期望：%s", got, strings.Join(want, ","))
	}
}

func TestDisabledSendsNothing(t *testing.T) {
	e, sender, _, _ := newTestEngine(t, []config.ParsedRule{{Kind: config.ActionTap, Keys: []uint16{0x34}, IntervalMS: 100}})
	e.Tick(base, true)
	e.Tick(base.Add(time.Second), true)
	expectEvents(t, sender)
}

func TestForegroundGate(t *testing.T) {
	e, sender, _, _ := newTestEngine(t, []config.ParsedRule{{Kind: config.ActionTap, Keys: []uint16{0x34}, IntervalMS: 100}})
	e.SetEnabled(true)
	e.Tick(base, false)
	e.Tick(base.Add(time.Second), false)
	expectEvents(t, sender)
	e.Tick(base.Add(2*time.Second), true)
	expectEvents(t, sender, "tap:4")
}

func TestTapFiresOnInterval(t *testing.T) {
	e, sender, _, _ := newTestEngine(t, []config.ParsedRule{{Kind: config.ActionTap, Keys: []uint16{0x34}, IntervalMS: 250}})
	e.SetEnabled(true)
	e.Tick(base, true)
	e.Tick(base.Add(100*time.Millisecond), true)
	e.Tick(base.Add(249*time.Millisecond), true)
	e.Tick(base.Add(250*time.Millisecond), true)
	expectEvents(t, sender, "tap:4", "tap:4")
}

func TestHoldTriggerGatesRule(t *testing.T) {
	e, sender, state, _ := newTestEngine(t, []config.ParsedRule{
		{HoldVK: 0x05, Kind: config.ActionTap, Keys: []uint16{0x31}, IntervalMS: 100},
	})
	e.SetEnabled(true)
	e.Tick(base, true)
	expectEvents(t, sender)
	state[0x05] = true
	e.Tick(base.Add(10*time.Millisecond), true)
	expectEvents(t, sender, "tap:1")
	state[0x05] = false
	e.Tick(base.Add(20*time.Millisecond), true)
	e.Tick(base.Add(30*time.Millisecond), true)
	expectEvents(t, sender, "tap:1")
}

func TestOnceFiresPerActivation(t *testing.T) {
	e, sender, _, _ := newTestEngine(t, []config.ParsedRule{{Kind: config.ActionOnce, Keys: []uint16{0x31}}})
	e.SetEnabled(true)
	e.Tick(base, true)
	e.Tick(base.Add(time.Second), true)
	expectEvents(t, sender, "tap:1")
	e.SetEnabled(false)
	e.Tick(base.Add(2*time.Second), true)
	e.SetEnabled(true)
	e.Tick(base.Add(3*time.Second), true)
	expectEvents(t, sender, "tap:1", "tap:1")
}

func TestHoldActionPressAndRelease(t *testing.T) {
	e, sender, _, _ := newTestEngine(t, []config.ParsedRule{{Kind: config.ActionHold, Keys: []uint16{0x02}}})
	e.SetEnabled(true)
	e.Tick(base, true)
	e.Tick(base.Add(time.Second), true)
	expectEvents(t, sender, "down:RMB")
	e.SetEnabled(false)
	e.Tick(base.Add(2*time.Second), true)
	expectEvents(t, sender, "down:RMB", "up:RMB")
}

func TestHoldReleasedOnForegroundLossAndReleaseAll(t *testing.T) {
	e, sender, _, _ := newTestEngine(t, []config.ParsedRule{{Kind: config.ActionHold, Keys: []uint16{0x02}}})
	e.SetEnabled(true)
	e.Tick(base, true)
	e.Tick(base.Add(time.Second), false)
	expectEvents(t, sender, "down:RMB", "up:RMB")
	e.Tick(base.Add(2*time.Second), true)
	e.ReleaseAll()
	e.ReleaseAll()
	expectEvents(t, sender, "down:RMB", "up:RMB", "down:RMB", "up:RMB")
}

func TestSeqCyclesKeys(t *testing.T) {
	e, sender, _, _ := newTestEngine(t, []config.ParsedRule{{Kind: config.ActionSeq, Keys: []uint16{0x32, 0x33}, IntervalMS: 100}})
	e.SetEnabled(true)
	e.Tick(base, true)
	e.Tick(base.Add(100*time.Millisecond), true)
	e.Tick(base.Add(200*time.Millisecond), true)
	expectEvents(t, sender, "tap:2", "tap:3", "tap:2")
}

func TestSetProfileReleasesAndSwitches(t *testing.T) {
	p0 := []config.ParsedRule{{Kind: config.ActionHold, Keys: []uint16{0x02}}}
	p1 := []config.ParsedRule{{Kind: config.ActionTap, Keys: []uint16{0x31}, IntervalMS: 100}}
	e, sender, _, _ := newTestEngine(t, p0, p1)
	e.SetEnabled(true)
	e.Tick(base, true)
	if err := e.SetProfile(1); err != nil {
		t.Fatalf("SetProfile 失败：%v", err)
	}
	e.Tick(base.Add(time.Second), true)
	expectEvents(t, sender, "down:RMB", "up:RMB", "tap:1")
	if err := e.SetProfile(9); err == nil {
		t.Fatalf("越界方案序号应当报错")
	}
}

func TestToggleReturnsState(t *testing.T) {
	e, _, _, _ := newTestEngine(t, []config.ParsedRule{{Kind: config.ActionTap, Keys: []uint16{0x31}, IntervalMS: 100}})
	if e.Toggle() != true || e.Enabled() != true {
		t.Fatalf("Toggle 应返回 true")
	}
	if e.Toggle() != false || e.Enabled() != false {
		t.Fatalf("再次 Toggle 应返回 false")
	}
}

func TestSendErrorsReported(t *testing.T) {
	e, sender, _, errs := newTestEngine(t, []config.ParsedRule{{Kind: config.ActionTap, Keys: []uint16{0x31}, IntervalMS: 100}})
	sender.fail = true
	e.SetEnabled(true)
	e.Tick(base, true)
	if len(*errs) != 1 || !strings.Contains((*errs)[0], "模拟发送失败") {
		t.Fatalf("发送失败未被上报：%v", *errs)
	}
}
