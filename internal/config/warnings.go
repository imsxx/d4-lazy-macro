package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/imsxx/diablo-lazy-macro/internal/keys"
)

// Warnings 返回配置中的可疑项（不阻断导入与保存，仅用于提示）。检查三类问题：
//
//  1. 同一个键既是触发键（启停键、按住触发键或方案切换键）又是动作键，会形成自我触发叠加
//  2. 同一方案切换键与启停键相同
//  3. 多个方案共用同一个切换键（同一时刻只有其中一个能生效）
func Warnings(p *Parsed) []string {
	triggerRoles := map[uint16]string{}
	if p.ToggleVK != 0 {
		triggerRoles[p.ToggleVK] = "启停键"
	}

	switchUsers := map[uint16][]string{}
	for i, prof := range p.Profiles {
		name := prof.Name
		if name == "" {
			name = fmt.Sprintf("方案 %d", i+1)
		}
		if prof.SwitchVK == 0 {
			continue
		}
		switchUsers[prof.SwitchVK] = append(switchUsers[prof.SwitchVK], name)
		if _, exists := triggerRoles[prof.SwitchVK]; !exists {
			triggerRoles[prof.SwitchVK] = "方案切换键"
		}
	}
	for _, prof := range p.Profiles {
		for _, r := range prof.Rules {
			if r.HoldVK == 0 {
				continue
			}
			if _, exists := triggerRoles[r.HoldVK]; !exists {
				triggerRoles[r.HoldVK] = "触发键"
			}
		}
	}

	var warnings []string

	// 方案切换键之间的重复（按键码排序，保证顺序稳定）
	dups := make([]uint16, 0, len(switchUsers))
	for vk, users := range switchUsers {
		if len(users) > 1 {
			dups = append(dups, vk)
		}
	}
	sort.Slice(dups, func(i, j int) bool { return dups[i] < dups[j] })
	for _, vk := range dups {
		warnings = append(warnings, fmt.Sprintf("键 %s 被多个方案用作切换键（%s），同一时刻只有最后一个设置会生效",
			keys.Label(vk), strings.Join(switchUsers[vk], "、")))
	}
	if users, ok := switchUsers[p.ToggleVK]; ok && p.ToggleVK != 0 {
		warnings = append(warnings, fmt.Sprintf("键 %s 同时是启停键与方案切换键（%s），按一下会同时触发两个动作，建议分开配置",
			keys.Label(p.ToggleVK), strings.Join(users, "、")))
	}

	// 触发键与动作键重叠
	seen := map[uint16]bool{}
	for _, prof := range p.Profiles {
		for _, r := range prof.Rules {
			for _, vk := range r.Keys {
				role, isTrigger := triggerRoles[vk]
				if !isTrigger || seen[vk] {
					continue
				}
				seen[vk] = true
				warnings = append(warnings, fmt.Sprintf("键 %s 同时用作%s与动作键，会形成自我触发叠加，建议分开配置", keys.Label(vk), role))
			}
		}
	}
	return warnings
}
