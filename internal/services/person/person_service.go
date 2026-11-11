// Package person implements the PersonService: the people households plan
// meals for, with their eating patterns, rules and targets.
package person

import (
	"context"
	"errors"
	"log/slog"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/mealplann.ing/internal/authn"
	"github.com/bs-tools/mealplann.ing/internal/errreport"
	peoplev1 "github.com/bs-tools/mealplann.ing/internal/gen/people/v1"
	"github.com/bs-tools/mealplann.ing/internal/gen/people/v1/peoplev1connect"
	"github.com/bs-tools/mealplann.ing/internal/people"
	"github.com/bs-tools/mealplann.ing/internal/protomask"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var _ peoplev1connect.PersonServiceHandler = (*Service)(nil)

// Repository stores people.
type Repository interface {
	CreatePerson(ctx context.Context, p *people.Person) (*people.Person, error)
	GetPerson(ctx context.Context, householdID, id pgtype.UUID) (*people.Person, error)
	ListPeople(ctx context.Context, householdID pgtype.UUID) ([]*people.Person, error)
	UpdatePerson(ctx context.Context, p *people.Person) (*people.Person, error)
	DeletePerson(ctx context.Context, householdID, id pgtype.UUID) error
}

// Service implements the PersonService.
type Service struct {
	log    *slog.Logger
	report *errreport.Reporter
	people Repository
}

// NewService returns a PersonService storing people in the repository.
func NewService(log *slog.Logger, report *errreport.Reporter, people Repository) *Service {
	return &Service{log: log, report: report, people: people}
}

// keepFields are the fields of a person updates don't change.
var keepFields = []protoreflect.Name{"id", "recommended", "create_time", "update_time"}

func (s *Service) CreatePerson(ctx context.Context, req *peoplev1.CreatePersonRequest) (*peoplev1.CreatePersonResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	p, err := PersonFromProto(req.GetPerson(), nil)
	if err != nil {
		return nil, err
	}
	p.HouseholdID = user.HouseholdID()
	created, err := s.people.CreatePerson(ctx, p)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to create person")
	}
	return peoplev1.CreatePersonResponse_builder{Person: PersonToProto(created)}.Build(), nil
}

func (s *Service) GetPerson(ctx context.Context, req *peoplev1.GetPersonRequest) (*peoplev1.GetPersonResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := ParsePersonID(req.GetId())
	if err != nil {
		return nil, err
	}
	p, err := s.people.GetPerson(ctx, user.HouseholdID(), id)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to get person")
	}
	return peoplev1.GetPersonResponse_builder{Person: PersonToProto(p)}.Build(), nil
}

func (s *Service) ListPeople(ctx context.Context, _ *peoplev1.ListPeopleRequest) (*peoplev1.ListPeopleResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	list, err := s.people.ListPeople(ctx, user.HouseholdID())
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to list people")
	}
	result := make([]*peoplev1.Person, 0, len(list))
	for _, p := range list {
		result = append(result, PersonToProto(p))
	}
	return peoplev1.ListPeopleResponse_builder{People: result}.Build(), nil
}

func (s *Service) UpdatePerson(ctx context.Context, req *peoplev1.UpdatePersonRequest) (*peoplev1.UpdatePersonResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := ParsePersonID(req.GetPerson().GetId())
	if err != nil {
		return nil, err
	}
	existing, err := s.people.GetPerson(ctx, user.HouseholdID(), id)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to get person")
	}
	merged := PersonToProto(existing)
	if err := protomask.Apply(merged, req.GetPerson(), req.GetUpdateMask(), keepFields...); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err.Error())
	}
	p, err := PersonFromProto(merged, existing)
	if err != nil {
		return nil, err
	}
	p.ID = existing.ID
	p.HouseholdID = existing.HouseholdID
	updated, err := s.people.UpdatePerson(ctx, p)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to update person")
	}
	return peoplev1.UpdatePersonResponse_builder{Person: PersonToProto(updated)}.Build(), nil
}

func (s *Service) DeletePerson(ctx context.Context, req *peoplev1.DeletePersonRequest) (*peoplev1.DeletePersonResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := ParsePersonID(req.GetId())
	if err != nil {
		return nil, err
	}
	if err := s.people.DeletePerson(ctx, user.HouseholdID(), id); err != nil {
		return nil, s.toConnectError(ctx, err, "failed to delete person")
	}
	return peoplev1.DeletePersonResponse_builder{}.Build(), nil
}

// ParsePersonID parses a person's ID from the API.
func ParsePersonID(s string) (pgtype.UUID, error) {
	id, err := store.StringToUUID(s)
	if err != nil {
		return pgtype.UUID{}, connect.Errorf(connect.CodeInvalidArgument, "invalid person ID: %v; find people with ListPeople", err).WithCause(err)
	}
	return id, nil
}

// toConnectError maps an error of the repository to a Connect error.
func (s *Service) toConnectError(ctx context.Context, err error, msg string) error {
	if connectErr, ok := errors.AsType[*connect.Error](err); ok {
		return connectErr
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, "the person doesn't exist in your household; find people with ListPeople")
	case errors.Is(err, people.ErrUserNotInHousehold):
		return connect.NewError(connect.CodeInvalidArgument, "user_id isn't a user of your household; find them with GetHousehold, or leave it empty")
	case errors.Is(err, people.ErrUserTaken):
		return connect.NewError(connect.CodeAlreadyExists, "another person is already this user; link each user to one person, or leave user_id empty")
	}
	s.report.Report(ctx, msg, err)
	return connect.NewError(connect.CodeInternal, msg).WithCause(err)
}
