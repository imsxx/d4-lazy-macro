// Package games 定义支持的暗黑系列游戏，是协议标识、显示名与默认目标进程的唯一事实源。
package games

// Game 描述一款受支持的游戏。
type Game struct {
	ID             string // 协议标识（分享码 g 字段）
	Name           string // 显示名
	DefaultProcess string // 默认目标进程名（D4 已实测；D3/D2R 未实测，可在设置中覆盖）
}

var all = []Game{
	{ID: "d4", Name: "暗黑破坏神4", DefaultProcess: "Diablo IV.exe"},
	{ID: "d3", Name: "暗黑破坏神3", DefaultProcess: "Diablo III64.exe"},
	{ID: "d2r", Name: "暗黑破坏神2：重制版", DefaultProcess: "D2R.exe"},
}

// All 返回全部受支持游戏（副本，避免外部修改内部表）。
func All() []Game {
	out := make([]Game, len(all))
	copy(out, all)
	return out
}

// Lookup 按协议标识查找游戏。
func Lookup(id string) (Game, bool) {
	for _, g := range all {
		if g.ID == id {
			return g, true
		}
	}
	return Game{}, false
}

// Default 返回默认游戏（暗黑破坏神4）。
func Default() Game {
	return all[0]
}
