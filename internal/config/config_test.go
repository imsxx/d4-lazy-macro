package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

// docExample 是 docs/share-code.md 中的示例码，必须始终可解析。
const docExample = `D4LM1:eyJ2IjoxLCJuIjoi5YWJ55-b5rOV5biILemAn-WItyIsImEiOiLmn5BCROS9nOiAhSIsInRnIjoiRjEiLCJwIjpbeyJuIjoi6YCf5Yi3IiwiciI6W3sidGciOiJ0IiwiYSI6InRhcDo0IiwiaSI6MjUwfSx7InRnIjoiaDpYMSIsImEiOiJzZXE6MiwzIiwiaSI6MjAwfSx7InRnIjoidCIsImEiOiJ0YXA6MyIsImkiOjMwMDAwfSx7InRnIjoidCIsImEiOiJob2xkOlJNQiJ9LHsidGciOiJoOlgyIiwiYSI6Im9uY2U6MSJ9XX1dfQ`

func mustParse(t *testing.T, code string) *Parsed {
	t.Helper()
	p, err := Parse(code)
	if err != nil {
		t.Fatalf("Parse 失败：%v", err)
	}
	return p
}

func TestParseDocExample(t *testing.T) {
	p := mustParse(t, docExample)
	if p.Name != "光矛法师-速刷" || p.Author != "某BD作者" {
		t.Fatalf("名称/作者解析错误：%q / %q", p.Name, p.Author)
	}
	if p.ToggleVK != 0x70 {
		t.Fatalf("启停键应为 F1(0x70)，实际 0x%02X", p.ToggleVK)
	}
	if len(p.Profiles) != 1 || len(p.Profiles[0].Rules) != 5 {
		t.Fatalf("方案/规则数量错误：%d / %d", len(p.Profiles), len(p.Profiles[0].Rules))
	}
	rules := p.Profiles[0].Rules
	if rules[0].Kind != ActionTap || rules[0].Keys[0] != 0x34 || rules[0].IntervalMS != 250 || rules[0].HoldVK != 0 {
		t.Fatalf("规则 1 解析错误：%+v", rules[0])
	}
	if rules[1].HoldVK != 0x05 || rules[1].Kind != ActionSeq || len(rules[1].Keys) != 2 ||
		rules[1].Keys[0] != 0x32 || rules[1].Keys[1] != 0x33 || rules[1].IntervalMS != 200 {
		t.Fatalf("规则 2 解析错误：%+v", rules[1])
	}
	if rules[2].Kind != ActionTap || rules[2].Keys[0] != 0x33 || rules[2].IntervalMS != 30000 {
		t.Fatalf("规则 3 解析错误：%+v", rules[2])
	}
	if rules[3].Kind != ActionHold || rules[3].Keys[0] != 0x02 || rules[3].IntervalMS != 0 {
		t.Fatalf("规则 4 解析错误：%+v", rules[3])
	}
	if rules[4].Kind != ActionOnce || rules[4].HoldVK != 0x06 || rules[4].Keys[0] != 0x31 {
		t.Fatalf("规则 5 解析错误：%+v", rules[4])
	}
}

func TestParseToleratesWhitespacePrefixCaseAndPadding(t *testing.T) {
	payload := strings.TrimPrefix(docExample, Prefix)
	messy := "d4lm1:" + payload[:20] + "\n\t " + payload[20:] + "=="
	p := mustParse(t, messy)
	if len(p.Profiles) != 1 || len(p.Profiles[0].Rules) != 5 {
		t.Fatalf("容错解析结果错误：%+v", p)
	}
}

func codeFromJSON(js string) string {
	return Prefix + base64.RawURLEncoding.EncodeToString([]byte(js))
}

func TestParseRejectsInvalid(t *testing.T) {
	valid := `{"tg":"t","a":"tap:1","i":100}`
	nine := strings.TrimSuffix(strings.Repeat(valid+",", 9), ",")
	cases := map[string]string{
		"空码":       "",
		"前缀错误":     "D4LM2:" + strings.TrimPrefix(docExample, Prefix),
		"非 Base64": Prefix + "不是base64!!",
		"版本不符":     codeFromJSON(`{"v":2,"p":[{"r":[` + valid + `]}]}`),
		"未知字段":     codeFromJSON(`{"v":1,"x":1,"p":[{"r":[` + valid + `]}]}`),
		"没有方案":     codeFromJSON(`{"v":1,"p":[]}`),
		"方案没有规则":   codeFromJSON(`{"v":1,"p":[{"r":[]}]}`),
		"规则过多":     codeFromJSON(`{"v":1,"p":[{"r":[` + nine + `]}]}`),
		"间隔过小":     codeFromJSON(`{"v":1,"p":[{"r":[{"tg":"t","a":"tap:1","i":5}]}]}`),
		"间隔缺失":     codeFromJSON(`{"v":1,"p":[{"r":[{"tg":"t","a":"tap:1"}]}]}`),
		"单点带间隔":    codeFromJSON(`{"v":1,"p":[{"r":[{"tg":"t","a":"once:1","i":100}]}]}`),
		"按键名非法":    codeFromJSON(`{"v":1,"p":[{"r":[{"tg":"t","a":"tap:ZZ","i":100}]}]}`),
		"序列过长":     codeFromJSON(`{"v":1,"p":[{"r":[{"tg":"t","a":"seq:1,2,3,4,5,6,7","i":100}]}]}`),
		"触发方式非法":   codeFromJSON(`{"v":1,"p":[{"r":[{"tg":"x","a":"tap:1","i":100}]}]}`),
		"触发键非法":    codeFromJSON(`{"v":1,"p":[{"r":[{"tg":"h:??","a":"tap:1","i":100}]}]}`),
		"启停键用鼠标":   codeFromJSON(`{"v":1,"tg":"LMB","p":[{"r":[` + valid + `]}]}`),
		"名称过长":     codeFromJSON(`{"v":1,"n":"` + strings.Repeat("长", 25) + `","p":[{"r":[` + valid + `]}]}`),
		"JSON 有尾巴": codeFromJSON(`{"v":1,"p":[{"r":[` + valid + `]}]}{}`),
		"JSON 未闭合": codeFromJSON(`{"v":1,"p":[{"r":[` + valid),
	}
	for name, code := range cases {
		if _, err := Parse(code); err == nil {
			t.Errorf("%s：应当报错但没有", name)
		}
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	raw := Raw{
		Version: 1,
		Name:    "测试配置",
		Author:  "作者",
		Toggle:  "F8",
		Profiles: []RawProfile{
			{Name: "速刷", Rules: []RawRule{
				{Trigger: "t", Action: "tap:4", IntervalMS: 250},
				{Trigger: "h:X1", Action: "seq:2,3", IntervalMS: 200},
				{Trigger: "t", Action: "hold:RMB"},
			}},
		},
	}
	code, err := Encode(raw)
	if err != nil {
		t.Fatalf("Encode 失败：%v", err)
	}
	if !strings.HasPrefix(code, Prefix) {
		t.Fatalf("编码结果缺少前缀：%s", code)
	}
	p := mustParse(t, code)
	if p.ToggleVK != 0x77 || p.Name != "测试配置" {
		t.Fatalf("回读解析错误：%+v", p)
	}
	last := p.Profiles[0].Rules[2]
	if last.Kind != ActionHold || last.Keys[0] != 0x02 {
		t.Fatalf("回读动作解析错误：%+v", last)
	}
}

func TestEncodeRejectsInvalid(t *testing.T) {
	bad := Raw{Version: 1, Profiles: []RawProfile{{Rules: []RawRule{
		{Trigger: "t", Action: "tap:1", IntervalMS: 5},
	}}}}
	if _, err := Encode(bad); err == nil {
		t.Fatalf("非法配置应当拒绝编码")
	}
}
