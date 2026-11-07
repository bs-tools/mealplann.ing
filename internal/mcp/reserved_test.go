package mcp

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// removedFieldsFixture returns a request whose nested messages had fields
// removed, as no API message has yet:
//
//	message CreateWorkoutsRequest { string plan_id = 1; repeated Workout workouts = 2; }
//	message Workout {
//	  reserved 2, 3;
//	  reserved "target_distance_meters", "route_id";
//	  string date = 1;
//	  Run run = 4;
//	}
//	message Run { int32 target_distance_meters = 1; }
func removedFieldsFixture(t *testing.T) *descriptorpb.FileDescriptorProto {
	t.Helper()
	field := func(name string, number int32, typ descriptorpb.FieldDescriptorProto_Type, typeName string, label descriptorpb.FieldDescriptorProto_Label) *descriptorpb.FieldDescriptorProto {
		f := &descriptorpb.FieldDescriptorProto{
			Name:   proto.String(name),
			Number: proto.Int32(number),
			Label:  label.Enum(),
			Type:   typ.Enum(),
		}
		if typeName != "" {
			f.TypeName = proto.String(typeName)
		}
		return f
	}
	const (
		optional = descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
		repeated = descriptorpb.FieldDescriptorProto_LABEL_REPEATED
		str      = descriptorpb.FieldDescriptorProto_TYPE_STRING
		msg      = descriptorpb.FieldDescriptorProto_TYPE_MESSAGE
	)
	return &descriptorpb.FileDescriptorProto{
		Name:    proto.String("test/v1/reserved.proto"),
		Package: proto.String("test.v1"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("CreateWorkoutsRequest"),
				Field: []*descriptorpb.FieldDescriptorProto{
					field("plan_id", 1, str, "", optional),
					field("workouts", 2, msg, ".test.v1.Workout", repeated),
				},
			},
			{
				Name: proto.String("Workout"),
				Field: []*descriptorpb.FieldDescriptorProto{
					field("date", 1, str, "", optional),
					field("run", 4, msg, ".test.v1.Run", optional),
				},
				ReservedRange: []*descriptorpb.DescriptorProto_ReservedRange{{Start: proto.Int32(2), End: proto.Int32(4)}},
				ReservedName:  []string{"target_distance_meters", "route_id"},
			},
			{
				Name: proto.String("Run"),
				Field: []*descriptorpb.FieldDescriptorProto{
					field("target_distance_meters", 1, descriptorpb.FieldDescriptorProto_TYPE_INT32, "", optional),
				},
			},
		},
	}
}

func TestCheckRemovedFields(t *testing.T) {
	t.Parallel()
	fd, err := protodesc.NewFile(removedFieldsFixture(t), protoregistry.GlobalFiles)
	if err != nil {
		t.Fatal(err)
	}
	md := fd.Messages().ByName("CreateWorkoutsRequest")
	for _, tc := range []struct {
		args, wantField string
	}{
		{`{"planId": "p", "workouts": [{"date": "2026-11-09", "run": {"targetDistanceMeters": 10000}}]}`, ""},
		// The fields moved into run, and agents use both spellings.
		{`{"workouts": [{"date": "2026-11-09"}, {"targetDistanceMeters": 10000}]}`, "targetDistanceMeters"},
		{`{"workouts": [{"route_id": "r"}]}`, "route_id"},
		// Fields that never existed are left for protojson to report.
		{`{"workouts": [{"nonsense": 1}]}`, ""},
		{`"not an object"`, ""},
	} {
		err := checkRemovedFields([]byte(tc.args), md)
		switch {
		case tc.wantField == "" && err != nil:
			t.Errorf("%s: got %v, want no error", tc.args, err)
		case tc.wantField != "" && (err == nil || !strings.Contains(err.Error(), "Workout no longer has the field "+tc.wantField)):
			t.Errorf("%s: got %v, want an error about %s", tc.args, err, tc.wantField)
		}
	}
}
