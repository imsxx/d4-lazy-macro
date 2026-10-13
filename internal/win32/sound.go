//go:build windows

package win32

var procBeep = kernel32.NewProc("Beep")

// 提示音的频率与时长（对齐 D3keyHelper 的 SoundBeep 750, 250）。
const (
	soundCueFreqHz   = 750
	soundCueDuration = 250
)

// PlaySoundCue 播放一声提示音，用于「切换配置成功」这类需要听觉反馈的场合。
//
// 异步执行：Windows 的 Beep 会阻塞到播放结束，而调用方通常是 UI 线程或 10ms 轮询线程，
// 同步播放会把主流程卡住 250ms。系统缺少可用音频设备时静默失败——提示音不该影响主流程。
func PlaySoundCue() {
	go func() {
		_, _, _ = procBeep.Call(uintptr(soundCueFreqHz), uintptr(soundCueDuration))
	}()
}
