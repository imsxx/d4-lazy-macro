package keys

import "testing"

func TestParseKeys(t *testing.T) {
	cases := map[string]uint16{
		"1":     0x31,
		"a":     0x41,
		"F1":    0x70,
		"f12":   0x7B,
		"LMB":   0x01,
		"rmb":   0x02,
		"X1":    0x05,
		"M5":    0x06,
		"TAB":   0x09,
		"space": 0x20,
		"0x31":  0x31,
		" 3 ":   0x33,
	}
	for in, want := range cases {
		got, err := Parse(in)
		if err != nil {
			t.Fatalf("Parse(%q) 返回错误：%v", in, err)
		}
		if got != want {
			t.Fatalf("Parse(%q) = 0x%02X，期望 0x%02X", in, got, want)
		}
	}
}

func TestParseRejectsInvalid(t *testing.T) {
	for _, in := range []string{"", "??", "AB", "0x00", "0xFF", "X3"} {
		if _, err := Parse(in); err == nil {
			t.Fatalf("Parse(%q) 应当报错", in)
		}
	}
}

func TestLabelAndIsMouse(t *testing.T) {
	labels := map[uint16]string{
		0x01: "LMB",
		0x02: "RMB",
		0x05: "X1",
		0x06: "X2",
		0x31: "1",
		0x41: "A",
		0x70: "F1",
		0x7B: "F12",
		0x09: "TAB",
		0x20: "SPACE",
		0xE7: "0xE7",
	}
	for vk, want := range labels {
		if got := Label(vk); got != want {
			t.Fatalf("Label(0x%02X) = %q，期望 %q", vk, got, want)
		}
	}
	if !IsMouse(0x02) || !IsMouse(0x05) || IsMouse(0x31) || IsMouse(0x70) {
		t.Fatalf("IsMouse 判定错误")
	}
}

func TestModifierKeys(t *testing.T) {
	// 修饰键用于「强制站立」等游戏内按键映射；无前缀写法等价于左侧键。
	cases := map[string]uint16{
		"SHIFT": 0xA0, "LSHIFT": 0xA0, "RSHIFT": 0xA1,
		"CTRL": 0xA2, "LCTRL": 0xA2, "RCTRL": 0xA3,
		"ALT": 0xA4, "LALT": 0xA4, "RALT": 0xA5,
	}
	for in, want := range cases {
		got, err := Parse(in)
		if err != nil {
			t.Fatalf("Parse(%q) 返回错误：%v", in, err)
		}
		if got != want {
			t.Fatalf("Parse(%q) = 0x%02X，期望 0x%02X", in, got, want)
		}
	}
	labels := map[uint16]string{0xA0: "LSHIFT", 0xA1: "RSHIFT", 0xA2: "LCTRL", 0xA3: "RCTRL", 0xA4: "LALT", 0xA5: "RALT"}
	for vk, want := range labels {
		if got := Label(vk); got != want {
			t.Fatalf("Label(0x%02X) = %q，期望 %q", vk, got, want)
		}
	}
	if IsMouse(0xA0) {
		t.Fatal("修饰键不应被判定为鼠标键")
	}
}
