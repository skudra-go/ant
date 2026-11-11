package developer_tooling

import (
	"context"
	"sync"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

type User = fixture.User

var Users = fixture.Users

// recorder is an Interceptor that keeps the statements it sees.
type recorder struct {
	mu     sync.Mutex
	sql    []string
	done   []ant.Outcome
	refuse error
}

func (r *recorder) Before(ctx context.Context, s *ant.Statement) (context.Context, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.sql = append(r.sql, s.SQL)

	return ctx, r.refuse
}

func (r *recorder) After(_ context.Context, _ *ant.Statement, o ant.Outcome) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.done = append(r.done, o)
}

func (r *recorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.sql, r.done = nil, nil
}

func (r *recorder) statements() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string{}, r.sql...)
}
