// Package batch runs the items of a batch request, like CreateFoods, each
// as its single request would run, reporting a result per item.
package batch

import (
	"context"
	"errors"
	"sync"

	"connectrpc.com/connect/v2"
)

// MaxItems is how many items a batch request can have. Each item takes a
// few database round trips of about 28 ms, so 100 items take a second or
// two at Concurrency at a time.
const MaxItems = 100

// Concurrency is how many items run at once. The database pool is small,
// and the server has one CPU, so more would only queue.
const Concurrency = 4

// Result is the result of one item: its response, or its error's code and
// message.
type Result[T any] struct {
	Value        T
	ErrorCode    string
	ErrorMessage string
}

// Check returns an error if a batch has no items or too many.
func Check(n int) error {
	switch {
	case n == 0:
		return connect.NewError(connect.CodeInvalidArgument, "requests is empty: give at least one request")
	case n > MaxItems:
		return connect.Errorf(connect.CodeInvalidArgument, "requests has %d requests, more than %d: split it into several batches", n, MaxItems)
	}
	return nil
}

// Run calls fn for each request, Concurrency at a time, and returns their
// results in order. Each item succeeds or fails on its own: an error of
// one doesn't stop the others, and is reported in its result. Errors that
// aren't Connect errors, which services report as internal, are reported
// without their details.
func Run[Req, Res any](ctx context.Context, reqs []Req, fn func(context.Context, Req) (Res, error)) []Result[Res] {
	results := make([]Result[Res], len(reqs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, Concurrency)
	for i, req := range reqs {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() {
				<-sem
				wg.Done()
			}()
			res, err := fn(ctx, req)
			if err == nil {
				results[i].Value = res
				return
			}
			code := connect.CodeOf(err)
			results[i].ErrorCode = code.String()
			if connectErr, ok := errors.AsType[*connect.Error](err); ok && code != connect.CodeInternal && code != connect.CodeUnknown {
				results[i].ErrorMessage = connectErr.Message()
			} else {
				results[i].ErrorMessage = "internal error; retry the request"
			}
		}()
	}
	wg.Wait()
	return results
}
