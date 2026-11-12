package querying

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/skudra-go/ant"
	"github.com/skudra-go/ant/tests/features/fixture"
)

// ant.Raw calls a function or procedure that takes and returns JSON, with
// ant.JSON marshalling the argument and unmarshalling the result.
type QuoteRequest struct {
	Qty   int     `json:"qty"`
	Price float64 `json:"price"`
}

type Quote struct {
	Total  float64 `json:"total"`
	Status string  `json:"status"`
}

const quoteDDL = `
CREATE FUNCTION quote_order(req jsonb) RETURNS jsonb
LANGUAGE plpgsql AS $$
BEGIN
	IF req IS NULL THEN
		RETURN NULL;
	END IF;

	RETURN jsonb_build_object(
		'total', (req->>'qty')::int * (req->>'price')::numeric,
		'status', 'ok'
	);
END $$;

CREATE FUNCTION quote_each(reqs jsonb) RETURNS SETOF jsonb
LANGUAGE sql AS $$
	SELECT quote_order(r) FROM jsonb_array_elements(reqs) AS r
$$;

CREATE PROCEDURE quote_proc(req jsonb, INOUT result jsonb)
LANGUAGE plpgsql AS $$
BEGIN
	result := quote_order(req);
END $$;
`

func TestRawProcedure(t *testing.T) {
	fixture.RunWith(t, quoteDDL, func(t *testing.T, db *fixture.AppDB) {
		ctx := t.Context()
		req := ant.JSON[QuoteRequest]{V: QuoteRequest{Qty: 3, Price: 2.5}}

		t.Run("a function returns one JSON value", func(t *testing.T) {
			got, err := ant.Raw[ant.JSON[Quote]](
				db,
				`SELECT quote_order(@req)`,
				sql.Named("req", req),
			).First(ctx)
			require.NoError(t, err)
			require.Equal(t, Quote{Total: 7.5, Status: "ok"}, got.V)
		})

		t.Run("a set-returning function gives one value per row", func(t *testing.T) {
			reqs := ant.JSON[[]QuoteRequest]{
				V: []QuoteRequest{
					{Qty: 1, Price: 4},
					{Qty: 2, Price: 0.5},
				},
			}

			got, err := ant.Raw[ant.JSON[Quote]](
				db,
				`SELECT * FROM quote_each(@reqs)`,
				sql.Named("reqs", reqs),
			).All(ctx)
			require.NoError(t, err)
			require.Len(t, got, 2)
			require.Equal(t, 4.0, got[0].V.Total)
			require.Equal(t, 1.0, got[1].V.Total)
		})

		t.Run("a procedure returns its INOUT parameters as a row", func(t *testing.T) {
			got, err := ant.Raw[ant.JSON[Quote]](
				db,
				`CALL quote_proc(@req, NULL)`,
				sql.Named("req", req),
			).First(ctx)
			require.NoError(t, err)
			require.Equal(t, 7.5, got.V.Total)
		})

		t.Run("NULL reads as the zero value", func(t *testing.T) {
			got, err := ant.Raw[ant.JSON[Quote]](
				db,
				`SELECT quote_order(NULL)`,
			).First(ctx)
			require.NoError(t, err)
			require.Zero(t, got.V)
		})
	})
}
