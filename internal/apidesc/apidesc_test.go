package apidesc

import (
	"strings"
	"testing"

	authv1 "github.com/bs-tools/foodplann.ing/internal/gen/auth/v1"
	"github.com/bs-tools/foodplann.ing/internal/gen/auth/v1/authv1connect"
	mcpv1 "github.com/bs-tools/foodplann.ing/internal/gen/mcp/v1"
	_ "github.com/bs-tools/foodplann.ing/internal/gen/oauth/v1"
	usersv1 "github.com/bs-tools/foodplann.ing/internal/gen/users/v1"
	"github.com/bs-tools/foodplann.ing/internal/gen/users/v1/usersv1connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// TestUpToDate checks that the embedded descriptor set matches the generated
// Go code. If it fails, run `make buf`.
func TestUpToDate(t *testing.T) {
	fs, err := Files()
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	fs.RangeFiles(func(embedded protoreflect.FileDescriptor) bool {
		if strings.HasPrefix(embedded.Path(), "google/") {
			// The well-known types are bundled with buf and protobuf-go, and
			// their versions may differ.
			return true
		}
		generated, err := protoregistry.GlobalFiles.FindFileByPath(embedded.Path())
		if err != nil {
			// Not linked into the test binary.
			return true
		}
		checked++
		want := protodesc.ToFileDescriptorProto(generated)
		got := protodesc.ToFileDescriptorProto(embedded)
		for _, fdp := range []*descriptorpb.FileDescriptorProto{want, got} {
			// The generated code has file options (e.g. go_package) set by
			// buf managed mode, and no source info.
			fdp.Options = nil
			fdp.SourceCodeInfo = nil
		}
		if !proto.Equal(got, want) {
			t.Errorf("embedded descriptor of %s does not match the generated code; run `make buf`", embedded.Path())
		}
		return true
	})
	if checked < 5 {
		t.Errorf("only checked %d files", checked)
	}
}

func TestService(t *testing.T) {
	sd, err := Service(authv1connect.AuthServiceName)
	if err != nil {
		t.Fatal(err)
	}
	if got := sd.Methods().ByName("CreateInvitation"); got == nil {
		t.Fatal("CreateInvitation method not found")
	}
	if _, err := Service("auth.v1.Invitation"); err == nil {
		t.Error("expected error for non-service descriptor")
	}
	if _, err := Service("does.not.Exist"); err == nil {
		t.Error("expected error for unknown service")
	}
}

func TestImports(t *testing.T) {
	fs, err := Files()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"google/protobuf/timestamp.proto",
		"google/protobuf/descriptor.proto",
		"mcp/v1/options.proto",
	} {
		if _, err := fs.FindFileByPath(path); err != nil {
			t.Errorf("finding %s: %v", path, err)
		}
	}
}

func TestOptionsResolved(t *testing.T) {
	sd, err := Service(usersv1connect.UserServiceName)
	if err != nil {
		t.Fatal(err)
	}
	md := sd.Methods().ByName("CreateAPIToken")
	opts := md.Options().(*descriptorpb.MethodOptions)
	if len(opts.ProtoReflect().GetUnknown()) != 0 {
		t.Error("method options have unresolved unknown fields")
	}
	ext := proto.GetExtension(opts, mcpv1.E_Method).(*mcpv1.MethodOptions)
	if !ext.GetExclude() {
		t.Error("CreateAPIToken is not excluded")
	}

	files, err := Files()
	if err != nil {
		t.Fatal(err)
	}
	d, err := files.FindDescriptorByName("users.v1.Household.id")
	if err != nil {
		t.Fatal(err)
	}
	fopts := d.(protoreflect.FieldDescriptor).Options().(*descriptorpb.FieldOptions)
	if !proto.GetExtension(fopts, mcpv1.E_Field).(*mcpv1.FieldOptions).GetOutputOnly() {
		t.Error("users.v1.Household.id is not output only")
	}
}

func TestComment(t *testing.T) {
	tests := []struct {
		name string
		desc protoreflect.Descriptor
		want string
	}{
		{
			name: "method from generated code",
			desc: authv1.File_auth_v1_auth_proto.Services().Get(0).Methods().ByName("ListPasskeys"),
			want: "List the signed in user's passkeys.",
		},
		{
			name: "field",
			desc: (&usersv1.UpdateHouseholdRequest{}).ProtoReflect().Descriptor().Fields().ByName("name"),
			want: "The new name of the household, from 1 to 100 characters.",
		},
		{
			name: "enum value",
			desc: authv1.InvitationKind_INVITATION_KIND_ADD_PASSKEY.Descriptor().Values().ByNumber(2),
			want: "Add a passkey to an existing user, e.g. on another device.",
		},
		{
			name: "no comment",
			desc: (&authv1.ListPasskeysRequest{}).ProtoReflect().Descriptor(),
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Comment(tt.desc); got != tt.want {
				t.Errorf("Comment() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCleanComment(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "empty",
			in:   "",
			want: "",
		},
		{
			name: "single line",
			in:   " Get a route by ID.\n",
			want: "Get a route by ID.",
		},
		{
			name: "collapses lines",
			in:   " Compute a route through the\n given waypoints.\n",
			want: "Compute a route through the given waypoints.",
		},
		{
			name: "keeps paragraphs",
			in:   " First paragraph\n continued.\n\n Second paragraph.\n",
			want: "First paragraph continued.\n\nSecond paragraph.",
		},
		{
			name: "keeps lists",
			in:   " Levels:\n\n 1. Months: the phase\n    and volume.\n 2. Weeks.\n - Bullet\n * Star\n",
			want: "Levels:\n\n1. Months: the phase and volume.\n2. Weeks.\n- Bullet\n* Star",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cleanComment(tt.in); got != tt.want {
				t.Errorf("cleanComment(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
