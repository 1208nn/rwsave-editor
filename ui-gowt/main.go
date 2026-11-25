// 铁锈战争存档 AI 编辑器 —— gowt 版
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/haiodo/gowt"

	"rwsave-editor/core"
)

// 表头与行共用列宽，保证对齐。
const (
	maxRows                                   = 10
	wSlot, wName, wDiff, wMoney, wAlly, wPick = 40, 240, 100, 100, 60, 110
)

var diffItems = []string{core.DiffNames[0], core.DiffNames[1], core.DiffNames[2],
	core.DiffNames[3], core.DiffNames[4], core.DiffNames[5]}

type row struct {
	slot    *gowt.LabelView
	name    *gowt.TextView
	diff    *gowt.ComboView
	credits *gowt.TextView
	ally    *gowt.LabelView
	pick    *gowt.ComboView
}

type app struct {
	w    *gowt.Window
	save *core.Save

	path   *gowt.TextView
	info   *gowt.LabelView
	status *gowt.LabelView
	rows   [maxRows]row

	dir     *gowt.TextView
	dirInfo *gowt.LabelView

	scroll *gowt.ScrollView
}

func main() {
	if err := gowt.Run(func(host *gowt.App) { newApp(host) }); err != nil {
		log.Fatal(err)
	}
}

func newApp(host *gowt.App) *app {
	a := &app{}
	a.w = host.Window("铁锈战争 存档 AI 编辑器（gowt）",
		// 页边距只留在页签内，窗口边缘不再套一圈。
		gowt.Padding(0),
		gowt.Tabs(
			gowt.Tab("单个存档", a.singleTab()...),
			gowt.Tab("批量修正", a.dirTab()...),
			gowt.Grow(),
		),
	)
	a.w.Show()
	a.w.SetSize(780, 560)
	a.buildRows()
	if len(os.Args) > 1 {
		a.load(os.Args[1])
	}
	return a
}

// buildRows 必须在窗口创建后调用：Ref 指向的控件此时才存在。
// 表头也放进滚动区，和行同宽，滚动条出现时不会和行错位。
func (a *app) buildRows() {
	a.scroll.Add(gowt.HBox(gowt.GrowX(),
		gowt.Label("槽位", gowt.Size(wSlot, 0)),
		gowt.Label("玩家名称", gowt.Size(wName, 0), gowt.GrowX()),
		gowt.Label("难度", gowt.Size(wDiff, 0)),
		gowt.Label("资金", gowt.Size(wMoney, 0)),
		gowt.Label("阵营", gowt.Size(wAlly, 0)),
		gowt.Label("控制", gowt.Size(wPick, 0)),
	))
	for i := range a.rows {
		r := &a.rows[i]
		a.scroll.Add(gowt.HBox(gowt.GrowX(),
			gowt.Label("", gowt.Ref(&r.slot), gowt.Size(wSlot, 0)),
			gowt.Text(gowt.Ref(&r.name), gowt.Size(wName, 0), gowt.GrowX()),
			gowt.Combo(diffItems, gowt.Ref(&r.diff), gowt.Size(wDiff, 0)),
			gowt.Text(gowt.Ref(&r.credits), gowt.Size(wMoney, 0)),
			gowt.Label("", gowt.Ref(&r.ally), gowt.Size(wAlly, 0)),
			gowt.Combo([]string{"人类", "AI"}, gowt.Ref(&r.pick), gowt.Size(wPick, 0)),
		))
		for _, w := range []gowt.Widget{r.slot, r.name, r.diff, r.credits, r.ally, r.pick} {
			w.SetVisible(false)
		}
	}
}

func (a *app) singleTab() []gowt.Option {
	opts := []gowt.Option{
		gowt.HBox(gowt.GrowX(),
			gowt.Text(gowt.Ref(&a.path), gowt.GrowX(), gowt.OnActivate(a.reload)),
			gowt.Button("…", gowt.OnClick(a.browseFile)),
		),
		gowt.Label("", gowt.Ref(&a.info)),
	}
	opts = append(opts, gowt.Scroll(gowt.Grow(), gowt.Ref(&a.scroll)))
	opts = append(opts,
		gowt.HBox(
			gowt.Button("自动识别", gowt.OnClick(a.autoFix)),
			gowt.Button("保存", gowt.OnClick(a.write)),
		),
		gowt.Label("", gowt.Ref(&a.status)),
	)
	return opts
}

func (a *app) dirTab() []gowt.Option {
	return []gowt.Option{
		gowt.HBox(gowt.GrowX(),
			gowt.Text(gowt.Ref(&a.dir), gowt.GrowX()),
			gowt.Button("…", gowt.OnClick(a.browseDir)),
			gowt.Button("预览", gowt.OnClick(a.previewDir)),
			gowt.Button("处理", gowt.OnClick(a.applyDir)),
		),
		gowt.Label("", gowt.Ref(&a.dirInfo)),
	}
}

func (a *app) dirPath() string {
	return strings.Trim(strings.TrimSpace(a.dir.Text()), `"`)
}

func (a *app) browseFile() {
	files := a.w.FileDialog(gowt.FileDialog{Title: "选择存档", Filters: []gowt.FileFilter{{Name: "铁锈战争存档", Pattern: "*.rwsave"}}})
	if len(files) > 0 {
		a.load(files[0])
	}
}

func (a *app) reload() { a.load(a.path.Text()) }

func (a *app) browseDir() {
	dir, ok := a.w.DirDialog("选择存档目录", a.dir.Text())
	if ok {
		a.dir.SetText(dir)
		a.dirInfo.SetText("")
	}
}

func (a *app) load(path string) {
	path = strings.Trim(strings.TrimSpace(path), `"`)
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		a.info.SetText("读取失败：" + err.Error())
		return
	}
	s, err := core.Load(data)
	if err != nil {
		a.info.SetText("解析失败：" + err.Error())
		return
	}
	a.save = s
	n := 0
	for _, t := range s.Teams {
		if !t.Present {
			continue
		}
		r := &a.rows[n]
		r.slot.SetText(fmt.Sprint(t.Slot))
		r.name.SetText(t.Name)
		r.diff.Select(core.InitialDifficulty(t))
		r.credits.SetText(fmt.Sprint(t.Credits))
		r.ally.SetText(fmt.Sprint(t.Ally))
		if t.IsAI {
			r.pick.Select(1)
		} else {
			r.pick.Select(0)
		}
		n++
	}
	for i := 0; i < maxRows; i++ {
		r := &a.rows[i]
		show := i < n
		for _, w := range []gowt.Widget{r.slot, r.name, r.diff, r.credits, r.ally, r.pick} {
			w.SetVisible(show)
		}
		if !show {
			r.slot.SetText("")
			r.name.SetText("")
			r.credits.SetText("")
			r.ally.SetText("")
		}
	}
	a.scroll.Fit()
	a.info.SetText(fmt.Sprintf("%s —— %d 个队伍", filepath.Base(path), n))
	a.path.SetText(path)
	a.status.SetText("")
	a.dir.SetText(filepath.Dir(path))
}

func (a *app) autoFix() {
	if a.save == nil {
		a.info.SetText("请先读取存档")
		return
	}
	hits := a.save.AutoTargets()
	if len(hits) == 0 {
		a.status.SetText("没有需要自动修复的队伍")
		return
	}
	i := 0
	for _, t := range a.save.Teams {
		if !t.Present {
			continue
		}
		if hits[t.Slot] {
			a.rows[i].pick.Select(1)
		}
		i++
	}
	a.status.SetText(fmt.Sprintf("已把 %d 个队伍标为 AI，检查后点「保存」", len(hits)))
}

func (a *app) write() {
	if a.save == nil {
		a.info.SetText("请先读取存档")
		return
	}
	changes := make(map[int]core.Change)
	changed := false
	i := 0
	for _, t := range a.save.Teams {
		if !t.Present {
			continue
		}
		name := a.rows[i].name.Text()
		money, err := strconv.ParseInt(strings.TrimSpace(a.rows[i].credits.Text()), 10, 32)
		if err != nil {
			a.status.SetText(fmt.Sprintf("槽位 %d 的资金必须是 32 位整数", t.Slot))
			return
		}
		isAI, credits := a.rows[i].pick.Index() == 1, int32(money)
		c := core.Change{Name: &name, Credits: &credits, IsAI: &isAI}
		if isAI || t.IsAI {
			d := int32(a.rows[i].diff.Index())
			c.Difficulty = &d
			changed = changed || d != t.Difficulty
		}
		changes[t.Slot] = c
		changed = changed || name != t.Name || credits != t.Credits || isAI != t.IsAI
		i++
	}
	if !changed {
		a.status.SetText("没有需要修改的内容")
		return
	}
	out, _, err := a.save.ApplyChanges(changes)
	if err != nil {
		a.status.SetText("写回失败：" + err.Error())
		return
	}
	bak, err := core.WriteWithBackup(a.path.Text(), out)
	if err != nil {
		a.status.SetText("写入失败：" + err.Error())
		return
	}
	a.load(a.path.Text())
	a.status.SetText("已写回；备份：" + bak)
}

func (a *app) previewDir() {
	dir := a.dirPath()
	if dir == "" {
		a.dirInfo.SetText("请先选择目录")
		return
	}
	preview, err := core.FixDir(dir, true)
	if err != nil {
		a.dirInfo.SetText("读取目录失败：" + err.Error())
		return
	}
	var files []string
	for _, r := range preview {
		if r.Status == "可修复" {
			files = append(files, r.File)
		}
	}
	if len(files) == 0 {
		a.dirInfo.SetText("所有存档都无需修复")
		return
	}
	a.dirInfo.SetText(fmt.Sprintf("可修复 %d 个：%s", len(files), strings.Join(files, "、")))
}

func (a *app) applyDir() {
	dir := a.dirPath()
	if dir == "" {
		a.dirInfo.SetText("请先选择目录")
		return
	}
	results, err := core.FixDir(dir, false)
	if err != nil {
		a.dirInfo.SetText("修复失败：" + err.Error())
		return
	}
	modified := 0
	for _, r := range results {
		if r.Status == "已修复" {
			modified++
		}
	}
	if modified == 0 {
		a.dirInfo.SetText("所有存档都无需修复")
	} else {
		a.dirInfo.SetText(fmt.Sprintf("处理 %d 个存档，修复 %d 个。", len(results), modified))
	}
}
