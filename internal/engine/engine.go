// Package engine 实现规则引擎：把分享码配置翻译成实际的键鼠动作。
// 纯逻辑：输入合成与按键状态通过接口注入，不直接触碰 Win32，便于单元测试。
package engine

import (
	"fmt"
	"sync"
	"time"

	"github.com/imsxx/diablo-lazy-macro/internal/config"
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
// 总开关语义（协议 v1）：enabled 为 false 时任何规则都不触发；为 true 时，
// t 规则（HoldVK == 0）持续运行，h 规则仅在触发键被按住时运行。
// 按住（hold）动作在触发结束时释放；失去前台、关闭总开关、切换方案、退出前也必须释放。
type Engine struct {
	mu      sync.Mutex
	sender  Sender
	state   KeyState
	onError func(error)
	cfg     *config.Parsed

	profile int
	enabled bool
	rules   []ruleState
}

type ruleState struct {
	rule     config.ParsedRule
	active   bool
	holding  bool
	nextFire time.Time
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
	e := &Engine{sender: sender, state: state, onError: onError, cfg: cfg}
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
			if !now.Before(st.nextFire) {
				e.fireLocked(st, st.rule.Keys[0], now)
			}
		case config.ActionSeq:
			if !now.Before(st.nextFire) {
				vk := st.rule.Keys[st.cursor%len(st.rule.Keys)]
				st.cursor++
				e.fireLocked(st, vk, now)
			}
		}
	}
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
	st.cursor = 0
}

func (e *Engine) fireLocked(st *ruleState, vk uint16, now time.Time) {
	if err := e.sender.Tap(vk); err != nil {
		e.onError(err)
	}
	st.nextFire = now.Add(time.Duration(st.rule.IntervalMS) * time.Millisecond)
}
