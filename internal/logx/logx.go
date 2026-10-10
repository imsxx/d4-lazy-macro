// Package logx 提供带环形缓冲与文件落盘的日志器，供执行器与日志窗口共用。
package logx

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	maxMemLines = 500
	maxFileSize = 2 << 20 // 2MB 后轮转为 app.1.log
)

// Logger 是并发安全的日志器：内存保留最近 maxMemLines 行，同时追加写入文件。
type Logger struct {
	mu       sync.Mutex
	lines    []string
	file     *os.File
	filePath string
}

// New 打开（或创建）<dir>/logs/app.log，超过 2MB 时先轮转。
func New(dir string) (*Logger, error) {
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建日志目录失败：%v", err)
	}
	path := filepath.Join(logDir, "app.log")
	if info, err := os.Stat(path); err == nil && info.Size() > maxFileSize {
		_ = os.Remove(path + ".1")
		if err := os.Rename(path, path+".1"); err != nil {
			return nil, fmt.Errorf("轮转日志失败：%v", err)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("打开日志文件失败：%v", err)
	}
	return &Logger{file: f, filePath: path}, nil
}

// Printf 记录一行日志：内存缓冲 + 日志文件 + 控制台（开发构建可见）。
func (l *Logger) Printf(format string, args ...any) {
	line := time.Now().Format("2006-01-02 15:04:05.000") + " " + fmt.Sprintf(format, args...)
	l.mu.Lock()
	l.lines = append(l.lines, line)
	if len(l.lines) > maxMemLines {
		l.lines = append([]string(nil), l.lines[len(l.lines)-maxMemLines:]...)
	}
	if l.file != nil {
		if _, err := l.file.WriteString(line + "\r\n"); err != nil {
			fmt.Fprintln(os.Stderr, "[日志] 写入文件失败：", err)
			_ = l.file.Close()
			l.file = nil
		}
	}
	l.mu.Unlock()
	fmt.Println(line)
}

// Lines 返回当前内存缓冲的副本。
func (l *Logger) Lines() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}

// Clear 清空内存缓冲（日志文件保留历史）。
func (l *Logger) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = nil
}

// FilePath 返回日志文件路径。
func (l *Logger) FilePath() string {
	return l.filePath
}

// Close 关闭日志文件。
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}
