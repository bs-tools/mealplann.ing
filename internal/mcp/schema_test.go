package mcp

import (
	"encoding/json"
	"math"
	"slices"
	"strings"
	"testing"

	// Register the types of the services the tests don't otherwise use, so
	// every API method's schema can be validated.
	_ "github.com/bs-tools/foodplann.ing/internal/gen/auth/v1"
	_ "github.com/bs-tools/foodplann.ing/internal/gen/oauth/v1"

	"github.com/bs-tools/foodplann.ing/internal/apidesc"
	mcpv1 "github.com/bs-tools/foodplann.ing/internal/gen/mcp/v1"
	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	_ "google.golang.org/protobuf/types/known/fieldmaskpb"
	_ "google.golang.org/protobuf/types/known/timestamppb"
)

// findMessage returns the descriptor of a message from the embedded
// descriptor set.
func findMessage(t *testing.T, name protoreflect.FullName) protoreflect.MessageDescriptor {
	t.Helper()
	files, err := apidesc.Files()
	if err != nil {
		t.Fatal(err)
	}
	d, err := files.FindDescriptorByName(name)
	if err != nil {
		t.Fatalf("finding %s: %v", name, err)
	}
	return d.(protoreflect.MessageDescriptor)
}

func propertyNames(s *jsonschema.Schema) []string {
	return s.PropertyOrder
}

// fixture returns a message of a test file with the features the schemas
// handle that the API doesn't use yet: oneofs, proto3 optional fields,
// output only fields on inputs, update masks and deprecated enum values. It
// has no comments, as only the embedded descriptor set has them.
//
//	enum Kind { KIND_UNSPECIFIED = 0; KIND_A = 1; KIND_OLD = 2 [deprecated = true]; KIND_B = 3; }
//	message Item {
//	  string id = 1 [(mcp.v1.field).output_only = true];
//	  string name = 2;
//	  google.protobuf.Timestamp create_time = 3 [(mcp.v1.field).output_only = true];
//	  repeated Item children = 4;
//	}
//	message CreateRequest {
//	  oneof source { Item item = 1; string text = 2; string item_id = 3; }
//	  bool include = 4;
//	  Kind kind = 5;
//	  repeated Kind kinds = 6;
//	  optional string note = 7;
//	}
//	message UpdateRequest { Item item = 1; google.protobuf.FieldMask update_mask = 2; }
func fixture(t *testing.T, name protoreflect.Name) protoreflect.MessageDescriptor {
	t.Helper()
	outputOnly := func() *descriptorpb.FieldOptions {
		opts := &descriptorpb.FieldOptions{}
		proto.SetExtension(opts, mcpv1.E_Field, mcpv1.FieldOptions_builder{OutputOnly: true}.Build())
		return opts
	}
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
		enm = descriptorpb.FieldDescriptorProto_TYPE_ENUM
	)
	repeated := func(f *descriptorpb.FieldDescriptorProto) *descriptorpb.FieldDescriptorProto {
		f.Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
		return f
	}
	inOneof := func(f *descriptorpb.FieldDescriptorProto, index int32) *descriptorpb.FieldDescriptorProto {
		f.OneofIndex = proto.Int32(index)
		return f
	}
	id := field("id", 1, str, "")
	id.Options = outputOnly()
	createTime := field("create_time", 3, msg, ".google.protobuf.Timestamp")
	createTime.Options = outputOnly()
	note := inOneof(field("note", 7, str, ""), 1)
	note.Proto3Optional = proto.Bool(true)
	value := func(name string, number int32, deprecated bool) *descriptorpb.EnumValueDescriptorProto {
		v := &descriptorpb.EnumValueDescriptorProto{Name: proto.String(name), Number: proto.Int32(number)}
		if deprecated {
			v.Options = &descriptorpb.EnumValueOptions{Deprecated: proto.Bool(true)}
		}
		return v
	}
	fd, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:       proto.String("test/v1/test.proto"),
		Package:    proto.String("test.v1"),
		Syntax:     proto.String("proto3"),
		Dependency: []string{"mcp/v1/options.proto", "google/protobuf/timestamp.proto", "google/protobuf/field_mask.proto"},
		EnumType: []*descriptorpb.EnumDescriptorProto{{
			Name: proto.String("Kind"),
			Value: []*descriptorpb.EnumValueDescriptorProto{
				value("KIND_UNSPECIFIED", 0, false),
				value("KIND_A", 1, false),
				value("KIND_OLD", 2, true),
				value("KIND_B", 3, false),
			},
		}},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("Item"),
				Field: []*descriptorpb.FieldDescriptorProto{
					id,
					field("name", 2, str, ""),
					createTime,
					repeated(field("children", 4, msg, ".test.v1.Item")),
				},
			},
			{
				Name: proto.String("CreateRequest"),
				Field: []*descriptorpb.FieldDescriptorProto{
					inOneof(field("item", 1, msg, ".test.v1.Item"), 0),
					inOneof(field("text", 2, str, ""), 0),
					inOneof(field("item_id", 3, str, ""), 0),
					field("include", 4, descriptorpb.FieldDescriptorProto_TYPE_BOOL, ""),
					field("kind", 5, enm, ".test.v1.Kind"),
					repeated(field("kinds", 6, enm, ".test.v1.Kind")),
					note,
				},
				OneofDecl: []*descriptorpb.OneofDescriptorProto{
					{Name: proto.String("source")},
					{Name: proto.String("_note")},
				},
			},
			{
				Name: proto.String("UpdateRequest"),
				Field: []*descriptorpb.FieldDescriptorProto{
					field("item", 1, msg, ".test.v1.Item"),
					field("update_mask", 2, msg, ".google.protobuf.FieldMask"),
				},
			},
		},
	}, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatal(err)
	}
	md := fd.Messages().ByName(name)
	if md == nil {
		t.Fatalf("no message %s in the fixture", name)
	}
	return md
}

func TestInputSchemaOneof(t *testing.T) {
	s := newInputSchema(fixture(t, "CreateRequest"))
	if s.Type != "object" {
		t.Errorf("type = %q, want object", s.Type)
	}
	want := []string{"item", "text", "itemId", "include", "kind", "kinds", "note"}
	if got := propertyNames(s); !slices.Equal(got, want) {
		t.Errorf("properties = %v, want %v", got, want)
	}

	text := s.Properties["text"]
	if text.Type != "string" {
		t.Errorf("text type = %q, want string", text.Type)
	}
	if want := `Mutually exclusive with "item", "itemId" (oneof "source")`; !strings.Contains(text.Description, want) {
		t.Errorf("text description %q does not contain %q", text.Description, want)
	}

	item := s.Properties["item"]
	if item.Type != "object" {
		t.Fatalf("item type = %q, want object", item.Type)
	}
	list := item.Properties["children"]
	if list.Type != "array" || list.Items.Type != "object" {
		t.Fatalf("item.children = %+v, want array of objects", list)
	}

	kind := s.Properties["kind"]
	if kind.Type != "string" {
		t.Errorf("kind type = %q, want string", kind.Type)
	}
	if wantEnum := []any{"KIND_A", "KIND_B"}; !slices.Equal(kind.Enum, wantEnum) {
		t.Errorf("kind enum = %v, want %v", kind.Enum, wantEnum)
	}
	kinds := s.Properties["kinds"]
	if kinds == nil || kinds.Type != "array" || kinds.Items.Type != "string" {
		t.Fatalf("kinds = %+v, want array of strings", kinds)
	}
	if !slices.Contains(kinds.Items.Enum, any("KIND_B")) {
		t.Errorf("kinds enum = %v, want the kinds", kinds.Items.Enum)
	}
}

func TestInputSchemaOptional(t *testing.T) {
	s := newInputSchema(fixture(t, "CreateRequest"))
	note := s.Properties["note"]
	if note == nil || note.Type != "string" {
		t.Fatalf("note = %+v, want string", note)
	}
	if strings.Contains(note.Description, "Mutually exclusive") {
		t.Errorf("proto3 optional field described as oneof: %q", note.Description)
	}
}

func TestInputSchemaOutputOnly(t *testing.T) {
	s := newInputSchema(fixture(t, "UpdateRequest"))
	mask := s.Properties["updateMask"]
	if mask.Type != "string" || !strings.Contains(mask.Description, "comma-separated list of lowerCamelCase field paths") {
		t.Errorf("updateMask = %+v, want comma-separated string", mask)
	}
	item := s.Properties["item"]
	for _, name := range []string{"id", "createTime"} {
		if _, ok := item.Properties[name]; ok {
			t.Errorf("output only field item.%s is in the input schema", name)
		}
	}
	if _, ok := item.Properties["name"]; !ok {
		t.Error("item.name is missing from the input schema")
	}

	// Output only fields are hidden at any depth.
	var check func(path string, s *jsonschema.Schema)
	check = func(path string, s *jsonschema.Schema) {
		for name, p := range s.Properties {
			if name == "id" || name == "createTime" {
				t.Errorf("output only field %s.%s is in the input schema", path, name)
			}
			check(path+"."+name, p)
		}
		if s.Items != nil {
			check(path+"[]", s.Items)
		}
	}
	check("CreateRequest", newInputSchema(fixture(t, "CreateRequest")))

	// But they are part of the output schema.
	out := newOutputSchema(fixture(t, "UpdateRequest")).Properties["item"]
	for _, name := range []string{"id", "name", "createTime"} {
		if _, ok := out.Properties[name]; !ok {
			t.Errorf("item.%s is missing from the output schema", name)
		}
	}
	if ct := out.Properties["createTime"]; ct.Type != "string" || ct.Format != "date-time" {
		t.Errorf("item.createTime = %+v, want date-time string", ct)
	}

	// And in the API: a household's ID and members are output only.
	household := newInputSchema(findMessage(t, "users.v1.Household"))
	if got := propertyNames(household); !slices.Equal(got, []string{"name"}) {
		t.Errorf("household input properties = %v, want [name]", got)
	}
}

func TestOutputSchemaEnumIncludesZero(t *testing.T) {
	s := newOutputSchema(findMessage(t, "auth.v1.GetInvitationResponse"))
	kind := s.Properties["invitation"].Properties["kind"]
	if !slices.Contains(kind.Enum, any("INVITATION_KIND_UNSPECIFIED")) {
		t.Errorf("output enum %v does not contain the zero value", kind.Enum)
	}
	if !strings.Contains(kind.Description, "- INVITATION_KIND_ADD_PASSKEY: Add a passkey to an existing user") {
		t.Errorf("kind description %q does not describe enum values", kind.Description)
	}
}

func TestSchemaEnumOmitsDeprecated(t *testing.T) {
	md := fixture(t, "CreateRequest")
	kinds := newInputSchema(md).Properties["kind"].Enum
	if !slices.Contains(kinds, any("KIND_B")) || slices.Contains(kinds, any("KIND_OLD")) {
		t.Errorf("kinds %v, want only the current ones", kinds)
	}
	// Outputs may still have them.
	if kinds := newOutputSchema(md).Properties["kind"].Enum; !slices.Contains(kinds, any("KIND_OLD")) {
		t.Errorf("output kinds %v, want the deprecated ones too", kinds)
	}
}

func TestSchemaFieldKinds(t *testing.T) {
	// descriptor.proto has fields of most kinds, as well as recursive
	// messages.
	md := findMessage(t, "google.protobuf.FieldDescriptorProto")
	in := newInputSchema(md)
	if got := in.Properties["number"].Type; got != "integer" {
		t.Errorf("int32 type = %q, want integer", got)
	}
	if got := in.Properties["proto3Optional"].Type; got != "boolean" {
		t.Errorf("bool type = %q, want boolean", got)
	}

	uninterpreted := findMessage(t, "google.protobuf.UninterpretedOption")
	in = newInputSchema(uninterpreted)
	out := newOutputSchema(uninterpreted)
	if got := in.Properties["positiveIntValue"].Types; !slices.Equal(got, []string{"integer", "string"}) {
		t.Errorf("uint64 types = %v, want integer and string", got)
	}
	if got := in.Properties["negativeIntValue"].Types; !slices.Equal(got, []string{"integer", "string"}) {
		t.Errorf("int64 types = %v, want integer and string", got)
	}
	if got := in.Properties["doubleValue"].Type; got != "number" {
		t.Errorf("double input type = %q, want number", got)
	}
	if got := out.Properties["doubleValue"].AnyOf; len(got) != 2 {
		t.Errorf("double output anyOf = %v, want number or non-finite string", got)
	}
	if got := in.Properties["stringValue"]; got.Type != "string" || got.ContentEncoding != "base64" {
		t.Errorf("bytes = %+v, want base64 string", got)
	}

	// DescriptorProto is recursive through nested_type.
	desc := newInputSchema(findMessage(t, "google.protobuf.DescriptorProto"))
	nested := desc.Properties["nestedType"]
	if nested.Type != "array" || nested.Items.Type != "object" || len(nested.Items.Properties) != 0 {
		t.Errorf("recursive nestedType = %+v, want array of property-less objects", nested)
	}
}

// fill sets every field of a message to a non-zero value, recursing into
// messages up to a fixed depth. If nonFinite is set, floating point fields
// are set to non-finite values.
func fill(t *testing.T, m protoreflect.Message, depth int, nonFinite bool) {
	t.Helper()
	if depth > 5 {
		return
	}
	switch m.Descriptor().FullName() {
	case "google.protobuf.Any", "google.protobuf.Struct", "google.protobuf.Value",
		"google.protobuf.ListValue":
		return
	case "google.protobuf.FieldMask":
		paths := m.Descriptor().Fields().ByName("paths")
		list := m.Mutable(paths).List()
		list.Append(protoreflect.ValueOfString("foo"))
		list.Append(protoreflect.ValueOfString("bar_baz"))
		return
	case "google.protobuf.Timestamp", "google.protobuf.Duration":
		m.Set(m.Descriptor().Fields().ByName("seconds"), protoreflect.ValueOfInt64(1700000000))
		m.Set(m.Descriptor().Fields().ByName("nanos"), protoreflect.ValueOfInt32(500))
		return
	}
	fields := m.Descriptor().Fields()
	for i := range fields.Len() {
		fd := fields.Get(i)
		switch {
		case fd.IsMap():
			mp := m.Mutable(fd).Map()
			key := scalarValue(fd.MapKey(), nonFinite).MapKey()
			if isMessage(fd.MapValue()) {
				v := mp.NewValue()
				fill(t, v.Message(), depth+1, nonFinite)
				mp.Set(key, v)
			} else {
				mp.Set(key, scalarValue(fd.MapValue(), nonFinite))
			}
		case fd.IsList():
			list := m.Mutable(fd).List()
			for range 2 {
				if isMessage(fd) {
					v := list.NewElement()
					fill(t, v.Message(), depth+1, nonFinite)
					list.Append(v)
				} else {
					list.Append(scalarValue(fd, nonFinite))
				}
			}
		case isMessage(fd):
			fill(t, m.Mutable(fd).Message(), depth+1, nonFinite)
		default:
			m.Set(fd, scalarValue(fd, nonFinite))
		}
	}
}

func scalarValue(fd protoreflect.FieldDescriptor, nonFinite bool) protoreflect.Value {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		return protoreflect.ValueOfBool(true)
	case protoreflect.StringKind:
		return protoreflect.ValueOfString("foo")
	case protoreflect.BytesKind:
		return protoreflect.ValueOfBytes([]byte("\xff\x00bytes"))
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return protoreflect.ValueOfInt32(-42)
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return protoreflect.ValueOfUint32(42)
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return protoreflect.ValueOfInt64(-1 << 60)
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return protoreflect.ValueOfUint64(1 << 63)
	case protoreflect.FloatKind:
		if nonFinite {
			return protoreflect.ValueOfFloat32(float32(math.Inf(-1)))
		}
		return protoreflect.ValueOfFloat32(1.5)
	case protoreflect.DoubleKind:
		if nonFinite {
			return protoreflect.ValueOfFloat64(math.NaN())
		}
		return protoreflect.ValueOfFloat64(-2.25)
	case protoreflect.EnumKind:
		// The last value that isn't deprecated, as input schemas leave
		// deprecated values out.
		values := fd.Enum().Values()
		for i := values.Len() - 1; i > 0; i-- {
			if opts, ok := values.Get(i).Options().(*descriptorpb.EnumValueOptions); !ok || !opts.GetDeprecated() {
				return protoreflect.ValueOfEnum(values.Get(i).Number())
			}
		}
		return protoreflect.ValueOfEnum(values.Get(values.Len() - 1).Number())
	default:
		panic("unexpected kind " + fd.Kind().String())
	}
}

// validate checks that the protojson encoding of m validates against s.
func validate(t *testing.T, s *jsonschema.Schema, m protoreflect.Message) {
	t.Helper()
	resolved, err := s.Resolve(nil)
	if err != nil {
		t.Fatalf("resolving schema: %v", err)
	}
	data, err := protojson.Marshal(m.Interface())
	if err != nil {
		t.Fatalf("marshaling: %v", err)
	}
	var instance any
	if err := json.Unmarshal(data, &instance); err != nil {
		t.Fatal(err)
	}
	if err := resolved.Validate(instance); err != nil {
		t.Errorf("%s does not validate against its schema: %v\n%s", m.Descriptor().FullName(), err, data)
	}
}

// TestSchemasValidate checks that the protojson encoding of fully populated
// request and response messages of every method validates against the
// generated schemas.
func TestSchemasValidate(t *testing.T) {
	files, err := apidesc.Files()
	if err != nil {
		t.Fatal(err)
	}
	var services []protoreflect.ServiceDescriptor
	files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		for i := range fd.Services().Len() {
			services = append(services, fd.Services().Get(i))
		}
		return true
	})
	if len(services) == 0 {
		t.Fatal("no services found")
	}
	for _, sd := range services {
		for i := range sd.Methods().Len() {
			md := sd.Methods().Get(i)
			t.Run(string(md.FullName()), func(t *testing.T) {
				for _, nonFinite := range []bool{false, true} {
					inType, err := protoregistry.GlobalTypes.FindMessageByName(md.Input().FullName())
					if err != nil {
						t.Fatal(err)
					}
					in := inType.New()
					fill(t, in, 0, nonFinite)
					clearOutputOnly(in)
					if !nonFinite {
						// Non-finite numbers aren't part of the input schema.
						validate(t, newInputSchema(md.Input()), in)
					}

					outType, err := protoregistry.GlobalTypes.FindMessageByName(md.Output().FullName())
					if err != nil {
						t.Fatal(err)
					}
					out := outType.New()
					fill(t, out, 0, nonFinite)
					validate(t, newOutputSchema(md.Output()), out)
				}
			})
		}
	}
}
