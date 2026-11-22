package people

import (
	"context"
	"testing"

	"github.com/bs-tools/mealplann.ing/internal/rules"
	"github.com/jackc/pgx/v5/pgtype"
)

type fakeLister []*Person

func (f fakeLister) ListPeople(context.Context, pgtype.UUID) ([]*Person, error) {
	return f, nil
}

// TestWithoutGoals checks that people read without goals have no rules or
// targets, and keep the rest, while the lister's own people keep theirs.
func TestWithoutGoals(t *testing.T) {
	t.Parallel()
	limit := 500.0
	ben := &Person{
		Name:    "Ben",
		Rules:   rules.Rules{MaxPortionGrams: &limit, WarnFastCarbs: true},
		Targets: []rules.Target{{}},
		Profile: Profile{HeightCm: 180},
	}
	got, err := WithoutGoals(fakeLister{ben}).ListPeople(context.Background(), pgtype.UUID{})
	if err != nil {
		t.Fatalf("ListPeople: %v", err)
	}
	if len(got) != 1 || got[0].Rules != (rules.Rules{}) || got[0].Targets != nil {
		t.Errorf("got %+v, want Ben without rules or targets", got)
	}
	if got[0].Name != "Ben" || got[0].Profile.HeightCm != 180 {
		t.Errorf("got %+v, want Ben's name and profile kept", got[0])
	}
	if ben.Rules.MaxPortionGrams == nil || len(ben.Targets) != 1 {
		t.Errorf("got %+v for the lister's own Ben, want his goals kept", ben)
	}
}
