package rpc

import (
    "context"
    "net/http"
)

// ctxKeyHTTPHeaders is an unexported type used as context key to avoid collisions.
type ctxKeyHTTPHeaders struct{}

// withHTTPHeaders returns a new context that carries the provided HTTP headers.
func withHTTPHeaders(ctx context.Context, h http.Header) context.Context {
    if h == nil {
        return ctx
    }
    return context.WithValue(ctx, ctxKeyHTTPHeaders{}, h)
}

// HTTPHeadersFromContext retrieves the HTTP headers from context if present.
// Returns nil if not attached to the context.
func HTTPHeadersFromContext(ctx context.Context) http.Header {
    if v := ctx.Value(ctxKeyHTTPHeaders{}); v != nil {
        if h, ok := v.(http.Header); ok {
            return h
        }
    }
    return nil
}

