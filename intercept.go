package ant

import (
	"context"
	"database/sql/driver"
	"log/slog"
	"reflect"
	"time"

	"github.com/skudra-go/ant/internal/exec"
)

// Statement is one SQL statement on its way to the database. An interceptor can
// change it. Args holds real values, so take care what you record.
type Statement = exec.Statement

// Outcome is what happened to a statement. Duration ends when the database
// answered, before rows are read.
type Outcome = exec.Outcome

// Interceptor observes every statement a context runs, for logging, metrics,
// and tracing. Before can return a new context, as tracing needs, and an error
// from it stops the statement.
type Interceptor = exec.Interceptor

// WithInterceptor adds an interceptor. Several run in order, and After runs in
// reverse order.
func WithInterceptor(i Interceptor) Option {
	return func(s *settings) { s.interceptors = append(s.interceptors, i) }
}

// logging is the Interceptor from WithLogger.
type logging struct {
	logger    *slog.Logger
	slow      time.Duration
	sensitive bool
}

// Before implements Interceptor.
func (l *logging) Before(
	ctx context.Context,
	_ *Statement,
) (context.Context, error) {
	return ctx, nil
}

// After implements Interceptor.
func (l *logging) After(ctx context.Context, s *Statement, o Outcome) {
	attrs := []slog.Attr{
		slog.String("sql", s.SQL),
		slog.Duration("duration", o.Duration),
		slog.Int("args", len(s.Args)),
	}

	if l.sensitive {
		attrs = append(attrs, slog.Any("values", loggedArgs(s.Args)))
	}

	switch {
	case o.Err != nil:
		l.logger.LogAttrs(
			ctx,
			slog.LevelError,
			"ant: statement failed",
			append(attrs, slog.String("error", o.Err.Error()))...)
	case l.slow > 0 && o.Duration >= l.slow:
		l.logger.LogAttrs(ctx, slog.LevelWarn, "ant: slow statement", attrs...)
	default:
		l.logger.LogAttrs(ctx, slog.LevelDebug, "ant: statement", attrs...)
	}
}

// LogOption configures WithLogger.
type LogOption func(*logging)

// SlowThreshold logs statements at or over d at warning level.
func SlowThreshold(d time.Duration) LogOption { return func(l *logging) { l.slow = d } }

// LogValues includes argument values in logs. They are left out by default,
// because they may be passwords or personal data.
func LogValues() LogOption { return func(l *logging) { l.sensitive = true } }

// WithLogger logs every statement to logger: failures at error level, slow ones
// at warning level, and the rest at debug level. Values are never logged unless
// LogValues asks.
func WithLogger(logger *slog.Logger, options ...LogOption) Option {
	l := &logging{logger: logger}

	for _, option := range options {
		option(l)
	}

	return WithInterceptor(l)
}

// loggedArgs returns args as a log shows them: a pointer as what it points at,
// or nil, and a driver.Valuer as the value it gives the database. A value that
// says how it logs, as a sensitive one does, logs that way.
func loggedArgs(args []any) []any {
	out := make([]any, len(args))

	for i, arg := range args {
		out[i] = loggedArg(arg)
	}

	return out
}

func loggedArg(arg any) any {
	for range 8 { // a pointer to a Valuer that gives a pointer, and so on
		if _, ok := arg.(slog.LogValuer); ok {
			return arg
		}

		v := reflect.ValueOf(arg)
		if v.Kind() == reflect.Pointer && v.IsNil() {
			return nil
		}

		if valuer, ok := arg.(driver.Valuer); ok {
			value, err := valuer.Value()
			if err != nil {
				return arg
			}

			arg = value

			continue
		}

		if v.Kind() != reflect.Pointer {
			return arg
		}

		arg = v.Elem().Interface()
	}

	return arg
}
