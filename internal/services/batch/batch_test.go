package batch

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"connectrpc.com/connect/v2"
)

func TestRun(t *testing.T) {
	t.Parallel()
	var running, most atomic.Int32
	results := Run(context.Background(), []int{1, 2, 3, 4, 5, 6, 7, 8}, func(_ context.Context, n int) (string, error) {
		now := running.Add(1)
		defer running.Add(-1)
		for {
			m := most.Load()
			if now <= m || most.CompareAndSwap(m, now) {
				break
			}
		}
		switch n {
		case 3:
			return "", connect.NewError(connect.CodeNotFound, "no such food")
		case 5:
			return "", errors.New("database is down")
		}
		return fmt.Sprint(n * 10), nil
	})
	if most.Load() > Concurrency {
		t.Errorf("ran %d at once, want at most %d", most.Load(), Concurrency)
	}
	for i, r := range results {
		switch i + 1 {
		case 3:
			if r.ErrorCode != "not_found" || r.ErrorMessage != "no such food" {
				t.Errorf("result %d = %+v, want not found", i, r)
			}
		case 5:
			if r.ErrorCode != "unknown" || r.ErrorMessage != "internal error; retry the request" {
				t.Errorf("result %d = %+v, want an internal error without details", i, r)
			}
		default:
			if r.Value != fmt.Sprint((i+1)*10) || r.ErrorCode != "" {
				t.Errorf("result %d = %+v, want %d", i, r, (i+1)*10)
			}
		}
	}
	if err := Check(0); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("Check(0) = %v, want invalid argument", err)
	}
	if err := Check(MaxItems + 1); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("Check(%d) = %v, want invalid argument", MaxItems+1, err)
	}
	if err := Check(MaxItems); err != nil {
		t.Errorf("Check(%d) = %v, want nil", MaxItems, err)
	}
}
