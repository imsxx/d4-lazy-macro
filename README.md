# D4 Lazy Macro · 暗黑4懒人宏

> 腱鞘炎一生之敌。*The lifelong enemy of your wrists.*

暗黑4键鼠辅助宏：只做**技能释放类**的按键自动化——连点、按住不放、单点、定时刷新。作者在工具内配好规则并导出「分享码」，玩家粘贴导入即完成配置。

## 三条红线（什么是我们不做的）

- **不读游戏内存、不注入进程、不加载驱动**：只合成键鼠输入，与雷蛇/罗技驱动宏属于同一技术类别
- **一切动作由玩家按键/按住触发**：没有无人值守循环，人始终在环
- **不做自动打怪巡航、挂机、自动拾取/分解/做装**：那是 bot 领域，本项目不做，也不接受相关贡献

## 能力规划

| 阶段 | 内容 |
| --- | --- |
| Phase 0（已完成） | 输入探针：合成键鼠输入在游戏内生效、可配置触发键按住识别、前台窗口保护（2026-10-10 实测通过） |
| Phase 1（已落地最小可用版） | 执行器：托盘常驻 + 全局热键 + 剪贴板导入分享码 + 应用前预览 + 多方案切换 + 导出分享码；图形化编辑器与自定义键位后续开放 |
| Phase 2 | 官网一页工具介绍（不承接分享码，生成与分发都在工具内完成） |

## 使用

1. 启动 `d4lazymacro.exe`，托盘出现图标。开箱默认总开关键为 `F1`（分享码里可以改）
2. 拿到一段分享码后：**托盘右键 →「从剪贴板导入分享码…」→ 核对预览 → 确认应用**
3. 进游戏，按 `F1` 启用宏，再按一次停用；**只有 `Diablo IV` 位于前台时才会发送按键**
4. 多个方案：托盘右键 →「切换方案」；作者侧：**「复制当前分享码」**即可把配置发给别人
5. 配置持久化在 `%APPDATA%\d4-lazy-macro\config.json`（托盘菜单可打开数据目录）

## Phase 0 验证结果（2026-10-10）

在真实游戏内全项通过：

- **键盘合成输入被游戏接受**：按住触发键与 F1 持续模式下，技能正常连发
- **鼠标合成输入被游戏接受**：`-spam "RMB"` 右键技能连点在游戏内生效
- **触发键轮询有效**：鼠标侧键、鼠标左右键、数字键均可作为按住触发（零全局钩子）
- **前台保护有效**：仅当 `Diablo IV.exe` 位于前台时才发送（进程名匹配）
- **权限**：游戏进程非管理员，普通权限运行即可，不存在 UIPI 拦截

## 分享码

分享码在**工具内**生成与消费：作者导出即复制，玩家从剪贴板粘贴导入。格式为 `D4LM1:` 前缀 + base64url 编码的紧凑 JSON；导入时工具会渲染一份人类可读摘要供核对，绝不执行码中的任何代码。协议见 [docs/share-code.md](docs/share-code.md)。

## 开发

```powershell
go run .          # 运行托盘执行器（开发构建带控制台，便于看日志）
go run ./cmd/probe   # 运行 Phase 0 输入探针（回归验证用）
go build ./...    # 构建
go test ./...     # 单元测试（协议、引擎、存档）
go vet ./...      # 静态检查
```

Windows 资源（Common Controls v6 清单，walk 托盘必需）由 `winres.json` 生成，产物已入库；改动后重新生成：

```powershell
go run github.com/tc-hib/go-winres@latest make --in winres.json --out rsrc --arch amd64,arm64
```

探针用法与游戏内测试清单见 [docs/probe-checklist.md](docs/probe-checklist.md)。

## 项目结构

```
main.go                     # 执行器入口：托盘常驻 + 热键 + 分享码导入/导出
cmd/probe/main.go           # Phase 0 输入探针（回归验证用）
internal/win32/             # Win32 封装：SendInput 键鼠合成、GetAsyncKeyState 轮询、前台窗口检测
internal/keys/              # 协议键名白名单与虚拟键码互转
internal/config/            # 分享码解析、编码、校验与冲突提示（Fail Fast）
internal/engine/            # 规则引擎：tap/seq/once/hold 状态机与卡键防护（纯逻辑，可测试）
internal/store/             # 本地配置持久化（%APPDATA%\d4-lazy-macro）
winres.json                 # Windows 资源定义（清单/版本），生成 rsrc_windows_*.syso
docs/                       # 协议与测试文档
```

## 免责声明

本项目为非官方开源工具，与暴雪娱乐无关。使用前请阅读并遵守游戏用户协议，使用风险自负；开发与维护者不承诺任何账号安全结果。

## License

[MIT](LICENSE)
