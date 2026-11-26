// Package metrics counts statements, with no dependencies, and writes them in
// Prometheus text format, which most monitoring systems read:
//
//	m := metrics.New()
//	db, err := ant.Open[AppDB](ctx, postgres.Dialect, dsn, ant.WithInterceptor(m))
//	http.Handle("/metrics", m)
//
// For OpenTelemetry, read Snapshot from a callback gauge, or see the tracing
// interceptor in the guide.
package metrics

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/skudra-go/ant"
)

// buckets are the histogram's upper bounds, in seconds.
var buckets = []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5}

// Metrics is an ant.Interceptor that counts statements by kind.
type Metrics struct {
	mu     sync.Mutex
	counts map[string]*series
}

type series struct {
	total   uint64
	errors  uint64
	seconds float64
	bucket  []uint64
}

// New returns empty metrics.
func New() *Metrics { return &Metrics{counts: map[string]*series{}} }

// Before implements ant.Interceptor.
func (m *Metrics) Before(ctx context.Context, _ *ant.Statement) (context.Context, error) {
	return ctx, nil
}

// kind returns the statement's operation in upper case, which keeps the
// number of series small.
func kind(text string) string { return strings.ToUpper(Operation(text)) }

// After implements ant.Interceptor.
func (m *Metrics) After(_ context.Context, s *ant.Statement, o ant.Outcome) {
	name := kind(s.SQL)

	m.mu.Lock()
	defer m.mu.Unlock()

	c := m.counts[name]
	if c == nil {
		c = &series{bucket: make([]uint64, len(buckets))}
		m.counts[name] = c
	}

	c.total++

	if o.Err != nil {
		c.errors++
	}

	seconds := o.Duration.Seconds()
	c.seconds += seconds

	for i, bound := range buckets {
		if seconds <= bound {
			c.bucket[i]++
		}
	}
}

// Sample is one kind of statement's totals.
type Sample struct {
	Kind     string        `json:"kind"`
	Count    uint64        `json:"count"`
	Errors   uint64        `json:"errors"`
	Duration time.Duration `json:"duration"`
}

// Snapshot returns the current totals.
func (m *Metrics) Snapshot() []Sample {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]Sample, 0, len(m.counts))

	for name, c := range m.counts {
		out = append(
			out,
			Sample{name, c.total, c.errors, time.Duration(c.seconds * float64(time.Second))},
		)
	}

	return out
}

// WritePrometheus writes the metrics in Prometheus text format.
func (m *Metrics) WritePrometheus(w io.Writer) {
	m.mu.Lock()
	defer m.mu.Unlock()

	fmt.Fprintln(w, "# TYPE ant_statements_total counter")

	for name, c := range m.counts {
		fmt.Fprintf(w, "ant_statements_total{kind=%q} %d\n", name, c.total)
	}

	fmt.Fprintln(w, "# TYPE ant_statement_errors_total counter")

	for name, c := range m.counts {
		fmt.Fprintf(w, "ant_statement_errors_total{kind=%q} %d\n", name, c.errors)
	}

	fmt.Fprintln(w, "# TYPE ant_statement_seconds histogram")

	for name, c := range m.counts {
		for i, bound := range buckets {
			fmt.Fprintf(
				w,
				"ant_statement_seconds_bucket{kind=%q,le=\"%g\"} %d\n",
				name,
				bound,
				c.bucket[i],
			)
		}

		fmt.Fprintf(w, "ant_statement_seconds_bucket{kind=%q,le=\"+Inf\"} %d\n", name, c.total)
		fmt.Fprintf(w, "ant_statement_seconds_sum{kind=%q} %g\n", name, c.seconds)
		fmt.Fprintf(w, "ant_statement_seconds_count{kind=%q} %d\n", name, c.total)
	}
}

// ServeHTTP makes Metrics an http.Handler for /metrics.
func (m *Metrics) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	m.WritePrometheus(w)
}
