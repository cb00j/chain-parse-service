package logger

import (
	"os"

	"github.com/sirupsen/logrus"
)

func init() {
	logrus.SetOutput(os.Stdout)
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
	})
}

// SetLevel sets the global log level.
func SetLevel(level string) {
	l, err := logrus.ParseLevel(level)
	if err != nil {
		l = logrus.InfoLevel
	}
	logrus.SetLevel(l)
}

// SetJSONFormat switches the output to JSON format.
func SetJSONFormat() {
	logrus.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
	})
}

// New creates a module-scoped logger with service and module fields pre-bound.
func New(service, module string) *logrus.Entry {
	return logrus.WithFields(logrus.Fields{
		"service": service,
		"module":  module,
	})
}
