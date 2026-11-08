package plan

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/mealplann.ing/internal/authn"
	"github.com/bs-tools/mealplann.ing/internal/foods"
	peoplev1 "github.com/bs-tools/mealplann.ing/internal/gen/people/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	"github.com/bs-tools/mealplann.ing/internal/gen/plans/v1/plansv1connect"
	"github.com/bs-tools/mealplann.ing/internal/pantry"
	"github.com/bs-tools/mealplann.ing/internal/people"
	"github.com/bs-tools/mealplann.ing/internal/plans"
	"github.com/bs-tools/mealplann.ing/internal/protomask"
	"github.com/bs-tools/mealplann.ing/internal/recipes"
	"github.com/bs-tools/mealplann.ing/internal/services/person"
	"github.com/bs-tools/mealplann.ing/internal/shopping"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/bs-tools/mealplann.ing/internal/templates"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var _ plansv1connect.PlanTemplateServiceHandler = (*Service)(nil)

// TemplateRepository stores plan templates.
type TemplateRepository interface {
	CreateTemplate(ctx context.Context, t *templates.Template) (*templates.Template, error)
	GetTemplate(ctx context.Context, householdID, id pgtype.UUID) (*templates.Template, error)
	ListTemplates(ctx context.Context, householdID pgtype.UUID) ([]*templates.Template, error)
	UpdateTemplate(ctx context.Context, t *templates.Template) (*templates.Template, error)
	DeleteTemplate(ctx context.Context, householdID, id pgtype.UUID) error
}

// Limits of a template's fields.
const (
	maxTemplateNameLength   = 200
	maxTemplateSourceLength = 500
	maxTemplateNotesLength  = 5000
	maxTemplateDays         = 28
	defaultTemplateDays     = 7
	maxTemplateSlots        = 20
	maxSlotNameLength       = 100
	maxSlotKeyLength        = 40
	maxTemplateEntries      = 500
	maxEntryKeyLength       = 60
	maxEntryServings        = 20
	maxTemplateBatches      = 200
)

const templateNotFound = "the template doesn't exist in your household; find templates with ListPlanTemplates"

// keyPattern is what slot and entry keys look like.
var keyPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// templateKeepFields are the fields of a template updates don't change.
var templateKeepFields = []protoreflect.Name{"id", "create_time", "update_time"}

func (s *Service) CreatePlanTemplate(ctx context.Context, req *plansv1.CreatePlanTemplateRequest) (*plansv1.CreatePlanTemplateResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	t, err := templateFromProto(req.GetTemplate())
	if err != nil {
		return nil, err
	}
	t.HouseholdID = user.HouseholdID()
	names, err := s.checkTemplateReferences(ctx, t)
	if err != nil {
		return nil, err
	}
	created, err := s.templates.CreateTemplate(ctx, t)
	if err != nil {
		return nil, s.templateError(ctx, err, "failed to create template", t.Name)
	}
	return plansv1.CreatePlanTemplateResponse_builder{Template: templateToProto(created, names)}.Build(), nil
}

func (s *Service) CreatePlanTemplateFromPlan(ctx context.Context, req *plansv1.CreatePlanTemplateFromPlanRequest) (*plansv1.CreatePlanTemplateFromPlanResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	name, source, err := checkTemplateHeader(req.GetName(), req.GetSource())
	if err != nil {
		return nil, err
	}
	start, err := parseDate("start_date", req.GetStartDate())
	if err != nil {
		return nil, err
	}
	days := req.GetDays()
	if days == 0 {
		days = defaultTemplateDays
	}
	if days < 1 || days > maxTemplateDays {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "days must be from 1 to %d, or 0 for %d", maxTemplateDays, defaultTemplateDays)
	}
	d, err := s.loadPlan(ctx, user.HouseholdID(), start, start.AddDate(0, 0, int(days)-1))
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to read plan", "")
	}
	t := templates.FromPlan(d.plan, d.people, start, days)
	if len(t.Entries) == 0 {
		return nil, connect.Errorf(connect.CodeFailedPrecondition,
			"nothing is planned with a recipe from %s for %d days: plan some meals first, or create the template with CreatePlanTemplate",
			store.FormatDate(start), days)
	}
	t.HouseholdID = user.HouseholdID()
	t.Name = name
	t.Source = source
	names := map[pgtype.UUID]string{}
	for id, r := range d.recipes {
		names[id] = r.Name
	}
	created, err := s.templates.CreateTemplate(ctx, t)
	if err != nil {
		return nil, s.templateError(ctx, err, "failed to create template", t.Name)
	}
	return plansv1.CreatePlanTemplateFromPlanResponse_builder{Template: templateToProto(created, names)}.Build(), nil
}

func (s *Service) GetPlanTemplate(ctx context.Context, req *plansv1.GetPlanTemplateRequest) (*plansv1.GetPlanTemplateResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := parseID("id", req.GetId(), "find templates with ListPlanTemplates")
	if err != nil {
		return nil, err
	}
	t, err := s.templates.GetTemplate(ctx, user.HouseholdID(), id)
	if err != nil {
		return nil, s.templateError(ctx, err, "failed to get template", "")
	}
	names, err := s.recipeNames(ctx, user.HouseholdID(), t)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to read recipes", "")
	}
	return plansv1.GetPlanTemplateResponse_builder{Template: templateToProto(t, names)}.Build(), nil
}

func (s *Service) ListPlanTemplates(ctx context.Context, _ *plansv1.ListPlanTemplatesRequest) (*plansv1.ListPlanTemplatesResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	list, err := s.templates.ListTemplates(ctx, user.HouseholdID())
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to list templates", "")
	}
	names, err := s.recipeNames(ctx, user.HouseholdID(), list...)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to read recipes", "")
	}
	summaries := make([]*plansv1.PlanTemplateSummary, 0, len(list))
	for _, t := range list {
		var recipeNames []string
		for _, e := range t.Entries {
			if name := names[e.RecipeID]; name != "" && !slices.Contains(recipeNames, name) {
				recipeNames = append(recipeNames, name)
			}
		}
		summaries = append(summaries, plansv1.PlanTemplateSummary_builder{
			Id:          store.UUIDToString(t.ID),
			Name:        t.Name,
			Source:      t.Source,
			Days:        t.Days,
			EntryCount:  int32(len(t.Entries)),
			RecipeNames: recipeNames,
			UpdateTime:  timestamppb.New(t.UpdateTime),
		}.Build())
	}
	return plansv1.ListPlanTemplatesResponse_builder{Templates: summaries}.Build(), nil
}

func (s *Service) UpdatePlanTemplate(ctx context.Context, req *plansv1.UpdatePlanTemplateRequest) (*plansv1.UpdatePlanTemplateResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := parseID("template.id", req.GetTemplate().GetId(), "find templates with ListPlanTemplates")
	if err != nil {
		return nil, err
	}
	existing, err := s.templates.GetTemplate(ctx, user.HouseholdID(), id)
	if err != nil {
		return nil, s.templateError(ctx, err, "failed to get template", "")
	}
	merged := templateToProto(existing, nil)
	if err := protomask.Apply(merged, req.GetTemplate(), req.GetUpdateMask(), templateKeepFields...); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err.Error())
	}
	t, err := templateFromProto(merged)
	if err != nil {
		return nil, err
	}
	t.ID = existing.ID
	t.HouseholdID = existing.HouseholdID
	names, err := s.checkTemplateReferences(ctx, t)
	if err != nil {
		return nil, err
	}
	updated, err := s.templates.UpdateTemplate(ctx, t)
	if err != nil {
		return nil, s.templateError(ctx, err, "failed to update template", t.Name)
	}
	return plansv1.UpdatePlanTemplateResponse_builder{Template: templateToProto(updated, names)}.Build(), nil
}

func (s *Service) DeletePlanTemplate(ctx context.Context, req *plansv1.DeletePlanTemplateRequest) (*plansv1.DeletePlanTemplateResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := parseID("id", req.GetId(), "find templates with ListPlanTemplates")
	if err != nil {
		return nil, err
	}
	if err := s.templates.DeleteTemplate(ctx, user.HouseholdID(), id); err != nil {
		return nil, s.templateError(ctx, err, "failed to delete template", "")
	}
	return plansv1.DeletePlanTemplateResponse_builder{}.Build(), nil
}

func (s *Service) PreviewPlanTemplate(ctx context.Context, req *plansv1.PreviewPlanTemplateRequest) (*plansv1.PreviewPlanTemplateResponse, error) {
	result, err := s.applyTemplate(ctx, req.GetTemplateId(), req.GetStartDate(), req.GetPersonIds(), true)
	if err != nil {
		return nil, err
	}
	return plansv1.PreviewPlanTemplateResponse_builder{Result: result}.Build(), nil
}

func (s *Service) ApplyPlanTemplate(ctx context.Context, req *plansv1.ApplyPlanTemplateRequest) (*plansv1.ApplyPlanTemplateResponse, error) {
	result, err := s.applyTemplate(ctx, req.GetTemplateId(), req.GetStartDate(), req.GetPersonIds(), false)
	if err != nil {
		return nil, err
	}
	return plansv1.ApplyPlanTemplateResponse_builder{Result: result}.Build(), nil
}

// applyTemplate applies a template to people from a start date, or only
// shows what it would plan if dryRun. It reads the template, then the plan
// of its days with the people and its recipes, and then the foods and the
// pantry, and plans in one more round trip unless dryRun.
func (s *Service) applyTemplate(ctx context.Context, rawID, rawStart string, rawPeople []string, dryRun bool) (*plansv1.AppliedTemplate, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := parseID("template_id", rawID, "find templates with ListPlanTemplates")
	if err != nil {
		return nil, err
	}
	start, err := parseDate("start_date", rawStart)
	if err != nil {
		return nil, err
	}
	var personIDs []pgtype.UUID
	for _, raw := range rawPeople {
		pid, err := parseID("person_ids", raw, "find people with ListPeople")
		if err != nil {
			return nil, err
		}
		personIDs = append(personIDs, pid)
	}
	t, err := s.templates.GetTemplate(ctx, user.HouseholdID(), id)
	if err != nil {
		return nil, s.templateError(ctx, err, "failed to get template", "")
	}
	end := start.AddDate(0, 0, int(t.Days)-1)
	var recipeIDs []pgtype.UUID
	for _, e := range t.Entries {
		recipeIDs = append(recipeIDs, e.RecipeID)
	}
	// Leftovers can't reach past the template's days, but what's planned
	// the day after counts toward the last day's warnings.
	d, err := s.loadPlan(ctx, user.HouseholdID(), start, end.AddDate(0, 0, 1), unique(recipeIDs)...)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to read plan", "")
	}
	var missing []string
	for _, e := range t.Entries {
		if _, ok := d.recipes[e.RecipeID]; !ok {
			missing = append(missing, e.Key)
		}
	}
	if len(missing) > 0 {
		return nil, connect.Errorf(connect.CodeFailedPrecondition,
			"the recipes of entries %s were deleted: give them other recipes, or remove them, with UpdatePlanTemplate",
			strings.Join(missing, ", "))
	}
	ps := d.people
	if len(personIDs) > 0 {
		ps = nil
		for _, pid := range personIDs {
			i := slices.IndexFunc(d.people, func(p *people.Person) bool { return p.ID == pid })
			if i < 0 {
				return nil, connect.NewError(connect.CodeInvalidArgument, "person_ids has someone who isn't in your household; find people with ListPeople")
			}
			if !slices.Contains(ps, d.people[i]) {
				ps = append(ps, d.people[i])
			}
		}
	}
	if len(ps) == 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "your household has nobody to plan for: add people with PersonService.CreatePerson")
	}

	applied := templates.Apply(t, ps, start)
	for _, c := range applied.Cooks {
		c.Cook.HouseholdID = user.HouseholdID()
	}
	// The foods and the pantry are read while the cooks are created, for
	// what they need bought.
	var foodList []*foods.Food
	var lots []*pantry.Item
	var foodsErr, lotsErr error
	var wg sync.WaitGroup
	wg.Go(func() {
		var ids []pgtype.UUID
		for _, id := range unique(recipeIDs) {
			for _, in := range d.recipes[id].Ingredients {
				ids = append(ids, in.FoodID)
			}
		}
		foodList, foodsErr = s.foods.ListFoodsByID(ctx, user.HouseholdID(), unique(ids))
	})
	wg.Go(func() { lots, lotsErr = s.pantry.ListItems(ctx, user.HouseholdID()) })
	var cooks []*plans.Cook
	var portions [][]*plans.Portion
	if dryRun {
		for _, c := range applied.Cooks {
			cook := *c.Cook
			cook.ID = store.NewUUID()
			cook.RecipeName = d.recipes[cook.RecipeID].Name
			var list []*plans.Portion
			for _, p := range c.Portions {
				portion := *p
				portion.ID = store.NewUUID()
				portion.CookID = cook.ID
				portion.Name = cook.RecipeName
				portion.CookDate = cook.Date
				portion.CookRecipeID = cook.RecipeID
				list = append(list, &portion)
			}
			cooks = append(cooks, &cook)
			portions = append(portions, list)
		}
	} else if len(applied.Cooks) > 0 {
		cooks, portions, err = s.plans.CreateCooksWithPortions(ctx, applied.Cooks)
	}
	wg.Wait()
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to apply template", "")
	}
	for _, err := range []error{foodsErr, lotsErr} {
		if err != nil {
			return nil, s.toConnectError(ctx, err, "failed to read foods", "")
		}
	}

	// The warnings are those of the eating times the portions are at, with
	// what was planned there already, and of the cooks.
	result := plansv1.AppliedTemplate_builder{EndDate: store.FormatDate(end)}
	for i, c := range cooks {
		c.AssignedServings = 0
		for _, p := range portions[i] {
			c.AssignedServings += p.Servings
			d.addItem(&item{portion: p, date: p.Date})
		}
		d.plan.Cooks = append(d.plan.Cooks, c)
	}
	var checked []plans.Slot
	for i, c := range cooks {
		result.Cooks = append(result.Cooks, cookToProto(c))
		for _, p := range portions[i] {
			result.Portions = append(result.Portions, portionToProto(p))
			slot := plans.Slot{PersonID: p.PersonID, EatingTimeID: p.EatingTimeID, Date: p.Date}
			if slices.Contains(checked, slot) {
				continue
			}
			checked = append(checked, slot)
			eater := ps[slices.IndexFunc(ps, func(e *people.Person) bool { return e.ID == p.PersonID })]
			result.Warnings = append(result.Warnings, d.slot(eater, eater.EatingTime(p.EatingTimeID), p.Date).proto.GetWarnings()...)
		}
		result.Warnings = append(result.Warnings, d.plannedCook(c).GetWarnings()...)
	}
	for _, skip := range applied.Skips {
		result.Skipped = append(result.Skipped, skip.String())
	}
	for _, key := range applied.Unused {
		result.Skipped = append(result.Skipped, fmt.Sprintf("Nobody eats entry %s, so it isn't planned", key))
	}

	foodsByID := map[pgtype.UUID]*foods.Food{}
	for _, f := range foodList {
		foodsByID[f.ID] = f
	}
	recipesByID := map[pgtype.UUID]*recipes.Recipe{}
	for id, r := range d.recipes {
		recipesByID[id] = r
	}
	items := shopping.Generate(&shopping.Input{
		ShopDate: start,
		Needs:    shopping.NeedsOf(&plans.Plan{Cooks: cooks}, recipesByID),
		Foods:    foodsByID,
		Lots:     lots,
	})
	for _, it := range items {
		result.Shopping = append(result.Shopping, plansv1.TemplateShoppingItem_builder{
			FoodId:     store.UUIDToString(it.FoodID),
			FoodName:   it.Name,
			AmountText: it.AmountText,
			TopUp:      it.TopUp,
		}.Build())
	}
	return result.Build(), nil
}

// checkTemplateReferences checks that a template's recipes and people are
// the household's, and returns the recipes' names by ID.
func (s *Service) checkTemplateReferences(ctx context.Context, t *templates.Template) (map[pgtype.UUID]string, error) {
	var recipeIDs []pgtype.UUID
	var personIDs []pgtype.UUID
	for _, e := range t.Entries {
		recipeIDs = append(recipeIDs, e.RecipeID)
		for _, ps := range e.PersonServings {
			personIDs = append(personIDs, ps.PersonID)
		}
	}
	var found []*recipes.Recipe
	var all []*people.Person
	var recipesErr, peopleErr error
	var wg sync.WaitGroup
	wg.Go(func() { found, recipesErr = s.recipes.ListRecipesByID(ctx, t.HouseholdID, unique(recipeIDs)) })
	if len(personIDs) > 0 {
		wg.Go(func() { all, peopleErr = s.people.ListPeople(ctx, t.HouseholdID) })
	}
	wg.Wait()
	for _, err := range []error{recipesErr, peopleErr} {
		if err != nil {
			return nil, s.toConnectError(ctx, err, "failed to read the template's recipes and people", "")
		}
	}
	names := map[pgtype.UUID]string{}
	for _, r := range found {
		names[r.ID] = r.Name
	}
	for _, e := range t.Entries {
		if _, ok := names[e.RecipeID]; !ok {
			return nil, connect.Errorf(connect.CodeInvalidArgument,
				"the recipe of entry %s isn't in your household; find recipes with ListRecipes, or create it with CreateRecipe", e.Key)
		}
		for _, ps := range e.PersonServings {
			if !slices.ContainsFunc(all, func(p *people.Person) bool { return p.ID == ps.PersonID }) {
				return nil, connect.Errorf(connect.CodeInvalidArgument,
					"entry %s has servings for someone who isn't in your household; find people with ListPeople", e.Key)
			}
		}
	}
	return names, nil
}

// recipeNames returns the names of the templates' recipes by ID, without
// those deleted.
func (s *Service) recipeNames(ctx context.Context, householdID pgtype.UUID, list ...*templates.Template) (map[pgtype.UUID]string, error) {
	var ids []pgtype.UUID
	for _, t := range list {
		for _, e := range t.Entries {
			ids = append(ids, e.RecipeID)
		}
	}
	names := map[pgtype.UUID]string{}
	if len(ids) == 0 {
		return names, nil
	}
	found, err := s.recipes.ListRecipesByID(ctx, householdID, unique(ids))
	if err != nil {
		return nil, err
	}
	for _, r := range found {
		names[r.ID] = r.Name
	}
	return names, nil
}

// templateError maps an error of the template repository to a Connect
// error.
func (s *Service) templateError(ctx context.Context, err error, msg, name string) error {
	if errors.Is(err, templates.ErrNameTaken) {
		return connect.Errorf(connect.CodeAlreadyExists,
			"your household already has a template named %q: choose another name, or update that template", name)
	}
	return s.toConnectError(ctx, err, msg, templateNotFound)
}

// checkTemplateHeader checks and trims a template's name and source.
func checkTemplateHeader(rawName, rawSource string) (string, string, error) {
	name := strings.TrimSpace(rawName)
	source := strings.TrimSpace(rawSource)
	switch {
	case name == "":
		return "", "", connect.NewError(connect.CodeInvalidArgument, `name is required: name the template, e.g. "Dietitian week 1"`)
	case utf8.RuneCountInString(name) > maxTemplateNameLength:
		return "", "", connect.Errorf(connect.CodeInvalidArgument, "name must be at most %d characters", maxTemplateNameLength)
	case utf8.RuneCountInString(source) > maxTemplateSourceLength:
		return "", "", connect.Errorf(connect.CodeInvalidArgument, "source must be at most %d characters", maxTemplateSourceLength)
	}
	return name, source, nil
}

// templateFromProto converts and validates a template, other than whether
// its recipes and people are the household's. Its ID, household and times
// are left for the caller to set.
func templateFromProto(p *plansv1.PlanTemplate) (*templates.Template, error) {
	name, source, err := checkTemplateHeader(p.GetName(), p.GetSource())
	if err != nil {
		return nil, err
	}
	notes, err := checkNotes("notes", p.GetNotes(), maxTemplateNotesLength)
	if err != nil {
		return nil, err
	}
	t := &templates.Template{Name: name, Source: source, Notes: notes, Days: p.GetDays()}
	if t.Days < 1 || t.Days > maxTemplateDays {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "days must be from 1 to %d", maxTemplateDays)
	}
	if n := len(p.GetSlots()); n == 0 || n > maxTemplateSlots {
		return nil, connect.Errorf(connect.CodeInvalidArgument,
			"slots must have from 1 to %d slots, e.g. one per shared meal and small meal", maxTemplateSlots)
	}
	for i, sp := range p.GetSlots() {
		field := fmt.Sprintf("slots[%d]", i)
		slot := &templates.Slot{Key: strings.TrimSpace(sp.GetKey()), Name: strings.TrimSpace(sp.GetName())}
		switch {
		case !keyPattern.MatchString(slot.Key) || len(slot.Key) > maxSlotKeyLength:
			return nil, connect.Errorf(connect.CodeInvalidArgument,
				"%s.key must be lowercase letters, digits and dashes, at most %d, e.g. \"dinner\" or \"small-2\"", field, maxSlotKeyLength)
		case t.Slot(slot.Key) != nil:
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.key %q is the key of another slot: keys are unique", field, slot.Key)
		case slot.Name == "" || utf8.RuneCountInString(slot.Name) > maxSlotNameLength:
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.name must be from 1 to %d characters", field, maxSlotNameLength)
		}
		kind, ok := person.KindsFromProto[sp.GetKind()]
		if !ok {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.kind is required: EATING_TIME_KIND_MAIN or EATING_TIME_KIND_SMALL", field)
		}
		slot.Kind = kind
		if sp.GetMeal() != peoplev1.HouseholdMeal_HOUSEHOLD_MEAL_UNSPECIFIED {
			slot.Meal, err = mealFromProto(sp.GetMeal())
			if err != nil {
				return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.meal must be a shared meal, or unspecified", field)
			}
			if slices.ContainsFunc(t.Slots, func(other *templates.Slot) bool { return other.Meal == slot.Meal }) {
				return nil, connect.Errorf(connect.CodeInvalidArgument, "%s is the second slot for %s: give each shared meal one slot", field, slot.Meal)
			}
		}
		t.Slots = append(t.Slots, slot)
	}
	if len(p.GetEntries()) > maxTemplateEntries {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "entries must have at most %d entries", maxTemplateEntries)
	}
	for i, ep := range p.GetEntries() {
		field := fmt.Sprintf("entries[%d]", i)
		e := &templates.Entry{Key: strings.TrimSpace(ep.GetKey()), Day: ep.GetDay(), SlotKey: strings.TrimSpace(ep.GetSlotKey()), Servings: ep.GetServings()}
		if e.Day < 0 || e.Day >= t.Days {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.day must be from 0 to %d, as the template has %d days", field, t.Days-1, t.Days)
		}
		if t.Slot(e.SlotKey) == nil {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.slot_key %q isn't one of the template's slots", field, e.SlotKey)
		}
		if e.RecipeID, err = parseID(field+".recipe_id", ep.GetRecipeId(), "find recipes with ListRecipes"); err != nil {
			return nil, err
		}
		if e.Servings == 0 {
			e.Servings = 1
		}
		if math.IsNaN(e.Servings) || e.Servings < 0 || e.Servings > maxEntryServings {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.servings must be more than 0 and at most %d, or 0 for 1", field, maxEntryServings)
		}
		if e.Key == "" {
			e.Key = templates.NewKey(t, e)
		}
		switch {
		case !keyPattern.MatchString(e.Key) || len(e.Key) > maxEntryKeyLength:
			return nil, connect.Errorf(connect.CodeInvalidArgument,
				"%s.key must be lowercase letters, digits and dashes, at most %d, or empty for one made of its day and slot", field, maxEntryKeyLength)
		case t.Entry(e.Key) != nil:
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.key %q is the key of another entry: keys are unique", field, e.Key)
		}
		for j, ps := range ep.GetPersonServings() {
			sub := fmt.Sprintf("%s.person_servings[%d]", field, j)
			pid, err := parseID(sub+".person_id", ps.GetPersonId(), "find people with ListPeople")
			if err != nil {
				return nil, err
			}
			if slices.ContainsFunc(e.PersonServings, func(other templates.PersonServings) bool { return other.PersonID == pid }) {
				return nil, connect.Errorf(connect.CodeInvalidArgument, "%s is the second for the same person: give each person one", sub)
			}
			if v := ps.GetServings(); math.IsNaN(v) || v < 0 || v > maxEntryServings {
				return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.servings must be from 0 to %d, with 0 for someone who doesn't eat it", sub, maxEntryServings)
			}
			e.PersonServings = append(e.PersonServings, templates.PersonServings{PersonID: pid, Servings: ps.GetServings()})
		}
		t.Entries = append(t.Entries, e)
	}
	if len(p.GetBatches()) > maxTemplateBatches {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "batches must have at most %d batches", maxTemplateBatches)
	}
	batchOf := map[string]int{}
	for i, bp := range p.GetBatches() {
		field := fmt.Sprintf("batches[%d]", i)
		b := &templates.Batch{Day: bp.GetDay(), Servings: bp.GetServings()}
		if b.Day < 0 || b.Day >= t.Days {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.day must be from 0 to %d, as the template has %d days", field, t.Days-1, t.Days)
		}
		if math.IsNaN(b.Servings) || b.Servings < 0 || b.Servings > maxCookServings {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.servings must be from 0 to %d, with 0 for the servings its entries plan", field, maxCookServings)
		}
		if len(bp.GetEntryKeys()) == 0 {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.entry_keys must have at least one entry", field)
		}
		var recipeID pgtype.UUID
		for _, key := range bp.GetEntryKeys() {
			e := t.Entry(key)
			switch {
			case e == nil:
				return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.entry_keys has %q, which isn't one of the template's entries", field, key)
			case e.Day < b.Day:
				return nil, connect.Errorf(connect.CodeInvalidArgument,
					"%s cooks on day %d, after entry %s on day %d eats it: cook it on or before its entries' days", field, b.Day, key, e.Day)
			case recipeID.Valid && e.RecipeID != recipeID:
				return nil, connect.Errorf(connect.CodeInvalidArgument, "%s has entries of different recipes: batch entries of one recipe", field)
			}
			if other, ok := batchOf[key]; ok {
				return nil, connect.Errorf(connect.CodeInvalidArgument, "entry %s is in batches[%d] and %s: put it in one", key, other, field)
			}
			batchOf[key] = i
			recipeID = e.RecipeID
			b.EntryKeys = append(b.EntryKeys, key)
		}
		t.Batches = append(t.Batches, b)
	}
	return t, nil
}

// templateToProto converts a template, with the names of its recipes by
// ID, or nil to leave them out.
func templateToProto(t *templates.Template, names map[pgtype.UUID]string) *plansv1.PlanTemplate {
	slots := make([]*plansv1.TemplateSlot, 0, len(t.Slots))
	for _, s := range t.Slots {
		slots = append(slots, plansv1.TemplateSlot_builder{
			Key:  s.Key,
			Name: s.Name,
			Kind: person.KindsToProto[s.Kind],
			Meal: person.MealsToProto[s.Meal],
		}.Build())
	}
	entries := make([]*plansv1.TemplateEntry, 0, len(t.Entries))
	for _, e := range t.Entries {
		var ps []*plansv1.PersonServings
		for _, s := range e.PersonServings {
			ps = append(ps, plansv1.PersonServings_builder{PersonId: store.UUIDToString(s.PersonID), Servings: s.Servings}.Build())
		}
		entries = append(entries, plansv1.TemplateEntry_builder{
			Key:            e.Key,
			Day:            e.Day,
			SlotKey:        e.SlotKey,
			RecipeId:       store.UUIDToString(e.RecipeID),
			RecipeName:     names[e.RecipeID],
			Servings:       e.Servings,
			PersonServings: ps,
		}.Build())
	}
	batches := make([]*plansv1.TemplateBatch, 0, len(t.Batches))
	for _, b := range t.Batches {
		var recipeID pgtype.UUID
		if e := t.Entry(b.EntryKeys[0]); e != nil {
			recipeID = e.RecipeID
		}
		batches = append(batches, plansv1.TemplateBatch_builder{
			Day:        b.Day,
			Servings:   b.Servings,
			EntryKeys:  b.EntryKeys,
			RecipeId:   store.UUIDToString(recipeID),
			RecipeName: names[recipeID],
		}.Build())
	}
	return plansv1.PlanTemplate_builder{
		Id:         store.UUIDToString(t.ID),
		Name:       t.Name,
		Source:     t.Source,
		Notes:      t.Notes,
		Days:       t.Days,
		Slots:      slots,
		Entries:    entries,
		Batches:    batches,
		CreateTime: timestamppb.New(t.CreateTime),
		UpdateTime: timestamppb.New(t.UpdateTime),
	}.Build()
}
