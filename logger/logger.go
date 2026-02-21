package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	nested "github.com/antonfisher/nested-logrus-formatter"
	log "github.com/sirupsen/logrus"
)

const (
	TYPE_HTTP = 1
)

func init() {
	// 不设置默认输出，由应用程序决定
	log.SetFormatter(Formatter(false)) // 默认不使用颜色
	// 默认输出到标准输出，避免启动早期日志写入 stderr 导致控制台显示为红色
	log.SetOutput(os.Stdout)
}

// SetOutput 设置日志输出目标
func SetOutput(out *os.File) {
	log.SetOutput(out)
}

// SetLevel 设置日志级别
func SetLevel(level log.Level) {
	log.SetLevel(level)
}

// UseStdout 使用标准输出
func UseStdout() {
	log.SetOutput(os.Stdout)
	log.SetFormatter(Formatter(true))
}

/*
func getUserInfo(ctx *gin.Context) int {
	if data, ok := ctx.Get("uid"); ok {
		if uid, ok := data.(int); ok {
			return uid
		}
	}
	return 0
}
*/

// getCaller 获取实际的调用者信息（跳过logger包装层）
func getCaller() (string, int) {
	// 跳过日志库的调用栈，获取实际调用者
	// 通过调用栈：用户代码 -> logger.Info -> addCallerField -> getCaller -> runtime.Caller
	// 所以需要跳过3层才能到达实际调用位置
	_, file, line, ok := runtime.Caller(3)
	if !ok {
		return "unknown", 0
	}
	// 提取文件名（不带路径）
	shortFile := filepath.Base(file)
	return shortFile, line
}

// addCallerField 添加调用者信息到日志字段
func addCallerField() *log.Entry {
	file, line := getCaller()
	return log.WithField("caller", fmt.Sprintf("%s:%d", file, line))
}

func Info(args ...interface{}) {
	addCallerField().Info(args...)
}

func Error(args ...interface{}) {
	addCallerField().Error(args...)
}

func Debug(args ...interface{}) {
	addCallerField().Debug(args...)
}

func Warn(args ...interface{}) {
	addCallerField().Warn(args...)
}

func Fatal(args ...interface{}) {
	addCallerField().Fatal(args...)
}

func Infof(format string, args ...interface{}) {
	addCallerField().Infof(format, args...)
}

func Errorf(format string, args ...interface{}) {
	addCallerField().Errorf(format, args...)
}

func Debugf(format string, args ...interface{}) {
	addCallerField().Debugf(format, args...)
}

func Warnf(format string, args ...interface{}) {
	addCallerField().Warnf(format, args...)
}

func Fatalf(format string, args ...interface{}) {
	addCallerField().Fatalf(format, args...)
}

func Log(args ...interface{}) *log.Entry {
	fields := log.Fields{}
	lenArgs := len(args)
	for i := 0; i < lenArgs; i = i + 2 {
		var key string
		var ok bool
		if key, ok = args[i].(string); !ok {
			continue
		}

		if i <= lenArgs-2 {
			fields[key] = args[i+1]
			continue
		}
		fields[key] = ""
	}

	// 添加调用者信息
	// 在Log函数调用链中也需要调整层级
	_, file, line, ok := runtime.Caller(2)
	if !ok {
		file = "unknown"
		line = 0
	}
	shortFile := filepath.Base(file)
	fields["caller"] = fmt.Sprintf("%s:%d", shortFile, line)

	log.SetFormatter(Formatter(true))
	return log.WithFields(fields)
}

func Formatter(isConsole bool) *nested.Formatter {
	fmtter := &nested.Formatter{
		FieldsOrder:      []string{"time", "level", "caller", "msg"},
		HideKeys:         true,
		TimestampFormat:  "2006-01-02 15:04:05.000",
		CallerFirst:      true,
		NoUppercaseLevel: true,
		ShowFullLevel:    true,
		//NoFieldsSpace:    true,
		// 禁用默认的调用者格式化，因为我们已经添加了自定义的caller字段
		CustomCallerFormatter: func(frame *runtime.Frame) string {
			return ""
		},
	}
	if isConsole {
		fmtter.NoColors = false
	} else {
		fmtter.NoColors = true
	}
	return fmtter
}

// DebugStack 用于调试日志调用栈，输出当前调用链的所有调用者信息
func DebugStack() {
	for i := 0; i < 5; i++ {
		_, file, line, ok := runtime.Caller(i)
		if !ok {
			break
		}
		shortFile := filepath.Base(file)
		log.Infof("调用栈[%d]: %s:%d", i, shortFile, line)
	}
}
