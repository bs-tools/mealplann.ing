package plan

import (
	"context"
	"slices"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/mealplann.ing/internal/authn"
	peoplev1 "github.com/bs-tools/mealplann.ing/internal/gen/people/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	"github.com/bs-tools/mealplann.ing/internal/people"
	"github.com/bs-tools/mealplann.ing/internal/plans"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Service) SetEatingOut(ctx context.Context, req *plansv1.SetEatingOutRequest) (*plansv1.SetEatingOutResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	date, err := parseDate("date", req.GetDate())
	if err != nil {
		return nil, err
	}
	notes, err := checkNotes("notes", req.GetNotes(), maxOtherNotesLength)
	if err != nil {
		return nil, err
	}
	var personIDs []pgtype.UUID
	for _, raw := range req.GetPersonIds() {
		id, err := parseID("person_ids", raw, "find people with ListPeople")
		if err != nil {
			return nil, err
		}
		personIDs = append(personIDs, id)
	}
	shared := req.GetMeal() != peoplev1.HouseholdMeal_HOUSEHOLD_MEAL_UNSPECIFIED
	if !shared && (req.GetPersonId() == "" || req.GetEatingTimeId() == "") {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			"give meal, for a shared meal, or person_id and eating_time_id, for one person's eating time")
	}
	if shared && (req.GetPersonId() != "" || req.GetEatingTimeId() != "") {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			"give meal, with person_ids, or person_id and eating_time_id, not both")
	}

	d, err := s.loadPlan(ctx, user.HouseholdID(), date, date)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to read plan", "")
	}
	var slots []plans.Slot
	if shared {
		meal, err := mealFromProto(req.GetMeal())
		if err != nil {
			return nil, err
		}
		eaters, err := mealEaters(d.people, meal, personIDs)
		if err != nil {
			return nil, err
		}
		for _, p := range eaters {
			for _, et := range p.EatingTimes {
				if et.Meal == meal {
					slots = append(slots, plans.Slot{PersonID: p.ID, EatingTimeID: et.ID, Date: date})
				}
			}
		}
	} else {
		personID, err := parseID("person_id", req.GetPersonId(), "find people with ListPeople")
		if err != nil {
			return nil, err
		}
		eatingTimeID, err := parseID("eating_time_id", req.GetEatingTimeId(), "find people's eating times with ListPeople")
		if err != nil {
			return nil, err
		}
		i := slices.IndexFunc(d.people, func(p *people.Person) bool { return p.ID == personID })
		if i < 0 {
			return nil, connect.NewError(connect.CodeInvalidArgument, "person_id isn't in your household; find people with ListPeople")
		}
		if d.people[i].EatingTime(eatingTimeID) == nil {
			return nil, connect.Errorf(connect.CodeInvalidArgument,
				"eating_time_id isn't one of %s's eating times; find them with ListPeople", d.people[i].Name)
		}
		slots = append(slots, plans.Slot{PersonID: personID, EatingTimeID: eatingTimeID, Date: date})
	}
	if req.GetEatingOut() {
		if err := d.checkNothingPlanned(slots, date); err != nil {
			return nil, err
		}
	}
	if err := s.plans.SetEatingOut(ctx, user.HouseholdID(), slots, req.GetEatingOut(), notes); err != nil {
		return nil, s.toConnectError(ctx, err, "failed to set eating out", "")
	}
	return plansv1.SetEatingOutResponse_builder{}.Build(), nil
}

// checkNothingPlanned fails if a portion, or a staple marked eaten, is
// planned at one of the slots, which eating out there would contradict.
func (d *planData) checkNothingPlanned(slots []plans.Slot, date time.Time) error {
	for _, slot := range slots {
		for _, it := range d.items[slot] {
			switch {
			case it.portion != nil:
				return connect.Errorf(connect.CodeFailedPrecondition,
					"%s is planned there on %s: remove it with DeletePortion first, or move it to another eating time with UpdatePortion",
					it.name(), store.FormatDate(date))
			case it.staple.EatenOn(date):
				return connect.Errorf(connect.CodeFailedPrecondition,
					"%s was marked eaten there on %s: un-mark it with SetStapleEaten first, which puts its food back in the pantry",
					it.name(), store.FormatDate(date))
			}
		}
	}
	return nil
}
