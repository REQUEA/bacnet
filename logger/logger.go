package logger

import "fmt"

type Logger interface {
	Info(...interface{})
	Error(...interface{})
	Trace(...interface{})
	Infof(format string, args ...interface{})
	Errorf(format string, args ...interface{})
	Tracef(format string, args ...interface{})
}

type NoOpLogger struct{}

func (NoOpLogger) Info(...interface{})           {}
func (NoOpLogger) Error(...interface{})          {}
func (NoOpLogger) Trace(...interface{})          {}
func (NoOpLogger) Infof(string, ...interface{})  {}
func (NoOpLogger) Errorf(string, ...interface{}) {}
func (NoOpLogger) Tracef(string, ...interface{}) {}

var logger Logger = NoOpLogger{}

func SetLogger(l Logger) {
	logger = l
}

func Info(args ...interface{})                  { logger.Info(args...) }
func Error(args ...interface{})                 { logger.Error(args...) }
func Trace(args ...interface{})                 { logger.Trace(args...) }
func Infof(format string, args ...interface{})  { logger.Infof(format, args...) }
func Errorf(format string, args ...interface{}) { logger.Errorf(format, args...) }
func Tracef(format string, args ...interface{}) { logger.Tracef(format, args...) }

// FmtLogger wraps a Logger that only implements Info/Error/Trace (without f-variants)
// by formatting via fmt.Sprintf. Useful for adapting simple loggers.
type FmtLogger struct {
	Base interface {
		Info(...interface{})
		Error(...interface{})
		Trace(...interface{})
	}
}

func (l FmtLogger) Info(args ...interface{})  { l.Base.Info(args...) }
func (l FmtLogger) Error(args ...interface{}) { l.Base.Error(args...) }
func (l FmtLogger) Trace(args ...interface{}) { l.Base.Trace(args...) }
func (l FmtLogger) Infof(format string, args ...interface{}) {
	l.Base.Info(fmt.Sprintf(format, args...))
}
func (l FmtLogger) Errorf(format string, args ...interface{}) {
	l.Base.Error(fmt.Sprintf(format, args...))
}
func (l FmtLogger) Tracef(format string, args ...interface{}) {
	l.Base.Trace(fmt.Sprintf(format, args...))
}
