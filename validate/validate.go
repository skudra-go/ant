// Package validate holds common rules for ModelBuilder.Validate:
//
//	b.Validate(&Users.Name, validate.Required[string]())
//	b.Validate(&Users.Name, validate.MaxLength(100))
//	b.Validate(&Users.Age, validate.Range(0, 150))
//	b.Validate(&Users.Email, validate.Email)
//	b.Validate(&Users.Plan, validate.OneOf("free", "pro"))
//
// A rule is a func(V) error, so your own need nothing from here.
package validate

import (
	"cmp"
	"errors"
	"fmt"
	"net/mail"
	"reflect"
	"regexp"
	"slices"
	"unicode/utf8"
)

// ErrRequired reports a zero value in a required field.
var ErrRequired = errors.New("is required")

// Required rejects the zero value.
func Required[V any]() func(V) error {
	return func(v V) error {
		if reflect.ValueOf(&v).Elem().IsZero() {
			return ErrRequired
		}

		return nil
	}
}

// MinLength rejects a string with fewer characters than n.
func MinLength(n int) func(string) error {
	return func(v string) error {
		if utf8.RuneCountInString(v) < n {
			return fmt.Errorf("must have at least %d characters", n)
		}

		return nil
	}
}

// MaxLength rejects a string with more characters than n.
func MaxLength(n int) func(string) error {
	return func(v string) error {
		if utf8.RuneCountInString(v) > n {
			return fmt.Errorf("must have at most %d characters", n)
		}

		return nil
	}
}

// Range rejects a value outside low and high, inclusive.
func Range[V cmp.Ordered](low, high V) func(V) error {
	return func(v V) error {
		if v < low || v > high {
			return fmt.Errorf("must be from %v to %v", low, high)
		}

		return nil
	}
}

// OneOf rejects a value not in the list.
func OneOf[V comparable](allowed ...V) func(V) error {
	return func(v V) error {
		if !slices.Contains(allowed, v) {
			return fmt.Errorf("must be one of %v", allowed)
		}

		return nil
	}
}

// Matches rejects a string that does not match pattern.
func Matches(pattern *regexp.Regexp) func(string) error {
	return func(v string) error {
		if !pattern.MatchString(v) {
			return fmt.Errorf("must match %s", pattern)
		}

		return nil
	}
}

// Email rejects a string that is not an email address.
func Email(v string) error {
	if _, err := mail.ParseAddress(v); err != nil {
		return errors.New("must be an email address")
	}

	return nil
}

// All runs rules in order, stopping at the first failure.
func All[V any](rules ...func(V) error) func(V) error {
	return func(v V) error {
		for _, rule := range rules {
			if err := rule(v); err != nil {
				return err
			}
		}

		return nil
	}
}
