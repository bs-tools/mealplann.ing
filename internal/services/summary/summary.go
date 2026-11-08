// Package summary trims API responses to what an agent usually needs, for
// requests with summary set. Full responses of a week's plan or a whole
// pantry run to tens of kilobytes, mostly nutrients agents don't read, and
// overflow the tool output of some MCP clients.
package summary

import (
	"slices"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// mainNutrients are the nutrients a summary keeps, by field name: those
// the household's rules are mostly about.
var mainNutrients = []protoreflect.Name{"energy_kcal", "protein_grams", "carbohydrate_grams"}

// dropped are the fields a summary leaves out, by name, wherever they are:
// detail for reading one thing closely, not for scanning many.
var dropped = []protoreflect.Name{
	"steps",
	"original_text",
	"per_hundred_grams",
	"incomplete_nutrients",
	"issues",
	"create_time",
	"update_time",
}

// Summarize trims the message in place: it keeps only the main nutrients
// (energy, protein and carbohydrate) of every foods.v1.Nutrients, and
// clears the steps of recipes, the original text of ingredients, the
// nutrition per 100 g, incomplete nutrients and ingredient issues, and
// create and update times.
func Summarize(m proto.Message) {
	summarize(m.ProtoReflect())
}

func summarize(m protoreflect.Message) {
	// Fields are cleared after ranging over them, as clearing fields while
	// ranging is undefined.
	var clear []protoreflect.FieldDescriptor
	nutrients := m.Descriptor().FullName() == "foods.v1.Nutrients"
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		switch {
		case nutrients:
			if !slices.Contains(mainNutrients, fd.Name()) {
				clear = append(clear, fd)
			}
		case slices.Contains(dropped, fd.Name()):
			clear = append(clear, fd)
		case fd.IsMap():
			if fd.MapValue().Message() != nil {
				v.Map().Range(func(_ protoreflect.MapKey, mv protoreflect.Value) bool {
					summarize(mv.Message())
					return true
				})
			}
		case fd.Message() == nil:
		case fd.IsList():
			list := v.List()
			for i := range list.Len() {
				summarize(list.Get(i).Message())
			}
		default:
			summarize(v.Message())
		}
		return true
	})
	for _, fd := range clear {
		m.Clear(fd)
	}
}
