// Package idempotency makes create requests safe to retry. A request with
// a request ID records its response, and a retry with the same ID returns
// it instead of creating again. Agents retry after timeouts, and without
// this a retry of a request that had succeeded would create a duplicate.
package idempotency

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"unicode/utf8"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/mealplann.ing/internal/errreport"
	"github.com/bs-tools/mealplann.ing/internal/idempotency/internal/gen"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/proto"
)

// MaxRequestIDLength is the longest request ID, in characters.
const MaxRequestIDLength = 100

// Store records the responses of requests with request IDs.
type Store struct {
	log    *slog.Logger
	report *errreport.Reporter
	db     *store.DB
}

// NewStore returns a Store keeping its records in the database.
func NewStore(log *slog.Logger, report *errreport.Reporter, db *store.DB) *Store {
	return &Store{log: log, report: report, db: db}
}

// Do runs call once per household and request ID, and returns its
// response, or the response of the request that ran it first. Without a
// request ID, or a Store, it just runs call.
//
// A request still running when its retry comes fails the retry with
// ABORTED, and a request ID used for another method fails with
// INVALID_ARGUMENT. A request that fails is forgotten, so its retry runs
// again. Only successful responses are kept, for a day.
func Do[T proto.Message](ctx context.Context, s *Store, householdID pgtype.UUID, method, requestID string, call func() (T, error)) (T, error) {
	var zero T
	if s == nil || requestID == "" {
		return call()
	}
	if utf8.RuneCountInString(requestID) > MaxRequestIDLength {
		return zero, connect.Errorf(connect.CodeInvalidArgument, "request_id must be at most %d characters", MaxRequestIDLength)
	}
	// Expired IDs are deleted, the request recorded as running unless its
	// ID is taken, and the ID's record read, in a single round trip.
	var started int64
	var record *gen.RequestID
	var b store.Batch
	_ = gen.New(&b).DeleteExpiredRequestIDs(ctx)
	b.Read(func(db store.Querier) error {
		var err error
		started, err = gen.New(db).StartRequest(ctx, &gen.StartRequestParams{HouseholdID: householdID, RequestID: requestID, Method: method})
		return err
	})
	b.Read(func(db store.Querier) error {
		var err error
		record, err = gen.New(db).GetRequest(ctx, &gen.GetRequestParams{HouseholdID: householdID, RequestID: requestID})
		return err
	})
	if err := b.Send(ctx, s.db); err != nil {
		return zero, fmt.Errorf("failed to start request: %w", err)
	}

	if started == 0 {
		switch {
		case record.Method != method:
			return zero, connect.Errorf(connect.CodeInvalidArgument,
				"request_id %q was used for %s: use a new request_id for each request", requestID, record.Method)
		case record.Response == nil:
			return zero, connect.Errorf(connect.CodeAborted,
				"a request with request_id %q is still running: retry it in a few seconds to get its response", requestID)
		}
		res := zero.ProtoReflect().New().Interface().(T)
		if err := proto.Unmarshal(record.Response, res); err != nil {
			return zero, fmt.Errorf("failed to read the response of request %q: %w", requestID, err)
		}
		s.log.DebugContext(ctx, "Returning the response of a retried request", slog.String("method", method))
		return res, nil
	}

	res, err := call()
	q := gen.New(s.db)
	// The request's outcome is recorded even if the caller has gone, as a
	// retry is what's expected next.
	ctx = context.WithoutCancel(ctx)
	if err != nil {
		if deleteErr := q.DeleteRequest(ctx, &gen.DeleteRequestParams{HouseholdID: householdID, RequestID: requestID}); deleteErr != nil {
			// The retry is refused as still running until the record
			// expires, which says to use a new request ID soon enough.
			return zero, errors.Join(err, fmt.Errorf("failed to forget request: %w", deleteErr))
		}
		return zero, err
	}
	data, err := proto.Marshal(res)
	if err != nil {
		return zero, fmt.Errorf("failed to record the response: %w", err)
	}
	if err := q.FinishRequest(ctx, &gen.FinishRequestParams{HouseholdID: householdID, RequestID: requestID, Response: data}); err != nil {
		// What was created stays created, so it's returned; a retry would
		// be refused as still running.
		s.report.Report(ctx, "Failed to record the response of a request, so its retries are refused as still running", err, slog.String("method", method))
	}
	return res, nil
}
