package user

import (
	"context"
	"errors"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/mealplann.ing/internal/authn"
	usersv1 "github.com/bs-tools/mealplann.ing/internal/gen/users/v1"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/bs-tools/mealplann.ing/internal/users"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// CalendarPath is the path calendar feeds are served on, followed by the
// feed's secret and ".ics".
const CalendarPath = "/calendar/"

// GetCalendarFeed returns whether the user has a calendar feed. Like API
// tokens, feeds are managed from the web app only, as their URLs read the
// user's plan without signing in.
func (s *Service) GetCalendarFeed(ctx context.Context, req *usersv1.GetCalendarFeedRequest) (*usersv1.GetCalendarFeedResponse, error) {
	user, err := authn.GetSessionUser(ctx)
	if err != nil {
		return nil, err
	}
	feed, err := s.feeds.GetCalendarFeed(ctx, user.ID())
	if errors.Is(err, store.ErrNotFound) {
		return usersv1.GetCalendarFeedResponse_builder{}.Build(), nil
	}
	if err != nil {
		s.report.Report(ctx, "failed to get calendar feed", err)
		return nil, connect.NewError(connect.CodeInternal, "failed to get calendar feed").WithCause(err)
	}
	return usersv1.GetCalendarFeedResponse_builder{Feed: calendarFeedToProto(feed, "")}.Build(), nil
}

// CreateCalendarFeed creates the user's calendar feed, replacing any old
// one, and returns its URL.
func (s *Service) CreateCalendarFeed(ctx context.Context, req *usersv1.CreateCalendarFeedRequest) (*usersv1.CreateCalendarFeedResponse, error) {
	user, err := authn.GetSessionUser(ctx)
	if err != nil {
		return nil, err
	}
	feed, secret, err := s.feeds.CreateCalendarFeed(ctx, user.ID())
	if err != nil {
		s.report.Report(ctx, "failed to create calendar feed", err)
		return nil, connect.NewError(connect.CodeInternal, "failed to create calendar feed").WithCause(err)
	}
	url := s.baseURL + CalendarPath + secret + ".ics"
	return usersv1.CreateCalendarFeedResponse_builder{Feed: calendarFeedToProto(feed, url)}.Build(), nil
}

// DeleteCalendarFeed deletes the user's calendar feed.
func (s *Service) DeleteCalendarFeed(ctx context.Context, req *usersv1.DeleteCalendarFeedRequest) (*usersv1.DeleteCalendarFeedResponse, error) {
	user, err := authn.GetSessionUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.feeds.DeleteCalendarFeed(ctx, user.ID()); err != nil {
		s.report.Report(ctx, "failed to delete calendar feed", err)
		return nil, connect.NewError(connect.CodeInternal, "failed to delete calendar feed").WithCause(err)
	}
	return usersv1.DeleteCalendarFeedResponse_builder{}.Build(), nil
}

func calendarFeedToProto(feed *users.CalendarFeed, url string) *usersv1.CalendarFeed {
	b := usersv1.CalendarFeed_builder{
		CreateTime: timestamppb.New(feed.CreateTime.Time),
		Url:        url,
	}
	if feed.LastUsedTime.Valid {
		b.LastUsedTime = timestamppb.New(feed.LastUsedTime.Time)
	}
	return b.Build()
}
