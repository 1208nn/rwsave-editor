package core

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	markValue = 12345
	// writeTeamState 的定长部分（不含名字、不含 AI 状态块）。
	teamStateHeader = 56
	// 本工具写入的空 AI 状态块长度。
	emptyAIStateLen = 81
)

var errShort = errors.New("存档数据不完整（可能是不支持的版本）")

// Team 是存档中的一个队伍槽位。
type Team struct {
	Slot       int
	Present    bool
	Credits    int32
	Ally       int32
	Name       string
	IsAI       bool // 存档记录的队伍类型：读档时游戏据此创建 AI 或普通队伍
	IsStd      bool // StandardPlayer（联机里的其他真人）
	Difficulty int32
	Eliminated bool

	recStart   int // 第一段：3 个类型布尔 + 队伍状态
	stateStart int // 第二段：Team.write 的起点
	aiStateLen int // 若是本工具写入的空 AI 状态块，则为 emptyAIStateLen，否则 0
}

// CanSetHuman 说明能否安全改回人类：真实 AI 运行数据无法在不破坏存档的前提下剥离。
func (t Team) CanSetHuman() bool { return !t.IsAI || t.aiStateLen > 0 }

// Kind 返回给界面显示的队伍类型。
func (t Team) Kind() string {
	switch {
	case t.IsAI:
		return "AI"
	case t.IsStd:
		return "联机玩家"
	}
	return "人类"
}

// LooksLikeAI 判断队伍名是否像游戏自带的人机，例如 "AI - Very Hard"。
func LooksLikeAI(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return n == "ai" || strings.HasPrefix(n, "ai ") || strings.HasPrefix(n, "ai-") || strings.HasPrefix(n, "ai_")
}

// AutoTargets 给出一键修复的目标：名字像人机、却被存成人类的队伍。
func (s *Save) AutoTargets() map[int]bool {
	out := map[int]bool{}
	for _, t := range s.Teams {
		if t.Present && !t.IsAI && LooksLikeAI(t.Name) {
			out[t.Slot] = true
		}
	}
	return out
}

// Pending 列出与当前存档相比将要发生的改动，用于写回前的确认。
func (s *Save) Pending(desired map[int]bool) []string {
	var out []string
	for _, t := range s.Teams {
		want, ok := desired[t.Slot]
		if !ok || !t.Present || want == t.IsAI {
			continue
		}
		from, to := t.Kind(), "人类"
		if want {
			to = "AI"
		}
		out = append(out, fmt.Sprintf("%s：%s → %s", t.Name, from, to))
	}
	return out
}

type Save struct {
	head    []byte // 压缩长度字段之前的所有字节
	tail    []byte // gzip 之后的 writeMark + "<SAVE END>"
	buf     []byte // 解压后的内容
	SaveVer int
	GameVer int
	Player  int
	Teams   []Team
}

// ---- 大端二进制读取 ----

type rd struct {
	b []byte
	p int
}

func (r *rd) need(n int) error {
	if n < 0 || r.p+n > len(r.b) {
		return errShort
	}
	return nil
}

func (r *rd) u8() (byte, error) {
	if err := r.need(1); err != nil {
		return 0, err
	}
	r.p++
	return r.b[r.p-1], nil
}

func (r *rd) boolean() (bool, error) {
	v, err := r.u8()
	return v != 0, err
}

func (r *rd) i16() (int16, error) {
	if err := r.need(2); err != nil {
		return 0, err
	}
	r.p += 2
	return int16(binary.BigEndian.Uint16(r.b[r.p-2:])), nil
}

func (r *rd) i32() (int32, error) {
	if err := r.need(4); err != nil {
		return 0, err
	}
	r.p += 4
	return int32(binary.BigEndian.Uint32(r.b[r.p-4:])), nil
}

func (r *rd) i64() (int64, error) {
	if err := r.need(8); err != nil {
		return 0, err
	}
	r.p += 8
	return int64(binary.BigEndian.Uint64(r.b[r.p-8:])), nil
}

func (r *rd) f32() (float32, error) {
	if err := r.need(4); err != nil {
		return 0, err
	}
	r.p += 4
	return math.Float32frombits(binary.BigEndian.Uint32(r.b[r.p-4:])), nil
}

func (r *rd) utf() (string, error) {
	n, err := r.i16()
	if err != nil {
		return "", err
	}
	if n < 0 || r.need(int(n)) != nil {
		return "", errShort
	}
	end := r.p + int(n)
	units := make([]uint16, 0, n)
	for r.p < end {
		b := r.b[r.p]
		r.p++
		switch {
		case b&0x80 == 0:
			units = append(units, uint16(b))
		case b&0xe0 == 0xc0:
			if r.p >= end || r.b[r.p]&0xc0 != 0x80 {
				return "", errors.New("无效的 modified UTF-8")
			}
			units = append(units, uint16(b&0x1f)<<6|uint16(r.b[r.p]&0x3f))
			r.p++
		case b&0xf0 == 0xe0:
			if r.p+1 >= end || r.b[r.p]&0xc0 != 0x80 || r.b[r.p+1]&0xc0 != 0x80 {
				return "", errors.New("无效的 modified UTF-8")
			}
			units = append(units, uint16(b&0x0f)<<12|uint16(r.b[r.p]&0x3f)<<6|uint16(r.b[r.p+1]&0x3f))
			r.p += 2
		default:
			return "", errors.New("无效的 modified UTF-8")
		}
	}
	return string(utf16.Decode(units)), nil
}

func (r *rd) nulUTF() (string, error) {
	if has, err := r.boolean(); err != nil || !has {
		return "", err
	}
	return r.utf()
}

func (r *rd) nulI32() error {
	has, err := r.boolean()
	if err != nil || !has {
		return err
	}
	_, err = r.i32()
	return err
}

// ---- 加载 ----

func Load(data []byte) (*Save, error) {
	r := &rd{b: data}
	if hdr, err := r.utf(); err != nil || hdr != "rustedWarfareSave" {
		return nil, errors.New("不是铁锈战争存档（文件头不匹配）")
	}
	gameVer, err := r.i32()
	if err != nil {
		return nil, err
	}
	saveVer, err := r.i32()
	if err != nil {
		return nil, err
	}
	if saveVer >= 5 {
		if _, err := r.boolean(); err != nil { // demoVersion
			return nil, err
		}
	}
	if saveVer < 23 {
		return nil, fmt.Errorf("存档格式过旧（v%d），不支持", saveVer)
	}
	if name, err := r.utf(); err != nil || name != "saveCompression" {
		return nil, errors.New("存档未使用压缩块（可能是不支持的旧版本）")
	}
	head := data[:r.p]
	compLen, err := r.i32()
	if err != nil {
		return nil, err
	}
	if compLen <= 0 || r.p+int(compLen) > len(data) {
		return nil, errors.New("压缩块长度异常")
	}
	zr, err := gzip.NewReader(bytes.NewReader(data[r.p : r.p+int(compLen)]))
	if err != nil {
		return nil, fmt.Errorf("解压失败: %w", err)
	}
	buf, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("解压失败: %w", err)
	}

	s := &Save{
		head:    head,
		tail:    data[r.p+int(compLen):],
		buf:     buf,
		SaveVer: int(saveVer),
		GameVer: int(gameVer),
	}
	return s, s.parseTeams()
}

// parseTeams 用结构校验扫描定位队伍段，避免解析触发器、统计等变长区块。
func (s *Save) parseTeams() error {
	buf := s.buf
	for i := 0; i+10 < len(buf); i++ {
		if buf[i] != 0x30 || buf[i+1] != 0x39 { // writeMark = 12345
			continue
		}
		r := &rd{b: buf, p: i + 2}
		player, err := r.i32()
		if err != nil {
			continue
		}
		maxTeam, err := r.i32()
		if err != nil || maxTeam < 1 || maxTeam > 64 || player < -3 || player > 64 {
			continue
		}
		teams, end, ok := parseTeamList(buf, r.p, int(maxTeam))
		if !ok || end+4 > len(buf) {
			continue
		}
		if count := int32(binary.BigEndian.Uint32(buf[end:])); count < 0 || count > 50_000_000 {
			continue
		}
		s.Player, s.Teams = int(player), teams
		return s.locateStates(end)
	}
	return errors.New("找不到队伍信息（可能不是标准存档）")
}

func parseTeamList(buf []byte, start, maxTeam int) ([]Team, int, bool) {
	r := &rd{b: buf, p: start}
	teams := make([]Team, maxTeam)
	named := 0
	for slot := 0; slot < maxTeam; slot++ {
		if err := r.need(3); err != nil {
			return nil, 0, false
		}
		rec := r.p
		if buf[rec] > 1 || buf[rec+1] > 1 || buf[rec+2] > 1 {
			return nil, 0, false
		}
		t := Team{Slot: slot, Present: buf[rec+2] == 1, IsAI: buf[rec] == 1, IsStd: buf[rec+1] == 1, recStart: rec}
		r.p += 3
		teams[slot] = t
		if !t.Present {
			continue
		}
		if id, err := r.u8(); err != nil || int(id) != slot {
			return nil, 0, false
		}
		credits, err := r.i32()
		if err != nil {
			return nil, 0, false
		}
		ally, err := r.i32()
		if err != nil {
			return nil, 0, false
		}
		name, err := r.nulUTF()
		if err != nil || len(name) > 64 || !utf8.ValidString(name) {
			return nil, 0, false
		}
		t.Credits, t.Ally, t.Name = credits, ally, name
		if t.Name != "" {
			named++
		}
		if !r.skipFiller(&t.Difficulty) {
			return nil, 0, false
		}
		teams[slot] = t
	}
	if named == 0 {
		return nil, 0, false
	}
	return teams, r.p, true
}

// skipFiller 跳过一个队伍记录中与编辑无关的字段，并把 AI 难度读进 difficulty。
func (r *rd) skipFiller(difficulty *int32) bool {
	var err error
	// PENDING_flag_U, ping, lastPingTime, isAI
	if _, err = r.boolean(); err != nil {
		return false
	}
	if _, err = r.i32(); err != nil {
		return false
	}
	if _, err = r.i64(); err != nil {
		return false
	}
	if _, err = r.boolean(); err != nil {
		return false
	}
	if *difficulty, err = r.i32(); err != nil { // aiDifficulty
		return false
	}
	// sortOrder, 保留字节, sharedControlEnabled, controlShared, eliminated,
	// surrenderVoted, surrenderVoteTime, clientId2, localPlayerTeamFlag,
	// 4 个可空 int, displaySlot
	if _, err = r.i32(); err != nil {
		return false
	}
	if _, err = r.u8(); err != nil {
		return false
	}
	for i := 0; i < 2; i++ {
		if _, err = r.boolean(); err != nil {
			return false
		}
	}
	if _, err = r.boolean(); err != nil {
		return false
	}
	if _, err = r.boolean(); err != nil {
		return false
	}
	if _, err = r.i32(); err != nil {
		return false
	}
	if _, err = r.nulUTF(); err != nil {
		return false
	}
	if _, err = r.i32(); err != nil {
		return false
	}
	for i := 0; i < 4; i++ {
		if err = r.nulI32(); err != nil {
			return false
		}
	}
	_, err = r.i32()
	return err == nil
}

// locateStates 定位第二段每队 Team.write 的起点，并识别本工具写入的空 AI 状态块。
func (s *Save) locateStates(from int) error {
	buf := s.buf
	for i := range s.Teams {
		t := &s.Teams[i]
		if !t.Present {
			continue
		}
		tslen := teamStateHeader + len(t.Name)
		p := -1
		for q := from; q+tslen+5 <= len(buf); q++ {
			if isTeamStateAt(buf, q, t) {
				p = q
				break
			}
		}
		if p < 0 {
			return fmt.Errorf("定位队伍 %q 的第二段数据失败", t.Name)
		}
		t.stateStart, from = p, p+tslen+1
		if t.IsAI && p >= emptyAIStateLen && bytes.Equal(buf[p-emptyAIStateLen:p], canonicalAI(t.Slot)) {
			t.aiStateLen = emptyAIStateLen
		}
	}
	return nil
}

// isTeamStateAt 判断 p 处是否为 writeTeamState 开头：teamId/资金/阵营/名字/后续标志全部吻合。
func isTeamStateAt(buf []byte, p int, t *Team) bool {
	if p < 0 || p+teamStateHeader+len(t.Name)+5 > len(buf) {
		return false
	}
	if buf[p] != byte(t.Slot) ||
		int32(binary.BigEndian.Uint32(buf[p+1:])) != t.Credits ||
		int32(binary.BigEndian.Uint32(buf[p+5:])) != t.Ally ||
		buf[p+9] != 1 ||
		int(binary.BigEndian.Uint16(buf[p+10:])) != len(t.Name) ||
		string(buf[p+12:p+12+len(t.Name)]) != t.Name {
		return false
	}
	after := p + teamStateHeader + len(t.Name) // 紧跟 writeByte(4)
	return buf[after] == 4 &&
		buf[after+1] <= 1 && buf[after+2] <= 1 && buf[after+3] <= 1 && buf[after+4] <= 1
}

// canonicalAI 生成一份空 AI 状态：无基地、无小组、需要首次初始化。
// 游戏读档后会用指挥中心自动重建基地并重新开始生产。
func canonicalAI(teamID int) []byte {
	b := make([]byte, 0, emptyAIStateLen)
	b = append(b, 0) // PENDING_aK
	b = append(b, f32(100+float32(teamID)*9)...)
	b = append(b, f32(0)...)
	b = append(b, f32(202+float32(teamID)*19)...)
	b = append(b, f32(0)...)
	b = append(b, f32(3500+float32(teamID)*5)...)
	b = append(b, i32(0)...) // rushWaveCounter
	b = append(b, f32(4200+float32(teamID)*5)...)
	b = append(b, 0)         // aiUpdatePaused = false
	b = append(b, i32(0)...) // attackingCount
	b = append(b, i32(0)...) // zones
	b = append(b, 9)         // 状态格式版本
	b = append(b, i32(0)...) // zoneIdCounter
	b = append(b, 1, 1, 1, 1, 1)
	b = append(b, f32(7500+float32(teamID)*5)...)
	b = append(b, i32(0)...) // transportsBuiltCount
	b = append(b, i32(0)...) // attackGroupsCreated
	b = append(b, i32(0)...) // defenseGroupsCreated
	b = append(b, i32(0)...) // outpostBasesCreated
	b = append(b, 0)         // aggressiveMode
	b = append(b, i32(0)...) // aiStateFlags
	b = append(b, 0x30, 0x39)
	b = append(b, i32(0)...) // components
	b = append(b, 0x30, 0x39)
	return b
}

func i32(v int32) []byte { return binary.BigEndian.AppendUint32(nil, uint32(v)) }

func f32(v float32) []byte { return i32(int32(math.Float32bits(v))) }

// Change 描述一个队伍槽位的可写字段。
type Change struct {
	IsAI    *bool
	Name    *string
	Credits *int32
}

// ApplyChanges 按槽位修改队伍类型、名称和资金。
func (s *Save) ApplyChanges(changes map[int]Change) ([]byte, []string, error) {
	var logs []string
	var edits []edit

	for i := range s.Teams {
		t := &s.Teams[i]
		c, ok := changes[t.Slot]
		if !ok || !t.Present {
			continue
		}
		if c.IsAI != nil && *c.IsAI != t.IsAI {
			if !*c.IsAI && t.aiStateLen == 0 {
				return nil, logs, fmt.Errorf("%s 带有真实 AI 运行数据，无法安全转回人类", t.Name)
			}
			flag := byte(0)
			if *c.IsAI {
				flag = 1
				edits = append(edits, edit{at: t.stateStart, insert: canonicalAI(t.Slot)})
				logs = append(logs, t.Name+"：人类 → AI")
			} else {
				edits = append(edits, edit{at: t.stateStart - t.aiStateLen, remove: t.aiStateLen})
				logs = append(logs, t.Name+"：AI → 人类")
			}
			stdFlag := byte(0)
			if !*c.IsAI && t.IsStd {
				stdFlag = 1
			}
			edits = append(edits,
				edit{at: t.recStart, remove: 1, insert: []byte{flag}},
				edit{at: t.recStart + 1, remove: 1, insert: []byte{stdFlag}},
			)
		}
		if c.Credits != nil && *c.Credits != t.Credits {
			v := i32(*c.Credits)
			edits = append(edits, edit{at: t.recStart + 4, remove: 4, insert: v}, edit{at: t.stateStart + 1, remove: 4, insert: v})
			logs = append(logs, fmt.Sprintf("%s：资金 %d → %d", t.Name, t.Credits, *c.Credits))
		}
		if c.Name != nil && *c.Name != t.Name {
			name, err := nullableUTF(*c.Name)
			if err != nil {
				return nil, logs, fmt.Errorf("玩家名称无效：%w", err)
			}
			oldLen := 3 + modifiedUTFLen(t.Name) // nullable marker + UTF length + bytes
			edits = append(edits,
				edit{at: t.recStart + 12, remove: oldLen, insert: name},
				edit{at: t.stateStart + 9, remove: oldLen, insert: name},
			)
			logs = append(logs, fmt.Sprintf("名称：%s → %s", t.Name, *c.Name))
		}
	}

	sort.Slice(edits, func(i, j int) bool { return edits[i].at > edits[j].at })
	buf := append([]byte(nil), s.buf...)
	for _, e := range edits {
		buf = splice(buf, e)
	}
	out, err := s.pack(buf)
	if err != nil {
		return nil, logs, err
	}
	if err := s.verifyChanges(out, changes); err != nil {
		return out, logs, fmt.Errorf("自检失败，未写入：%w", err)
	}
	return out, logs, nil
}

func modifiedUTFLen(s string) int { b, _ := modifiedUTF(s); return len(b) }

func nullableUTF(s string) ([]byte, error) {
	b, err := modifiedUTF(s)
	if err != nil {
		return nil, err
	}
	if len(b) > 64 || len(b) > 65535 {
		return nil, errors.New("名称太长（最多 64 字节）")
	}
	out := []byte{1, byte(len(b) >> 8), byte(len(b))}
	return append(out, b...), nil
}

func modifiedUTF(s string) ([]byte, error) {
	if !utf8.ValidString(s) {
		return nil, errors.New("不是有效的 UTF-8")
	}
	var out []byte
	for _, u := range utf16.Encode([]rune(s)) {
		switch {
		case u >= 1 && u <= 0x7f:
			out = append(out, byte(u))
		case u <= 0x7ff:
			out = append(out, byte(0xc0|u>>6), byte(0x80|u&0x3f))
		default:
			out = append(out, byte(0xe0|u>>12), byte(0x80|(u>>6)&0x3f), byte(0x80|u&0x3f))
		}
	}
	return out, nil
}

func (s *Save) verifyChanges(data []byte, changes map[int]Change) error {
	got, err := Load(data)
	if err != nil {
		return err
	}
	for _, t := range got.Teams {
		c, ok := changes[t.Slot]
		if !ok || !t.Present {
			continue
		}
		if c.IsAI != nil && t.IsAI != *c.IsAI {
			return fmt.Errorf("队伍 %d 的类型未生效", t.Slot)
		}
		if c.Name != nil && t.Name != *c.Name {
			return fmt.Errorf("队伍 %d 的名称未生效", t.Slot)
		}
		if c.Credits != nil && t.Credits != *c.Credits {
			return fmt.Errorf("队伍 %d 的资金未生效", t.Slot)
		}
	}
	return nil
}

func changesToTargets(changes map[int]Change) map[int]bool {
	out := make(map[int]bool)
	for slot, c := range changes {
		if c.IsAI != nil {
			out[slot] = *c.IsAI
		}
	}
	return out
}

// Apply 保持旧接口，供自动修复和现有测试使用。
func (s *Save) Apply(desired map[int]bool) ([]byte, []string, error) {
	changes := make(map[int]Change, len(desired))
	for slot, isAI := range desired {
		v := isAI
		changes[slot] = Change{IsAI: &v}
	}
	return s.ApplyChanges(changes)
}

type edit struct {
	at     int
	remove int
	insert []byte
}

func splice(buf []byte, e edit) []byte {
	out := make([]byte, 0, len(buf)-e.remove+len(e.insert))
	out = append(out, buf[:e.at]...)
	out = append(out, e.insert...)
	return append(out, buf[e.at+e.remove:]...)
}

func (s *Save) pack(buf []byte) ([]byte, error) {
	var gz bytes.Buffer
	zw, err := gzip.NewWriterLevel(&gz, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := zw.Write(buf); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	out := append([]byte(nil), s.head...)
	out = append(out, i32(int32(gz.Len()))...)
	out = append(out, gz.Bytes()...)
	return append(out, s.tail...), nil
}

// verify 重新解析新存档，确认结构与目标类型都正确。
func (s *Save) verify(data []byte, desired map[int]bool) error {
	got, err := Load(data)
	if err != nil {
		return err
	}
	if len(got.Teams) != len(s.Teams) {
		return errors.New("队伍数量不一致")
	}
	for _, t := range got.Teams {
		if want, ok := desired[t.Slot]; ok && t.Present && t.IsAI != want {
			return fmt.Errorf("队伍 %s 的类型未生效", t.Name)
		}
	}
	return nil
}
