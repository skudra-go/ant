// Package schema is the ent schema for the benchmarks. ent generates its client
// from these types, so this is the extra step ent asks for that the others don't.
package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// User mirrors compare.User.
type User struct {
	ent.Schema
}

// Fields of User.
func (User) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Time("created_at").
			Default(time.Now),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
		field.String("name"),
		field.String("email"),
		field.Int("age"),
		field.String("bio").
			Optional().
			Nillable(),
	}
}

// Edges of User.
func (User) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("posts", Post.Type),
	}
}

// Post mirrors compare.Post.
type Post struct {
	ent.Schema
}

// Fields of Post.
func (Post) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Time("created_at").
			Default(time.Now),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
		field.Int64("user_id"),
		field.String("title"),
	}
}

// Edges of Post.
func (Post) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("posts").
			Field("user_id").
			Unique().
			Required(),
	}
}
