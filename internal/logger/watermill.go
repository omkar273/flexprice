package logger

import "github.com/ThreeDotsLabs/watermill"

// watermillLogger adapts our Logger to watermill's LoggerAdapter interface.
type watermillLogger struct {
	logger *Logger
	fields watermill.LogFields
}

// GetWatermillLogger returns a watermill-compatible logger adapter.
func (l *Logger) GetWatermillLogger() watermill.LoggerAdapter {
	return &watermillLogger{logger: l}
}

func (w *watermillLogger) Error(msg string, err error, fields watermill.LogFields) {
	w.logger.Errorw(msg, w.toArgs(fields, "error", err)...)
}

func (w *watermillLogger) Info(msg string, fields watermill.LogFields) {
	w.logger.Infow(msg, w.toArgs(fields)...)
}

func (w *watermillLogger) Debug(msg string, fields watermill.LogFields) {
	w.logger.Debugw(msg, w.toArgs(fields)...)
}

func (w *watermillLogger) Trace(_ string, _ watermill.LogFields) {
	// Suppress TRACE — too noisy for high-frequency Kafka operations.
}

func (w *watermillLogger) With(fields watermill.LogFields) watermill.LoggerAdapter {
	merged := make(watermill.LogFields, len(w.fields)+len(fields))
	for k, v := range w.fields {
		merged[k] = v
	}
	for k, v := range fields {
		merged[k] = v
	}
	return &watermillLogger{logger: w.logger, fields: merged}
}

func (w *watermillLogger) toArgs(fields watermill.LogFields, extra ...interface{}) []interface{} {
	args := make([]interface{}, 0, (len(w.fields)+len(fields))*2+len(extra))
	for k, v := range w.fields {
		args = append(args, k, v)
	}
	for k, v := range fields {
		args = append(args, k, v)
	}
	return append(args, extra...)
}
