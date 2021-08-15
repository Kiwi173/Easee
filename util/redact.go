package util

import (
	"context"
	"strings"

	"github.com/andig/evcc/util/internal"
)

type redactor struct {
	parent     *redactor
	redactions []string
}

// Redactor is the context key to use with golang.org/x/net/context's
// WithValue function to associate a *util.Redactor value with a context.
var Redactor internal.ContextKey

func contextRedactor(ctx context.Context) *redactor {
	if ctx != nil {
		if r, ok := ctx.Value(Redactor).(*redactor); ok {
			return r
		}
	}

	return nil
}

func NewRedactor(ctx context.Context) *redactor {
	return &redactor{parent: contextRedactor(ctx)}
}

func (r *redactor) Add(s string) {
	r.redactions = append(r.redactions, s)
}

func (r *redactor) Redact(s string) string {
	for _, match := range r.redactions {
		s = strings.ReplaceAll(s, match, "***")
	}

	if r.parent != nil {
		s = r.parent.Redact(s)
	}

	return s
}
