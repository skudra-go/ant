package otel

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/dialect"
)

func TestSpans(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	tracer := New(provider.Tracer("test"))

	statement := &ant.Statement{SQL: "SELECT * FROM users WHERE id = $1", Args: []any{"secret"}}

	ctx, err := tracer.Before(context.Background(), statement)
	if err != nil {
		t.Fatal(err)
	}

	tracer.After(ctx, statement, ant.Outcome{Err: errors.New("boom")})

	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "db.select" {
		t.Fatalf("spans: %+v", spans)
	}

	if spans[0].Status().Code != codes.Error {
		t.Fatalf("status: %+v", spans[0].Status())
	}

	for _, attribute := range spans[0].Attributes() {
		if attribute.Value.AsString() == "secret" {
			t.Fatal("a value reached a span")
		}
	}
}

// fake is a dialect with only a name, which is all WithDialect reads. The names
// are those ant's dialects give: postgres, mysql, mariadb, sqlite, sqlserver.
type fake struct {
	dialect.Dialect
	name string
}

func (f fake) Name() string { return f.name }

func named(name string) dialect.Dialect { return fake{name: name} }

// db.system names the database the dialect talks to, as OpenTelemetry spells it.
func TestSystem(t *testing.T) {
	cases := []struct {
		name    string
		options []Option
		want    string
	}{
		{"default", nil, "postgresql"},
		{"postgres", []Option{WithDialect(named("postgres"))}, "postgresql"},
		{"mysql", []Option{WithDialect(named("mysql"))}, "mysql"},
		{"mariadb", []Option{WithDialect(named("mariadb"))}, "mariadb"},
		{"sqlite", []Option{WithDialect(named("sqlite"))}, "sqlite"},
		{"sqlserver", []Option{WithDialect(named("sqlserver"))}, "mssql"},
		{"other dialect", []Option{WithDialect(named("cockroach"))}, "cockroach"},
		{"nil dialect", []Option{WithDialect(nil)}, "postgresql"},
		{"named", []Option{WithSystem("cockroachdb")}, "cockroachdb"},
		{"last wins", []Option{WithDialect(named("mysql")), WithSystem("tidb")}, "tidb"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			tracer := New(provider.Tracer("test"), c.options...)

			statement := &ant.Statement{SQL: "SELECT 1"}

			ctx, err := tracer.Before(context.Background(), statement)
			if err != nil {
				t.Fatal(err)
			}

			tracer.After(ctx, statement, ant.Outcome{})

			got := ""

			for _, a := range recorder.Ended()[0].Attributes() {
				if a.Key == "db.system" {
					got = a.Value.AsString()
				}
			}

			if got != c.want {
				t.Fatalf("db.system = %q, want %q", got, c.want)
			}
		})
	}
}
