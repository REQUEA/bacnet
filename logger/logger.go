package logger

type Logger interface {
	Info(...interface{})
	Error(...interface{})
	Trace(...interface{})
}

type NoOpLogger struct{}

func (NoOpLogger) Info(...interface{})  {}
func (NoOpLogger) Error(...interface{}) {}
func (NoOpLogger) Trace(...interface{}) {}

var logger Logger

func SetLogger(l Logger) {
	logger = l
}

func Info(args ...interface{})  { logger.Info(args...) }
func Error(args ...interface{}) { logger.Error(args...) }
func Trace(args ...interface{}) { logger.Trace(args...) }
