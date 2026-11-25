// 铁锈战争存档 AI 编辑器 —— GOUI 版
package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/golang-gui/goui/gui"
	"github.com/golang-gui/goui/layout"
	"github.com/golang-gui/goui/theme/modern"
	"github.com/golang-gui/goui/ui"
	"github.com/golang-gui/goui/widgets"
	wui "github.com/golang-gui/goui/widgets/ui"

	"rwsave-editor/core"
)

var kinds = gui.NewSliceListModel([]widgets.DropDownItem{{Text: "人类"}, {Text: "AI"}})

// 名称列固定宽度，表头与输入框同宽才能对齐。
const nameWidth = 260

// quietLog 丢弃 goui 在 Wayland 下每次弹层都会打的无害工作区提示。
type quietLog struct{ io.Writer }

func (q quietLog) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("popup work area unsupported")) {
		return len(p), nil
	}
	return q.Writer.Write(p)
}

type app struct {
	path    ui.State[string]
	dir     ui.State[string]
	save    ui.State[*core.Save]
	targets ui.State[[]bool]
	log     ui.State[string]
	dirLog  ui.State[string]
	names   []ui.State[string]
	credits []ui.State[string]
}

func main() {
	log.SetOutput(quietLog{os.Stderr})
	a := &app{
		path:    ui.MakeState(""),
		dir:     ui.MakeState(""),
		save:    ui.MakeState[*core.Save](nil),
		targets: ui.MakeState[[]bool](nil),
		log:     ui.MakeState(""),
		dirLog:  ui.MakeState(""),
	}
	if len(os.Args) > 1 {
		a.path.Set(os.Args[1])
		a.load(os.Args[1])
	}
	err := ui.Run("com.example.rwsave", func(host ui.App) ui.RootView {
		return ui.Root().
			StyleSheet(modern.FromSettings(host.Settings())).
			Windows(
				ui.Window("main").
					Title("铁锈战争 存档 AI 编辑器（GOUI）").
					Size(780, 560).
					Content(ui.VBox(
						ui.HeaderBar(wui.TabBar("main")).Padding(4),
						wui.TabView(
							wui.TabPage("single", a.singleTab(host)).Title("单个存档"),
							wui.TabPage("batch", a.dirTab(host)).Title("批量修复目录"),
						).ID("main").MainWeight(1),
					)),
			)
	})
	if err != nil {
		log.Fatal(err)
	}
}

func (a *app) singleTab(host ui.App) ui.View {
	return ui.VBox(
		ui.HBox(
			ui.TextInput().
				BindText(&a.path).
				MainWeight(1).
				Placeholder("存档完整路径").
				OnSubmit(func() { a.load(a.path.Get()) }),
			ui.Button("…").OnClick(func() { a.browse(host, false) }),
		).Spacing(8),
		ui.ScrollView(ui.Build(func() ui.View { return a.rowsView() })).
			Direction(ui.ScrollBoth).MainWeight(1),
		ui.HBox(
			ui.Button("一键自动修复").OnClick(a.autoFix),
			ui.Button("写回存档").OnClick(a.write),
		).Spacing(8),
		ui.Label("").BindText(&a.log),
	).Spacing(12).Padding(16).CrossAlign(layout.CrossStretch)
}

func (a *app) rowsView() ui.View {
	s := a.save.Get()
	ts := a.targets.Get()
	// 表头就在滚动内容里，和行同一个容器，列宽必然对齐。
	head := ui.HBox(
		ui.Label("槽位").MinWidth(35).MaxWidth(35),
		ui.Label("玩家名称").MinWidth(nameWidth).MaxWidth(nameWidth),
		ui.Label("难度").MinWidth(60).MaxWidth(60),
		ui.Label("资金").MinWidth(90).MaxWidth(90),
		ui.Label("阵营").MinWidth(45).MaxWidth(45),
		ui.Label("控制").MinWidth(110).MaxWidth(110),
		ui.Label("").MainWeight(1),
	).Spacing(8)
	if s == nil {
		return ui.VBox(head).CrossAlign(layout.CrossStretch)
	}
	rows := []ui.View{head}
	i := 0
	for _, t := range s.Teams {
		if !t.Present {
			continue
		}
		idx := i
		sel := 0
		if idx < len(ts) && ts[idx] {
			sel = 1
		}
		rows = append(rows, ui.HBox(
			ui.Label(fmt.Sprint(t.Slot)).MinWidth(35).MaxWidth(35),
			ui.TextInput().BindText(&a.names[idx]).MinWidth(nameWidth).MaxWidth(nameWidth),
			ui.Label(diffText(t)).MinWidth(60).MaxWidth(60),
			ui.TextInput().BindText(&a.credits[idx]).MinWidth(90).MaxWidth(90),
			ui.Label(fmt.Sprint(t.Ally)).MinWidth(45).MaxWidth(45),
			wui.DropDown(kinds).Selected(sel).MinWidth(110).MaxWidth(110).
				OnSelected(func(v int) { a.setTarget(idx, v == 1) }),
			ui.Label("").MainWeight(1),
		).Spacing(8))
		i++
	}
	return ui.VBox(rows...).Spacing(6).CrossAlign(layout.CrossStretch)
}

func diffText(t core.Team) string {
	if !t.IsAI {
		return ""
	}
	return fmt.Sprint(t.Difficulty)
}

func (a *app) setTarget(i int, isAI bool) {
	ts := append([]bool(nil), a.targets.Get()...)
	ts[i] = isAI
	a.targets.Set(ts)
}

func (a *app) browse(host ui.App, dir bool) {
	opts := ui.DialogOptions{Title: "选择存档"}
	pick := func(paths []ui.FileAccess, err error) {
		if err != nil || len(paths) == 0 {
			return
		}
		p, err := paths[0].LocalPath()
		if err != nil {
			return
		}
		defer paths[0].Close()
		if dir {
			a.dir.Set(p)
			a.dirLog.Set("目录：" + p)
			return
		}
		a.path.Set(p)
		a.load(p)
	}
	if dir {
		_ = host.FileDialog().OpenDirectory("main", ui.DialogOptions{Title: "选择存档目录"}, pick)
		return
	}
	_ = host.FileDialog().OpenFile("main", opts, pick)
}

func (a *app) load(p string) {
	p = strings.Trim(strings.TrimSpace(p), `"`)
	if p == "" {
		return
	}
	data, err := os.ReadFile(p)
	if err != nil {
		a.log.Set("读取失败：" + err.Error())
		return
	}
	s, err := core.Load(data)
	if err != nil {
		a.log.Set("解析失败：" + err.Error())
		return
	}
	targets := make([]bool, 0, len(s.Teams))
	names := make([]ui.State[string], 0, len(s.Teams))
	money := make([]ui.State[string], 0, len(s.Teams))
	for _, t := range s.Teams {
		if t.Present {
			targets = append(targets, t.IsAI)
			names = append(names, ui.MakeState(t.Name))
			money = append(money, ui.MakeState(fmt.Sprint(t.Credits)))
		}
	}
	a.names, a.credits = names, money
	a.targets.Set(targets)
	a.dir.Set(filepath.Dir(p))
	a.save.Set(s)
}

func (a *app) autoFix() {
	s := a.save.Get()
	if s == nil {
		a.log.Set("请先读取存档")
		return
	}
	hits := s.AutoTargets()
	if len(hits) == 0 {
		a.log.Set("没有需要自动修复的队伍")
		return
	}
	ts := append([]bool(nil), a.targets.Get()...)
	i := 0
	for _, t := range s.Teams {
		if !t.Present {
			continue
		}
		if hits[t.Slot] {
			ts[i] = true
		}
		i++
	}
	a.targets.Set(ts)
	a.log.Set(fmt.Sprintf("已把 %d 个队伍标为 AI，检查后点「写回存档」", len(hits)))
}

func (a *app) changes() (map[int]core.Change, error) {
	s := a.save.Get()
	if s == nil {
		return nil, nil
	}
	changes := make(map[int]core.Change)
	i := 0
	for _, t := range s.Teams {
		if !t.Present {
			continue
		}
		money, err := strconv.ParseInt(strings.TrimSpace(a.credits[i].Get()), 10, 32)
		if err != nil {
			return nil, fmt.Errorf("槽位 %d 的资金必须是 32 位整数", t.Slot)
		}
		name := a.names[i].Get()
		credits, isAI := int32(money), a.targets.Get()[i]
		changes[t.Slot] = core.Change{Name: &name, Credits: &credits, IsAI: &isAI}
		i++
	}
	return changes, nil
}

func (a *app) write() {
	s := a.save.Get()
	p := strings.Trim(strings.TrimSpace(a.path.Get()), `"`)
	changes, err := a.changes()
	if err != nil {
		a.log.Set(err.Error())
		return
	}
	if s == nil {
		a.log.Set("请先读取存档")
		return
	}
	out, _, err := s.ApplyChanges(changes)
	if err != nil {
		a.log.Set("写回失败：" + err.Error())
		return
	}
	bak, err := core.WriteWithBackup(p, out)
	if err != nil {
		a.log.Set("写入失败：" + err.Error())
		return
	}
	a.load(p)
	a.log.Set("已写回；备份：" + bak)
}

func (a *app) dirTab(host ui.App) ui.View {
	return ui.VBox(
		ui.HBox(
			ui.TextInput().BindText(&a.dir).MainWeight(1).Placeholder("存档目录"),
			ui.Button("…").OnClick(func() { a.browse(host, true) }),
			ui.Button("一键修复此目录").OnClick(a.fixDir),
		).Spacing(8),
		ui.Label("").BindText(&a.dirLog),
	).Spacing(12).Padding(16).CrossAlign(layout.CrossStretch)
}

func (a *app) fixDir() {
	dir := strings.Trim(strings.TrimSpace(a.dir.Get()), `"`)
	if dir == "" {
		a.dirLog.Set("请先选择目录")
		return
	}
	preview, err := core.FixDir(dir, true)
	if err != nil {
		a.dirLog.Set("读取目录失败：" + err.Error())
		return
	}
	n := 0
	for _, r := range preview {
		if r.Status == "可修复" {
			n++
		}
	}
	if n == 0 {
		a.dirLog.Set("所有存档都无需修复")
		return
	}
	results, err := core.FixDir(dir, false)
	if err != nil {
		a.dirLog.Set("修复失败：" + err.Error())
		return
	}
	modified := 0
	for _, r := range results {
		if r.Status == "已修复" {
			modified++
		}
	}
	a.dirLog.Set(fmt.Sprintf("处理 %d 个存档，修复 %d 个。", len(results), modified))
}
