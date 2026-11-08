package shopping

import (
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect/v2"
	shoppingv1 "github.com/bs-tools/mealplann.ing/internal/gen/shopping/v1"
	"github.com/bs-tools/mealplann.ing/internal/services/food"
	"github.com/bs-tools/mealplann.ing/internal/shopping"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func storeToProto(s *shopping.Store) *shoppingv1.Store {
	return shoppingv1.Store_builder{
		Id:         store.UUIDToString(s.ID),
		Name:       s.Name,
		Position:   s.Position,
		CreateTime: timestamppb.New(s.CreateTime),
		UpdateTime: timestamppb.New(s.UpdateTime),
	}.Build()
}

// storeFromProto converts and validates a store's name. Its other fields
// are left for the caller to set.
func storeFromProto(p *shoppingv1.Store) (*shopping.Store, error) {
	name := strings.TrimSpace(p.GetName())
	if name == "" || utf8.RuneCountInString(name) > maxStoreNameChars {
		return nil, connect.Errorf(connect.CodeInvalidArgument, `name must be from 1 to %d characters, e.g. "Supermarket"`, maxStoreNameChars)
	}
	return &shopping.Store{Name: name}, nil
}

// listToProto converts a list, with its items if it has them.
func listToProto(l *shopping.List) *shoppingv1.ShoppingList {
	items := make([]*shoppingv1.ShoppingItem, 0, len(l.Items))
	for _, it := range l.Items {
		items = append(items, itemToProto(it))
	}
	b := shoppingv1.ShoppingList_builder{
		Id:           store.UUIDToString(l.ID),
		StartDate:    store.FormatDate(l.StartDate),
		EndDate:      store.FormatDate(l.EndDate),
		Complete:     !l.CompleteTime.IsZero(),
		Items:        items,
		ItemCount:    l.ItemCount,
		CheckedCount: l.CheckedCount,
		TopUpCount:   l.TopUpCount,
		CreateTime:   timestamppb.New(l.CreateTime),
		UpdateTime:   timestamppb.New(l.UpdateTime),
	}
	if !l.CompleteTime.IsZero() {
		b.CompleteTime = timestamppb.New(l.CompleteTime)
	}
	return b.Build()
}

func itemToProto(it *shopping.Item) *shoppingv1.ShoppingItem {
	checkedBy := ""
	if it.Checked {
		checkedBy = it.CheckedByName
	}
	return shoppingv1.ShoppingItem_builder{
		Id:           store.UUIDToString(it.ID),
		FoodId:       optionalID(it.FoodID),
		Name:         it.Name,
		AmountText:   it.AmountText,
		Grams:        it.Grams,
		NeededGrams:  it.NeededGrams,
		PantryGrams:  it.PantryGrams,
		Packages:     it.Packages,
		StoreId:      optionalID(it.StoreID),
		StoreName:    it.StoreName,
		StoreSection: food.StoreSectionToProto(it.StoreSection),
		NeededByDate: store.FormatDate(it.NeededBy),
		TopUp:        it.TopUp,
		Warning:      it.Warning,
		Checked:      it.Checked,
		CheckedBy:    checkedBy,
		Note:         it.Note,
		Manual:       it.Manual,
	}.Build()
}

// optionalID returns the string of an ID, empty if it's invalid.
func optionalID(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return store.UUIDToString(id)
}

// parseID parses a reference, naming field in errors, with how to find a
// valid one.
func parseID(field, s, find string) (pgtype.UUID, error) {
	id, err := store.StringToUUID(strings.TrimSpace(s))
	if err != nil {
		return pgtype.UUID{}, connect.Errorf(connect.CodeInvalidArgument, "invalid %s: %v; %s", field, err, find).WithCause(err)
	}
	return id, nil
}

// parseDate parses a date, naming field in errors.
func parseDate(field, s string) (time.Time, error) {
	date, err := store.ParseDate(strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, connect.Errorf(connect.CodeInvalidArgument, `%s must be a date, "YYYY-MM-DD", e.g. "2026-11-07"`, field)
	}
	if date.Year() < minYear || date.Year() > maxYear {
		return time.Time{}, connect.Errorf(connect.CodeInvalidArgument, "%s must be from %d to %d", field, minYear, maxYear)
	}
	return date, nil
}

// checkText trims text and checks its length, naming field in errors.
func checkText(field, text string, most int) (string, error) {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > most {
		return "", connect.Errorf(connect.CodeInvalidArgument, "%s must be at most %d characters", field, most)
	}
	return text, nil
}
