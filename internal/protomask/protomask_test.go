package protomask_test

import (
	"slices"
	"testing"

	"github.com/bs-tools/foodplann.ing/internal/protomask"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// fixture returns the messages of a test file, as no API message has
// nested messages or oneofs to mask yet:
//
//	message Recipe { string id = 1; string name = 2; string cuisine = 3; string notes = 4; string source_url = 5; }
//	message Meal {
//	  string id = 1;
//	  string title = 2;
//	  oneof kind { Cook cook = 3; Snack snack = 4; }
//	}
//	message Cook { double servings = 1; repeated Step steps = 2; Portions portions = 3; bool batch = 4; }
//	message Step { int32 minutes = 1; }
//	message Portions { double regular_grams = 1; double small_grams = 2; }
//	message Snack { int32 grams = 1; }
func fixture(t *testing.T) protoreflect.FileDescriptor {
	t.Helper()
	field := func(name string, number int32, typ descriptorpb.FieldDescriptorProto_Type, typeName string) *descriptorpb.FieldDescriptorProto {
		f := &descriptorpb.FieldDescriptorProto{
			Name:   proto.String(name),
			Number: proto.Int32(number),
			Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:   typ.Enum(),
		}
		if typeName != "" {
			f.TypeName = proto.String(typeName)
		}
		return f
	}
	const (
		str = descriptorpb.FieldDescriptorProto_TYPE_STRING
		msg = descriptorpb.FieldDescriptorProto_TYPE_MESSAGE
		dbl = descriptorpb.FieldDescriptorProto_TYPE_DOUBLE
		i32 = descriptorpb.FieldDescriptorProto_TYPE_INT32
	)
	cook := field("cook", 3, msg, ".test.v1.Cook")
	cook.OneofIndex = proto.Int32(0)
	snack := field("snack", 4, msg, ".test.v1.Snack")
	snack.OneofIndex = proto.Int32(0)
	steps := field("steps", 2, msg, ".test.v1.Step")
	steps.Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
	fd, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:    proto.String("test/v1/protomask.proto"),
		Package: proto.String("test.v1"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("Recipe"),
				Field: []*descriptorpb.FieldDescriptorProto{
					field("id", 1, str, ""),
					field("name", 2, str, ""),
					field("cuisine", 3, str, ""),
					field("notes", 4, str, ""),
					field("source_url", 5, str, ""),
				},
			},
			{
				Name: proto.String("Meal"),
				Field: []*descriptorpb.FieldDescriptorProto{
					field("id", 1, str, ""),
					field("title", 2, str, ""),
					cook,
					snack,
				},
				OneofDecl: []*descriptorpb.OneofDescriptorProto{{Name: proto.String("kind")}},
			},
			{
				Name: proto.String("Cook"),
				Field: []*descriptorpb.FieldDescriptorProto{
					field("servings", 1, dbl, ""),
					steps,
					field("portions", 3, msg, ".test.v1.Portions"),
					field("batch", 4, descriptorpb.FieldDescriptorProto_TYPE_BOOL, ""),
				},
			},
			{Name: proto.String("Step"), Field: []*descriptorpb.FieldDescriptorProto{field("minutes", 1, i32, "")}},
			{
				Name: proto.String("Portions"),
				Field: []*descriptorpb.FieldDescriptorProto{
					field("regular_grams", 1, dbl, ""),
					field("small_grams", 2, dbl, ""),
				},
			},
			{Name: proto.String("Snack"), Field: []*descriptorpb.FieldDescriptorProto{field("grams", 1, i32, "")}},
		},
	}, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatal(err)
	}
	return fd
}

// newMessage returns a new message of the fixture with the fields set, by
// name: scalars, or messages of further fields as map[string]any, or lists
// of those as []map[string]any.
func newMessage(md protoreflect.MessageDescriptor, fields map[string]any) *dynamicpb.Message {
	m := dynamicpb.NewMessage(md)
	for name, v := range fields {
		fd := md.Fields().ByName(protoreflect.Name(name))
		switch v := v.(type) {
		case map[string]any:
			m.Set(fd, protoreflect.ValueOfMessage(newMessage(fd.Message(), v)))
		case []map[string]any:
			list := m.Mutable(fd).List()
			for _, item := range v {
				list.Append(protoreflect.ValueOfMessage(newMessage(fd.Message(), item)))
			}
		default:
			m.Set(fd, protoreflect.ValueOf(v))
		}
	}
	return m
}

// get returns the value of the field at the dotted path of m.
func get(m protoreflect.Message, path ...protoreflect.Name) protoreflect.Value {
	for _, name := range path[:len(path)-1] {
		m = m.Get(m.Descriptor().Fields().ByName(name)).Message()
	}
	return m.Get(m.Descriptor().Fields().ByName(path[len(path)-1]))
}

func has(m protoreflect.Message, path ...protoreflect.Name) bool {
	for _, name := range path[:len(path)-1] {
		m = m.Get(m.Descriptor().Fields().ByName(name)).Message()
	}
	return m.Has(m.Descriptor().Fields().ByName(path[len(path)-1]))
}

func TestApply(t *testing.T) {
	t.Parallel()
	md := fixture(t).Messages().ByName("Recipe")
	newDst := func() *dynamicpb.Message {
		return newMessage(md, map[string]any{"id": "1", "name": "Old", "cuisine": "Thai", "notes": "Notes"})
	}
	src := newMessage(md, map[string]any{"id": "2", "name": "New", "cuisine": ""})
	str := func(m *dynamicpb.Message, name protoreflect.Name) string {
		return get(m, name).String()
	}

	dst := newDst()
	if err := protomask.Apply(dst, src, &fieldmaskpb.FieldMask{Paths: []string{"name", "cuisine"}}, "id"); err != nil {
		t.Fatal(err)
	}
	if str(dst, "id") != "1" || str(dst, "name") != "New" || str(dst, "cuisine") != "" || str(dst, "notes") != "Notes" {
		t.Errorf("unexpected result %v", dst)
	}

	dst = newDst()
	if err := protomask.Apply(dst, src, &fieldmaskpb.FieldMask{Paths: []string{"sourceUrl"}}); err != nil {
		t.Errorf("JSON name path: %v", err)
	}

	dst = newDst()
	if err := protomask.Apply(dst, src, nil, "id"); err != nil {
		t.Fatal(err)
	}
	if str(dst, "id") != "1" || str(dst, "name") != "New" || str(dst, "notes") != "" {
		t.Errorf("unexpected result for empty mask %v", dst)
	}

	for _, path := range []string{"nope", "id"} {
		if err := protomask.Apply(newDst(), src, &fieldmaskpb.FieldMask{Paths: []string{path}}, "id"); err == nil {
			t.Errorf("expected error for path %q", path)
		}
	}
}

func TestFields(t *testing.T) {
	t.Parallel()
	md := fixture(t).Messages().ByName("Recipe")
	names := func(mask *fieldmaskpb.FieldMask) ([]string, error) {
		fields, err := protomask.Fields(md, mask, "id")
		var result []string
		for _, fd := range fields {
			result = append(result, string(fd.Name()))
		}
		return result, err
	}

	got, err := names(&fieldmaskpb.FieldMask{Paths: []string{"notes", "sourceUrl", "notes"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"notes", "source_url"}; !slices.Equal(got, want) {
		t.Errorf("got fields %v, want %v", got, want)
	}

	got, err = names(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != md.Fields().Len()-1 || slices.Contains(got, "id") {
		t.Errorf("unexpected fields for empty mask %v", got)
	}

	for _, path := range []string{"nope", "id"} {
		if _, err := names(&fieldmaskpb.FieldMask{Paths: []string{path}}); err == nil {
			t.Errorf("expected error for path %q", path)
		}
	}
}

func TestApplyNested(t *testing.T) {
	t.Parallel()
	md := fixture(t).Messages().ByName("Meal")
	newDst := func() *dynamicpb.Message {
		return newMessage(md, map[string]any{"id": "1", "title": "Dinner", "cook": map[string]any{
			"servings": 4.0,
			"steps":    []map[string]any{{"minutes": int32(20)}},
			"portions": map[string]any{"regular_grams": 400.0, "small_grams": 150.0},
		}})
	}
	// The request carries only the field to change.
	src := newMessage(md, map[string]any{"cook": map[string]any{
		"batch": true, "portions": map[string]any{"regular_grams": 450.0},
	}})

	dst := newDst()
	if err := protomask.Apply(dst, src, &fieldmaskpb.FieldMask{Paths: []string{"cook.batch", "cook.portions.regular_grams"}}, "id"); err != nil {
		t.Fatal(err)
	}
	if !get(dst, "cook", "batch").Bool() || get(dst, "cook", "portions", "regular_grams").Float() != 450 ||
		get(dst, "cook", "portions", "small_grams").Float() != 150 || get(dst, "cook", "steps").List().Len() != 1 ||
		get(dst, "cook", "servings").Float() != 4 || get(dst, "title").String() != "Dinner" {
		t.Errorf("got %v, want only the batch flag and the regular portion changed", dst)
	}

	// A field missing from the request is cleared.
	dst = newDst()
	if err := protomask.Apply(dst, dynamicpb.NewMessage(md), &fieldmaskpb.FieldMask{Paths: []string{"cook.portions"}}); err != nil {
		t.Fatal(err)
	}
	if has(dst, "cook", "portions") || get(dst, "cook", "steps").List().Len() != 1 {
		t.Errorf("got %v, want only the portions cleared", dst)
	}

	for _, path := range []string{
		// Not a field, or not a message.
		"cook.nope", "title.length", "cook.steps.minutes",
		// A kind of meal dst isn't: it's switched by replacing the whole
		// field.
		"snack.grams",
		// Within a field that's never copied.
		"id.value",
	} {
		if err := protomask.Apply(newDst(), src, &fieldmaskpb.FieldMask{Paths: []string{path}}, "id"); err == nil {
			t.Errorf("expected an error for path %q", path)
		}
	}
}
