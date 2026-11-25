// 铁锈战争存档 AI 编辑器 —— IUP 版
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gen2brain/iup-go/iup"

	"rwsave-editor/core"
)

// 表头与行共用列宽规则，保证对齐。宽度 0 表示自适应，高度统一 22。
var columns = []struct{ title, width string }{
	{"槽位", "40x22"}, {"玩家名称", "0x22"}, {"难度", "100x22"},
	{"资金", "90x22"}, {"阵营", "50x22"}, {"控制", "90x22"},
}

// applyCol 统一设置列宽：固定高度，宽度为 0 的列横向伸缩。
func applyCol(h iup.Ihandle, width string) {
	h.SetAttribute("RASTERSIZE", width)
	if strings.HasPrefix(width, "0x") {
		h.SetAttribute("EXPAND", "HORIZONTAL")
	}
}

type row struct {
	slot      int
	box       iup.Ihandle
	slotLabel iup.Ihandle
	name      iup.Ihandle
	diff      iup.Ihandle
	credits   iup.Ihandle
	ally      iup.Ihandle
	pick      iup.Ihandle
}

type app struct {
	save *core.Save
	path string

	pathEdit    iup.Ihandle
	info        iup.Ihandle
	tab         iup.Ihandle
	anchor      iup.Ihandle
	list        iup.Ihandle // 表头 + 队伍行
	placeholder iup.Ihandle
	rows        []*row

	dir     string
	dirEdit iup.Ihandle
	dirInfo iup.Ihandle
}

func main() {
	iup.Open()
	defer iup.Close()
	iup.SetGlobal("UTF8MODE", "YES")
	iup.SetGlobal("UTF8MODE_FILE", "YES")

	a := &app{}
	tabs := iup.Tabs(a.singleTab(), a.dirTab())
	// 页签控件本身必须跟着对话框伸缩，否则页面和滚动区都会卡在初始尺寸。
	tabs.SetAttribute("EXPAND", "YES")
	dlg := iup.Dialog(tabs)
	dlg.SetAttribute("TITLE", "铁锈战争 存档 AI 编辑器（IUP）")
	dlg.SetAttribute("MARGIN", "8x8")
	dlg.SetAttribute("RASTERSIZE", "780x560")
	dlg.SetAttribute("MINSIZE", "380x300")

	iup.Show(dlg)
	if len(os.Args) > 1 {
		a.load(os.Args[1])
	}
	iup.MainLoop()
}

func (a *app) singleTab() iup.Ihandle {
	a.info = iup.Label("")
	a.info.SetAttribute("EXPAND", "HORIZONTAL")

	a.placeholder = iup.Label("尚未读取存档")

	var header []iup.Ihandle
	for _, c := range columns {
		l := iup.Label(c.title)
		l.SetAttribute("ALIGNMENT", "ALEFT")
		applyCol(l, c.width)
		header = append(header, l)
	}
	headRow := iup.Hbox(header...)
	headRow.SetAttribute("EXPAND", "HORIZONTAL")

	a.list = iup.Vbox(headRow, a.placeholder)
	a.list.SetAttribute("GAP", "2")

	// 原生滚动容器：窗口小就出滚动条，窗口大就全部显示，行本身不伸缩。
	scroll := iup.ScrollBox(a.list)
	scroll.SetAttribute("EXPAND", "YES")

	a.anchor = iup.Hbox(
		iup.On(iup.Button("自动识别"), iup.ActionCB, func(iup.Ihandle) int { a.autoFix(); return iup.DEFAULT }),
		iup.On(iup.Button("保存"), iup.ActionCB, func(iup.Ihandle) int { a.write(); return iup.DEFAULT }),
	)
	a.anchor.SetAttribute("GAP", "6")
	a.anchor.SetAttribute("EXPAND", "HORIZONTAL")

	a.pathEdit = iup.Text()
	a.pathEdit.SetAttribute("EXPAND", "HORIZONTAL")
	a.pathEdit.SetCallback("K_ANY", iup.KAnyFunc(func(_ iup.Ihandle, c int) int {
		if c == iup.K_CR {
			a.load(a.pathEdit.GetAttribute("VALUE"))
		}
		return iup.DEFAULT
	}))

	pathRow := iup.Hbox(
		a.pathEdit,
		iup.On(iup.Button("…"), iup.ActionCB, func(iup.Ihandle) int { a.pickFile(); return iup.DEFAULT }),
	)
	pathRow.SetAttribute("EXPAND", "HORIZONTAL")
	pathRow.SetAttribute("ALIGNMENT", "ACENTER")
	pathRow.SetAttribute("GAP", "6")

	a.tab = iup.Vbox(
		pathRow,
		scroll,
		a.info,
		a.anchor,
	)
	a.tab.SetAttribute("GAP", "6")
	a.tab.SetAttribute("EXPAND", "YES")
	a.tab.SetAttribute("TABTITLE", "单个存档")
	return a.tab
}

func (a *app) dirTab() iup.Ihandle {
	a.dirInfo = iup.Label("")
	a.dirInfo.SetAttribute("EXPAND", "HORIZONTAL")
	a.dirEdit = iup.Text()
	a.dirEdit.SetAttribute("EXPAND", "HORIZONTAL")

	dirRow := iup.Hbox(
		a.dirEdit,
		iup.On(iup.Button("…"), iup.ActionCB, func(iup.Ihandle) int { a.pickDir(); return iup.DEFAULT }),
		iup.On(iup.Button("预览"), iup.ActionCB, func(iup.Ihandle) int { a.previewDir(); return iup.DEFAULT }),
		iup.On(iup.Button("处理"), iup.ActionCB, func(iup.Ihandle) int { a.applyDir(); return iup.DEFAULT }),
	)
	dirRow.SetAttribute("EXPAND", "HORIZONTAL")
	dirRow.SetAttribute("GAP", "6")

	tab := iup.Vbox(
		dirRow,
		a.dirInfo,
	)
	tab.SetAttribute("GAP", "6")
	tab.SetAttribute("EXPAND", "YES")
	tab.SetAttribute("TABTITLE", "批量修正")
	return tab
}

func (a *app) pickFile() {
	d := iup.FileDlg()
	d.SetAttribute("DIALOGTYPE", "OPEN")
	d.SetAttribute("TITLE", "选择存档")
	d.SetAttribute("EXTFILTER", "铁锈战争存档|*.rwsave|所有文件|*.*|")
	iup.Popup(d, iup.CENTER, iup.CENTER)
	p := d.GetAttribute("VALUE")
	iup.Destroy(d)
	if p != "" {
		a.load(p)
	}
}

func (a *app) pickDir() {
	d := iup.FileDlg()
	d.SetAttribute("DIALOGTYPE", "DIR")
	d.SetAttribute("TITLE", "选择存档目录")
	d.SetAttribute("DIRECTORY", a.dir)
	iup.Popup(d, iup.CENTER, iup.CENTER)
	p := d.GetAttribute("VALUE")
	iup.Destroy(d)
	if p == "" {
		return
	}
	a.dir = strings.TrimRight(p, `\/`)
	a.dirEdit.SetAttribute("VALUE", a.dir)
}

func (a *app) load(path string) {
	path = strings.Trim(strings.TrimSpace(path), `"`)
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		a.info.SetAttribute("TITLE", "读取失败："+err.Error())
		return
	}
	s, err := core.Load(data)
	if err != nil {
		a.info.SetAttribute("TITLE", "解析失败："+err.Error())
		return
	}
	a.save, a.path = s, path

	for _, r := range a.rows {
		iup.Destroy(r.box)
	}
	a.rows = nil
	if a.placeholder != 0 {
		iup.Destroy(a.placeholder)
		a.placeholder = 0
	}
	for _, t := range s.Teams {
		if !t.Present {
			continue
		}
		r := &row{slot: t.Slot}
		r.slotLabel = iup.Label(fmt.Sprint(t.Slot))
		r.name, r.credits, r.ally = iup.Text(), iup.Text(), iup.Label(fmt.Sprint(t.Ally))
		r.name.SetAttribute("VALUE", t.Name)
		r.credits.SetAttribute("VALUE", fmt.Sprint(t.Credits))
		r.ally.SetAttribute("ALIGNMENT", "ALEFT")
		r.diff = diffDropdown(int(initialDiff(t)))
		r.pick = iup.List()
		r.pick.SetAttribute("1", "人类")
		r.pick.SetAttribute("2", "AI")
		r.pick.SetAttribute("DROPDOWN", "YES")
		r.pick.SetAttribute("VALUE", pickValue(t.IsAI))
		for i, h := range []iup.Ihandle{r.slotLabel, r.name, r.diff, r.credits, r.ally, r.pick} {
			applyCol(h, columns[i].width)
		}
		r.box = iup.Hbox(r.slotLabel, r.name, r.diff, r.credits, r.ally, r.pick)
		r.box.SetAttribute("ALIGNMENT", "ACENTER")
		r.box.SetAttribute("EXPAND", "HORIZONTAL")
		iup.Append(a.list, r.box)
		iup.Map(r.box)
		a.rows = append(a.rows, r)
	}
	if len(a.rows) == 0 {
		a.placeholder = iup.Label("这个存档里没有队伍")
		iup.Append(a.list, a.placeholder)
		iup.Map(a.placeholder)
	}
	iup.Refresh(a.tab)
	a.info.SetAttribute("TITLE", fmt.Sprintf("%s —— %d 个队伍", filepath.Base(path), len(a.rows)))
	a.pathEdit.SetAttribute("VALUE", path)
	a.dir = filepath.Dir(path)
	a.dirEdit.SetAttribute("VALUE", a.dir)
}

func (a *app) teams() []core.Team {
	var out []core.Team
	for _, t := range a.save.Teams {
		if t.Present {
			out = append(out, t)
		}
	}
	return out
}

func (a *app) autoFix() {
	if a.save == nil {
		a.info.SetAttribute("TITLE", "请先读取存档")
		return
	}
	hits := a.save.AutoTargets()
	if len(hits) == 0 {
		a.info.SetAttribute("TITLE", "没有需要自动修复的队伍")
		return
	}
	for i, t := range a.teams() {
		if hits[t.Slot] {
			a.rows[i].pick.SetAttribute("VALUE", "2")
		}
	}
	a.info.SetAttribute("TITLE", fmt.Sprintf("已把 %d 个队伍标为 AI，检查后点「保存」", len(hits)))
}

func (a *app) write() {
	if a.save == nil {
		a.info.SetAttribute("TITLE", "请先读取存档")
		return
	}
	changes := make(map[int]core.Change, len(a.rows))
	changed := false
	present := a.teams()
	for i, r := range a.rows {
		original := present[i]
		name := r.name.GetAttribute("VALUE")
		credit, err := strconv.ParseInt(strings.TrimSpace(r.credits.GetAttribute("VALUE")), 10, 32)
		if err != nil {
			iup.Message("资金无效", "槽位 "+strconv.Itoa(r.slot)+" 的资金必须是 32 位整数。")
			return
		}
		isAI, credits := r.pick.GetAttribute("VALUE") == "2", int32(credit)
		c := core.Change{Name: &name, Credits: &credits, IsAI: &isAI}
		if isAI || original.IsAI {
			d, _ := strconv.Atoi(r.diff.GetAttribute("VALUE"))
			v := int32(d - 1)
			c.Difficulty = &v
			changed = changed || v != original.Difficulty
		}
		changes[r.slot] = c
		changed = changed || name != original.Name || credits != original.Credits || isAI != original.IsAI
	}
	if !changed {
		a.info.SetAttribute("TITLE", "没有需要修改的内容")
		return
	}
	out, _, err := a.save.ApplyChanges(changes)
	if err != nil {
		iup.Message("写回失败", err.Error())
		return
	}
	bak, err := core.WriteWithBackup(a.path, out)
	if err != nil {
		iup.Message("写回失败", err.Error())
		return
	}
	a.load(a.path)
	a.info.SetAttribute("TITLE", "已写回；备份："+bak)
}

func (a *app) dirPath() string {
	return strings.Trim(strings.TrimSpace(a.dirEdit.GetAttribute("VALUE")), `"`)
}

func (a *app) previewDir() {
	dir := a.dirPath()
	if dir == "" {
		a.dirInfo.SetAttribute("TITLE", "请先选择目录")
		return
	}
	a.dir = strings.TrimRight(dir, `\/`)
	preview, err := core.FixDir(dir, true)
	if err != nil {
		a.dirInfo.SetAttribute("TITLE", "读取目录失败："+err.Error())
		return
	}
	var files []string
	for _, r := range preview {
		if r.Status == "可修复" {
			files = append(files, r.File)
		}
	}
	if len(files) == 0 {
		a.dirInfo.SetAttribute("TITLE", "所有存档都无需修复")
		return
	}
	a.dirInfo.SetAttribute("TITLE", fmt.Sprintf("可修复 %d 个：%s", len(files), strings.Join(files, "、")))
}

func (a *app) applyDir() {
	dir := a.dirPath()
	if dir == "" {
		a.dirInfo.SetAttribute("TITLE", "请先选择目录")
		return
	}
	a.dir = strings.TrimRight(dir, `\/`)
	results, err := core.FixDir(dir, false)
	if err != nil {
		a.dirInfo.SetAttribute("TITLE", "修复失败："+err.Error())
		return
	}
	modified := 0
	for _, r := range results {
		if r.Status == "已修复" {
			modified++
		}
	}
	if modified == 0 {
		a.dirInfo.SetAttribute("TITLE", "所有存档都无需修复")
	} else {
		if modified == 0 {
			a.dirInfo.SetAttribute("TITLE", "所有存档都无需修复")
		} else {
			a.dirInfo.SetAttribute("TITLE", fmt.Sprintf("处理 %d 个存档，修复 %d 个。", len(results), modified))
		}
	}
}

// initialDiff 计算难度下拉框的初始选中项：队名带难度词用检测值，
// 否则 AI 用存档里的值，人类默认「中等」。
func initialDiff(t core.Team) int {
	if d := core.DifficultyFromName(t.Name); d >= 0 {
		return int(d)
	}
	if t.IsAI && t.Difficulty >= 0 && t.Difficulty < int32(len(core.DiffNames)) {
		return int(t.Difficulty)
	}
	return 2
}

// diffDropdown 建一个难度下拉框，VALUE 是 1 起始的选项序号。
func diffDropdown(sel int) iup.Ihandle {
	d := iup.List()
	d.SetAttribute("DROPDOWN", "YES")
	for i, n := range core.DiffNames {
		d.SetAttribute(strconv.Itoa(i+1), n)
	}
	d.SetAttribute("VALUE", strconv.Itoa(sel+1))
	return d
}

func pickValue(isAI bool) string {
	if isAI {
		return "2"
	}
	return "1"
}
