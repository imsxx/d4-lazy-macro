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

	// 顺序（循环按键）模式的调度状态：游标指向下一个候选规则，seqNext 是下一次发送时刻。
	mode      string
	seqCursor int
	seqNext   time.Time

	// randIntn 是延迟随机使用的随机源；默认 math/rand.Intn，测试可替换为确定性实现。
	randIntn func(n int) int
}

type ruleState struct {
	rule     config.ParsedRule
	active   bool
	holding  bool
	nextFire time.Time // 下一次连点周期的起点
	fireAt   time.Time // 本周期内的实际发送时刻（叠加延迟偏移后）
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
	e.mode = e.cfg.Profiles[i].Mode
	if e.mode == "" {
		e.mode = config.ModeConcurrent
	}
	e.seqCursor = 0
	e.seqNext = time.Time{}
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
	if e.mode == config.ModeSequential {
		e.tickSequentialLocked(now, foreground)
		return
	}
	for i := range e.rules {
		st := &e.rules[i]
		e.updateActiveLocked(st, now, foreground)
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

// updateActiveLocked 依据总开关、前台与按住触发键维护一条规则的激活状态。
func (e *Engine) updateActiveLocked(st *ruleState, now time.Time, foreground bool) {
	active := e.enabled && foreground
	if active && st.rule.HoldVK != 0 {
		active = e.state.Pressed(st.rule.HoldVK)
	}
	if active == st.active {
		return
	}
	st.active = active
	if active {
		e.activateLocked(st, now)
	} else {
		e.deactivateLocked(st)
	}
}

// tickSequentialLocked 顺序（循环按键）模式：
// 按住/单点类规则仍按各自语义独立维持；连点类规则按槽位顺序轮流触发，
// 每条规则触发后等待自己的执行间隔，一轮走完再从第一条继续——即罗技/雷蛇式顺序宏的心智。
func (e *Engine) tickSequentialLocked(now time.Time, foreground bool) {
	for i := range e.rules {
		st := &e.rules[i]
		if st.rule.Kind == config.ActionTap {
			continue // 连点由顺序轮统一调度
		}
		e.updateActiveLocked(st, now, foreground)
	}
	if !e.enabled || !foreground || len(e.rules) == 0 {
		return
	}
	if now.Before(e.seqNext) {
		return
	}
	// 从游标处找下一条可发送的连点规则：触发键未按住、或 Buff 仍在生效的规则跳过，游标继续前进。
	for n := 0; n < len(e.rules); n++ {
		st := &e.rules[e.seqCursor%len(e.rules)]
		e.seqCursor = (e.seqCursor + 1) % len(e.rules)
		if st.rule.Kind != config.ActionTap {
			continue
		}
		if st.rule.HoldVK != 0 && !e.state.Pressed(st.rule.HoldVK) {
			continue
		}
		if e.buffGateLocked(st) {
			continue
		}
		if err := e.sender.Tap(st.rule.Keys[0]); err != nil {
			e.onError(err)
		}
		e.seqNext = now.Add(e.delayOffsetLocked(st.rule) + time.Duration(st.rule.IntervalMS)*time.Millisecond)
		return
	}
	// 一轮下来没有任何技能可发送（全部被 Buff 守卫拦下）：稍后重试。
	e.seqNext = now.Add(buffResampleDelay)
}

// tickTapLocked 推进连点策略：周期起点 → 延迟偏移 → 发送 → 周期推进。
// 需要屏幕守卫时，若该技能 Buff 仍在生效，本轮让路、稍后重新采样。
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
	if e.buffGateLocked(st) {
		// Buff 仍在生效：不按键，稍后重新采样。
		st.fireAt = time.Time{}
		st.nextFire = now.Add(buffResampleDelay)
		return
	}
	if err := e.sender.Tap(st.rule.Keys[0]); err != nil {
		e.onError(err)
	}
	st.nextFire = now.Add(time.Duration(st.rule.IntervalMS) * time.Millisecond)
	st.fireAt = time.Time{}
}

// buffGateLocked 判断本次连点是否应因「该技能 Buff 仍在生效」而让路。
func (e *Engine) buffGateLocked(st *ruleState) bool {
	return st.rule.Buff && e.buffChecker != nil && e.buffChecker(st.rule.Keys[0])
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
	st.cursor = 0
}

func (e *Engine) fireLocked(st *ruleState, vk uint16, now time.Time) {
	if err := e.sender.Tap(vk); err != nil {
		e.onError(err)
	}
	st.nextFire = now.Add(time.Duration(st.rule.IntervalMS) * time.Millisecond)
}
