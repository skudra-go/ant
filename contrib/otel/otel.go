// Package otel traces ant's statements with OpenTelemetry. It is a module of its
// own, so that ant itself has no such dependency.
//
//	db, err := ant.Open[AppDB](ctx, mysql.Dialect, dsn,
//		ant.WithInterceptor(otel.New(tracer, otel.WithDialect(mysql.Dialect))))
//
// Each statement becomes a span under the caller's, named after the kind of
// statement, with its SQL in db.query.text. Values are never recorded.
package otel

import (
	"context"
	"strings"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type spanKey struct{}

// Tracer is an ant.Interceptor that starts a span per statement.
type Tracer struct {
	tracer trace.Tracer
	system string
}

// New returns an interceptor for tracer. Spans say db.system is postgresql
// unless an option names the database.
func New(tracer trace.Tracer, options ...Option) *Tracer {
	t := &Tracer{tracer: tracer, system: "postgresql"}

	for _, o := range options {
		o(t)
	}

	return t
}

// Option configures a Tracer.
type Option func(*Tracer)

// WithDialect sets db.system from the dialect the context uses, in
// OpenTelemetry's spelling: postgresql, mysql, mariadb, sqlite, or mssql. A
// dialect ant does not ship gives its own name, and a nil one changes nothing.
//
//	otel.New(tracer, otel.WithDialect(mysql.Dialect))
func WithDialect(d dialect.Dialect) Option {
	return func(t *Tracer) {
		if d == nil {
			return
		}

		name := d.Name()
		if system, ok := systems[name]; ok {
			name = system
		}

		t.system = name
	}
}

// WithSystem sets db.system as given.
func WithSystem(system string) Option { return func(t *Tracer) { t.system = system } }

// systems maps ant's dialect names to OpenTelemetry's db.system values.
var systems = map[string]string{
	"postgres":  "postgresql",
	"mysql":     "mysql",
	"mariadb":   "mariadb",
	"sqlite":    "sqlite",
	"sqlserver": "mssql",
}

// Before implements ant.Interceptor.
func (t *Tracer) Before(ctx context.Context, s *ant.Statement) (context.Context, error) {
	name, _, _ := strings.Cut(strings.TrimSpace(s.SQL), " ")
	ctx, span := t.tracer.Start(
		ctx,
		"db."+strings.ToLower(name),
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", t.system),
			attribute.String("db.query.text", s.SQL),
		),
	)

	return context.WithValue(ctx, spanKey{}, span), nil
}

// After implements ant.Interceptor.
func (t *Tracer) After(ctx context.Context, _ *ant.Statement, o ant.Outcome) {
	span, ok := ctx.Value(spanKey{}).(trace.Span)
	if !ok {
		return
	}

	if o.Err != nil {
		span.RecordError(o.Err)
		span.SetStatus(codes.Error, o.Err.Error())
	}

	span.End()
}
