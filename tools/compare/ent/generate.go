// Package ent is the client ent generates from ./schema. Regenerate it with
// go generate ./ent after changing the schema.
package ent

//go:generate go run -mod=mod entgo.io/ent/cmd/ent generate ./schema
