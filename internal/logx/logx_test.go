package logx

import (
	"os"
	"strings"
	"testing"
)

func TestPrintfBuffersAndWritesFile(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatalf("New 失败：%v", err)
	}
	defer l.Close()
	l.Printf("第一条 %d", 1)
	l.Printf("第二条 %s", "abc")
	lines := l.Lines()
	if len(lines) != 2 || !strings.Contains(lines[0], "第一条 1") || !strings.Contains(lines[1], "第二条 abc") {
		t.Fatalf("内存缓冲内容不符：%v", lines)
	}
	data, err := os.ReadFile(l.FilePath())
	if err != nil {
		t.Fatalf("读取日志文件失败：%v", err)
	}
	if !strings.Contains(string(data), "第二条 abc") {
		t.Fatalf("日志文件内容不符：%s", string(data))
	}
}

func TestRingBufferCapacity(t *testing.T) {
	l, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New 失败：%v", err)
	}
	defer l.Close()
	for i := 0; i < maxMemLines+20; i++ {
		l.Printf("行 %d", i)
	}
	lines := l.Lines()
	if len(lines) != maxMemLines {
		t.Fatalf("期望保留 %d 行，实际 %d", maxMemLines, len(lines))
	}
	if !strings.Contains(lines[len(lines)-1], "行 519") {
		t.Fatalf("最后一行不符：%s", lines[len(lines)-1])
	}
}

func TestClearKeepsFileAndRotates(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatalf("New 失败：%v", err)
	}
	l.Printf("清空前的记录")
	l.Clear()
	if len(l.Lines()) != 0 {
		t.Fatalf("Clear 后内存缓冲应为空")
	}
	data, err := os.ReadFile(l.FilePath())
	if err != nil {
		t.Fatalf("读取日志文件失败：%v", err)
	}
	if !strings.Contains(string(data), "清空前的记录") {
		t.Fatalf("Clear 不应清除文件历史")
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close 失败：%v", err)
	}

	big := make([]byte, maxFileSize+16)
	for i := range big {
		big[i] = 'x'
	}
	if err := os.WriteFile(l.FilePath(), big, 0o644); err != nil {
		t.Fatalf("构造大日志失败：%v", err)
	}
	l2, err := New(dir)
	if err != nil {
		t.Fatalf("轮转后 New 失败：%v", err)
	}
	defer l2.Close()
	if _, err := os.Stat(l2.FilePath() + ".1"); err != nil {
		t.Fatalf("轮转文件不存在：%v", err)
	}
}
