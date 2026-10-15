// Package logging provides a zap <-> slog bridge.
//
// Echo v5's Logger is a *slog.Logger and echozap only supports v4, so this adapter forwards
// slog records to an existing zap logger and keeps the whole process on one structured-JSON
// log format.
package logging

import (
	"context"
	"log/slog"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// SlogHandler implements slog.Handler and forwards records to zap.
type SlogHandler struct {
	logger *zap.Logger
	attrs  []slog.Attr
	group  string
}

func NewSlogHandler(logger *zap.Logger) *SlogHandler {
	return &SlogHandler{logger: logger}
}

func (h *SlogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return h.logger.Core().Enabled(toZapLevel(level))
}

func (h *SlogHandler) Handle(_ context.Context, r slog.Record) error {
	fields := make([]zap.Field, 0, len(h.attrs)+r.NumAttrs()+2)
	for _, a := range h.attrs {
		fields = append(fields, attrToField(h.group, a))
	}
	r.Attrs(func(a slog.Attr) bool {
		fields = append(fields, attrToField(h.group, a))
		return true
	})
	if !r.Time.IsZero() {
		fields = append(fields, zap.Time("time", r.Time))
	}
	fields = append(fields, zap.Int("slog_level", int(r.Level)))

	h.logger.Log(toZapLevel(r.Level), r.Message, fields...)
	return nil
}

func (h *SlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &next
}

// The group prefix is joined in attrToField.
func (h *SlogHandler) WithGroup(name string) slog.Handler {
	next := *h
	if h.group == "" {
		next.group = name
	} else {
		next.group = h.group + "." + name
	}
	return &next
}

func toZapLevel(level slog.Level) zapcore.Level {
	switch {
	case level < slog.LevelInfo:
		return zapcore.DebugLevel
	case level < slog.LevelWarn:
		return zapcore.InfoLevel
	case level < slog.LevelError:
		return zapcore.WarnLevel
	default:
		return zapcore.ErrorLevel
	}
}

func attrToField(group string, a slog.Attr) zap.Field {
	key := a.Key
	if group != "" {
		key = group + "." + key
	}
	switch a.Value.Kind() {
	case slog.KindString:
		return zap.String(key, a.Value.String())
	case slog.KindInt64:
		return zap.Int64(key, a.Value.Int64())
	case slog.KindUint64:
		return zap.Uint64(key, a.Value.Uint64())
	case slog.KindFloat64:
		return zap.Float64(key, a.Value.Float64())
	case slog.KindBool:
		return zap.Bool(key, a.Value.Bool())
	case slog.KindDuration:
		return zap.Duration(key, a.Value.Duration())
	case slog.KindTime:
		return zap.Time(key, a.Value.Time())
	default:
		return zap.Any(key, a.Value.Any())
	}
}
