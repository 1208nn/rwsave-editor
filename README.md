# 铁锈战争 存档 AI 编辑器

把 `.rwsave` 里被存成「人类」的人机改回真正的 AI，读档后 AI 才会行动。
同一份逻辑做了三个界面版本，用来挑手感：

| 版本                       | 可执行文件                  |
| -------------------------- | --------------------------- |
| IUP（原生 Win32 控件）     | `dist/iup/rwsave-iup.exe`   |
| GOUI（自绘 Material 风格） | `dist/goui/rwsave-goui.exe` |
| gowt（SWT 移植，原生控件） | `dist/gowt/rwsave-gowt.exe` |

三者功能完全一致，都编译成单文件、无需安装运行时。
把 `.rwsave` 直接拖到 exe 上（或把路径作为第一个参数）即可自动打开。

## 用法

两个标签页：

**单个存档**

1. 粘贴存档路径（或点「…」浏览）。
2. 每行一个队伍：名字、资金、右侧下拉框（人类/AI）。
3. 「一键自动修复」：把名字像 `AI - Very Hard` 却存成人类的队伍全部改成 AI。
4. 「写回存档」：先存 `.rwsave.bak` 再写回，改完自动重新读取。

**批量修复目录**

- 选目录 →「一键修复此目录」。规则同上，每个文件先留 `.bak`，逐个报告结果。

## 规则

- 名字形如 `AI`、`AI - xxx`、`AI-xxx` 视为人机。
- 已经是真实 AI（带基地/作战小组数据）的队伍不会被降级：把这类数据从存档里剥离会损坏存档。

## 原理

联机（RWPP 等）客户端保存的存档，会把主机上跑的人机记录成普通人类队伍，并且不写 AI 的运行数据。
本工具把队伍类型改回 AI，并补写一份最小的 AI 运行状态（0 个基地/小组 + 需要首次初始化），
游戏读档后会自动用自己的指挥中心重建基地、重新开始生产。

注意：**必须从单人的「载入游戏」进入**，不要在联机大厅里继续，否则游戏仍按联机客户端模式运行。

## 源码

```
core/        存档解析与改写（纯 Go，无 UI 依赖）
ui-iup/      IUP 界面
ui-goui/     GOUI 界面
ui-gowt/     gowt 界面
```

构建（都不需要 gcc）：

```sh
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H=windowsgui" -o dist/iup/rwsave-iup.exe  ./ui-iup
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H=windowsgui" -o dist/goui/rwsave-goui.exe ./ui-goui
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H=windowsgui" -o dist/gowt/rwsave-gowt.exe ./ui-gowt
```

交叉编译 Linux：`GOOS=linux GOARCH=amd64 go build ...`（同样不需要 gcc）。
