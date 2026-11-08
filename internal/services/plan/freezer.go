package plan

import (
	"context"
	"math"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/mealplann.ing/internal/authn"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	pantrysvc "github.com/bs-tools/mealplann.ing/internal/services/pantry"
	"github.com/bs-tools/mealplann.ing/internal/store"
)

func (s *Service) ListFrozenLeftovers(ctx context.Context, _ *plansv1.ListFrozenLeftoversRequest) (*plansv1.ListFrozenLeftoversResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	frozen, err := s.plans.ListFrozenCooks(ctx, user.HouseholdID())
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to list frozen leftovers", "")
	}
	today := pantrysvc.Today(s.nowFn())
	leftovers := make([]*plansv1.FrozenLeftover, 0, len(frozen))
	for _, f := range frozen {
		b := plansv1.FrozenLeftover_builder{
			Cook:              cookToProto(f.Cook),
			RemainingServings: round(math.Max(f.FrozenServings-f.LateServings, 0)),
			PlannedServings:   round(f.LateServings),
		}
		if f.FreezerDays != nil {
			keepsUntil := f.Date.AddDate(0, 0, int(*f.FreezerDays))
			b.KeepsUntil = store.FormatDate(keepsUntil)
			b.Expired = keepsUntil.Before(today)
		}
		if f.FridgeDays != nil {
			b.FrozenFrom = store.FormatDate(f.Date.AddDate(0, 0, int(*f.FridgeDays)+1))
		}
		leftovers = append(leftovers, b.Build())
	}
	return plansv1.ListFrozenLeftoversResponse_builder{Leftovers: leftovers}.Build(), nil
}
