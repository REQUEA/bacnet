package bacip

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
