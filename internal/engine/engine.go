// Package engine 实现规则引擎：把分享码配置翻译成实际的键鼠动作。
// 纯逻辑：输入合成与按键状态通过接口注入，不直接触碰 Win32，便于单元测试。
package engine

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/imsxx/diablo-lazy-macro/internal/config"
)

const (
	// buffResampleDelay 是「Buff 仍在生效」时的重采样间隔：既不频繁取色，也不会错过 Buff 断裂。
	buffResampleDelay = 150 * time.Millisecond
	// minDelayMS 是延迟生效阈值：|延迟| 未超过该值视为关闭（与 D3keyHelper 的 20ms 门槛一致）。
	minDelayMS = 20
	// randDelayFloorMS 是延迟随机的下限（对齐 D3keyHelper 的 Random(10, 延迟)）。
	randDelayFloorMS = 10
)

// Sender 负责实际合成输入（由 internal/win32 的实现注入）。
type Sender interface {
	Tap(vk uint16) error
	KeyDown(vk uint16) error
	KeyUp(vk uint16) error
}

// KeyState 提供按键当前是否被按下的状态（由 Win32 轮询实现注入）。
type KeyState interface {
	Pressed(vk uint16) bool
}

// Engine 是规则运行时的状态机。公共方法内部加锁，可被 UI 线程与 tick 线程并发调用。
//
// 总开关语义（协议 v2）：enabled 为 false 时任何规则都不触发；为 true 时，
// t 规则（HoldVK == 0）持续运行，h 规则仅在触发键被按住时运行。
// 按住（hold）动作在触发结束时释放；失去前台、关闭总开关、切换方案、退出前也必须释放。
type Engine struct {
	mu      sync.Mutex
	sender  Sender
	state   KeyState
	onError func(error)
	cfg     *config.Parsed

	profile     int
	enabled     bool
	rules       []ruleState
	buffChecker func(vk uint16) bool

	// randIntn 是延迟随机使用的随机源；默认 math/rand.Intn，测试可替换为确定性实现。
	randIntn func(n int) int
}

type ruleState struct {
	rule     config.ParsedRule
	active   bool
	holding  bool
	nextFire time.Time // 下一次连点周期的起点
	fireAt   time.Time // 本周期内的实际发送时刻（叠加延迟偏移后）
	burst    int       // 剩余连点次数；0 表示未处于爆发中
	cursor   int
}

// New 创建引擎。onError 不得为 nil：发送失败必须被上报，禁止静默吞错。
func New(sender Sender, state KeyState, cfg *config.Parsed, onError func(error)) (*Engine, error) {
	if sender == nil || state == nil || cfg == nil || onError == nil {
		return nil, fmt.Errorf("engine: 构造参数不得为空")
	}
	if len(cfg.Profiles) == 0 {
		return nil, fmt.Errorf("engine: 配置没有任何方案")
	}
	e := &Engine{sender: sender, state: state, onError: onError, cfg: cfg, randIntn: rand.Intn}
	e.loadProfileLocked(0)
	return e, nil
}

func (e *Engine) loadProfileLocked(i int) {
	e.profile = i
	rules := e.cfg.Profiles[i].Rules
	e.rules = make([]ruleState, 0, len(rules))
	for _, r := range rules {
		e.rules = append(e.rules, ruleState{rule: r})
	}
}

// Enabled 返回总开关状态。
func (e *Engine) Enabled() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.enabled
}

// SetEnabled 设置总开关；按住中的键会在下一次 Tick 释放。
func (e *Engine) SetEnabled(on bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.enabled = on
}

// SetBuffChecker 注入屏幕 Buff 状态检测器：入参为技能键，返回该键的 Buff 是否仍在生效。
// 只有声明了「保持Buff」策略（Buff == true）的规则会调用它，且仅在即将发送按键时调用。
func (e *Engine) SetBuffChecker(checker func(vk uint16) bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.buffChecker = checker
}

// Toggle 切换总开关并返回新状态。
func (e *Engine) Toggle() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.enabled = !e.enabled
	return e.enabled
}

// ProfileIndex 返回当前方案序号。
func (e *Engine) ProfileIndex() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.profile
}

// ProfileCount 返回方案数量。
func (e *Engine) ProfileCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.cfg.Profiles)
}

// ProfileName 返回指定方案名；序号必须合法（编程错误直接 panic，Fail Fast）。
func (e *Engine) ProfileName(i int) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if i < 0 || i >= len(e.cfg.Profiles) {
		panic(fmt.Sprintf("engine: 方案序号越界：%d", i))
	}
	return e.cfg.Profiles[i].Name
}

// SwitchVK 返回指定方案的切换键；0 表示该方案未设置切换键。序号必须合法（Fail Fast）。
func (e *Engine) SwitchVK(i int) uint16 {
	e.mu.Lock()
	defer e.mu.Unlock()
	if i < 0 || i >= len(e.cfg.Profiles) {
		panic(fmt.Sprintf("engine: 方案序号越界：%d", i))
	}
	return e.cfg.Profiles[i].SwitchVK
}

// SetProfile 切换方案：先释放旧方案按住中的键，再装载新方案。
func (e *Engine) SetProfile(i int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if i < 0 || i >= len(e.cfg.Profiles) {
		return fmt.Errorf("方案序号 %d 超出范围（共 %d 个方案）", i, len(e.cfg.Profiles))
	}
	e.releaseHoldsLocked()
	e.loadProfileLocked(i)
	return nil
}

// ReleaseAll 立即释放所有按住中的键（幂等）。退出前与切换方案前必须调用。
func (e *Engine) ReleaseAll() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.releaseHoldsLocked()
}

func (e *Engine) releaseHoldsLocked() {
	for i := range e.rules {
		st := &e.rules[i]
		if !st.holding {
			continue
		}
		if err := e.sender.KeyUp(st.rule.Keys[0]); err != nil {
			e.onError(err)
		}
		st.holding = false
	}
}

// Tick 推进一次状态机；调用方（主循环）按约 10ms 周期驱动。
// foreground 表示目标进程是否位于前台；非前台时不发送、按住动作释放，恢复后重新激活。
func (e *Engine) Tick(now time.Time, foreground bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.rules {
		st := &e.rules[i]
		active := e.enabled && foreground
		if active && st.rule.HoldVK != 0 {
			active = e.state.Pressed(st.rule.HoldVK)
		}
		if active != st.active {
			st.active = active
			if active {
				e.activateLocked(st, now)
			} else {
				e.deactivateLocked(st)
			}
		}
		if !st.active {
			continue
		}
		switch st.rule.Kind {
		case config.ActionTap:
			e.tickTapLocked(st, now)
		case config.ActionSeq:
			if !now.Before(st.nextFire) {
				vk := st.rule.Keys[st.cursor%len(st.rule.Keys)]
				st.cursor++
				e.fireLocked(st, vk, now)
			}
		}
	}
}

// tickTapLocked 推进连点策略：周期起点 → 延迟偏移 → 连点爆发 → 周期推进。
// 需要屏幕守卫时，若自身 Buff 仍在生效、或同方案存在优先级更高且仍在生效的 Buff，本轮整段让路。
func (e *Engine) tickTapLocked(st *ruleState, now time.Time) {
	if now.Before(st.nextFire) {
		return
	}
	if st.fireAt.IsZero() {
		st.fireAt = st.nextFire.Add(e.delayOffsetLocked(st.rule))
	}
	if now.Before(st.fireAt) {
		return
	}
	if st.burst <= 0 {
		st.burst = st.rule.Repeat
		if st.burst < 1 {
			st.burst = 1
		}
	}
	if e.buffGateLocked(st) {
		// Buff 仍在生效：不按键、不消耗重复次数，稍后重新采样。
		st.burst = 0
		st.fireAt = time.Time{}
		st.nextFire = now.Add(buffResampleDelay)
		return
	}
	if err := e.sender.Tap(st.rule.Keys[0]); err != nil {
		e.onError(err)
	}
	st.burst--
	if st.burst > 0 {
		// 连点重复：按重复间隔继续爆发，周期起点保持不变。
		st.fireAt = now.Add(e.repeatIntervalLocked(st.rule))
		return
	}
	st.nextFire = now.Add(time.Duration(st.rule.IntervalMS) * time.Millisecond)
	st.fireAt = time.Time{}
}

// buffGateLocked 判断本次连点是否应因 Buff 状态让路：
// 自身 Buff 仍在生效，或同方案存在优先级更高且仍在生效的 Buff（复刻 D3keyHelper 的仲裁）。
func (e *Engine) buffGateLocked(st *ruleState) bool {
	if !st.rule.Buff || e.buffChecker == nil {
		return false
	}
	if e.buffChecker(st.rule.Keys[0]) {
		return true
	}
	for i := range e.rules {
		other := &e.rules[i]
		if other == st || !other.rule.Buff || other.rule.Priority <= st.rule.Priority {
			continue
		}
		if e.buffChecker(other.rule.Keys[0]) {
			return true
		}
	}
	return false
}

// delayOffsetLocked 计算本周期内发送时点的偏移：
// 正延迟在周期内偏后等待；负延迟推迟到周期末尾前 |延迟|（等价于 interval + 延迟）。
// |延迟| 未超过阈值视为关闭；勾选延迟随机时取 0～|延迟| 之间的随机值。
func (e *Engine) delayOffsetLocked(r config.ParsedRule) time.Duration {
	raw := r.DelayMS
	if raw == 0 {
		return 0
	}
	mag := raw
	if mag < 0 {
		mag = -mag
	}
	if mag <= minDelayMS {
		return 0
	}
	if r.RandomDelay {
		mag = randDelayFloorMS + e.randIntn(mag-randDelayFloorMS+1)
	}
	offset := mag
	if raw < 0 {
		offset = r.IntervalMS - mag
		if offset < 0 {
			offset = 0
		}
	}
	return time.Duration(offset) * time.Millisecond
}

// repeatIntervalLocked 返回连点重复之间的间隔。
func (e *Engine) repeatIntervalLocked(r config.ParsedRule) time.Duration {
	if r.RepeatInterval > 0 {
		return time.Duration(r.RepeatInterval) * time.Millisecond
	}
	return time.Duration(r.IntervalMS) * time.Millisecond
}

func (e *Engine) activateLocked(st *ruleState, now time.Time) {
	switch st.rule.Kind {
	case config.ActionOnce:
		if err := e.sender.Tap(st.rule.Keys[0]); err != nil {
			e.onError(err)
		}
	case config.ActionHold:
		if err := e.sender.KeyDown(st.rule.Keys[0]); err != nil {
			e.onError(err)
			return
		}
		st.holding = true
	default: // tap / seq：激活即触发一次
		st.nextFire = now
		st.fireAt = time.Time{}
		st.burst = 0
	}
}

func (e *Engine) deactivateLocked(st *ruleState) {
	if st.holding {
		if err := e.sender.KeyUp(st.rule.Keys[0]); err != nil {
			e.onError(err)
		}
		st.holding = false
	}
	st.nextFire = time.Time{}
	st.fireAt = time.Time{}
	st.burst = 0
	st.cursor = 0
}

func (e *Engine) fireLocked(st *ruleState, vk uint16, now time.Time) {
	if err := e.sender.Tap(vk); err != nil {
		e.onError(err)
	}
	st.nextFire = now.Add(time.Duration(st.rule.IntervalMS) * time.Millisecond)
}
