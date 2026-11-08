package apilimit

import (
	"strings"
	"testing"

	usersv1 "github.com/bs-tools/mealplann.ing/internal/gen/users/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestCountBinary(t *testing.T) {
	t.Parallel()
	tokens := func(n int, withTime bool) []*usersv1.APIToken {
		tokens := make([]*usersv1.APIToken, n)
		for i := range tokens {
			b := usersv1.APIToken_builder{}
			if withTime {
				b.CreateTime = timestamppb.Now()
			}
			tokens[i] = b.Build()
		}
		return tokens
	}
	for name, tt := range map[string]struct {
		msg     proto.Message
		wantErr bool
	}{
		"empty": {msg: usersv1.ListAPITokensResponse_builder{}.Build()},
		"at most values": {
			msg: usersv1.ListAPITokensResponse_builder{Tokens: tokens(MaxRequestValues, false)}.Build(),
		},
		"too many messages": {
			msg:     usersv1.ListAPITokensResponse_builder{Tokens: tokens(MaxRequestValues+1, false)}.Build(),
			wantErr: true,
		},
		// The tokens and their creation times.
		"too many nested messages": {
			msg:     usersv1.ListAPITokensResponse_builder{Tokens: tokens(MaxRequestValues/2+1, true)}.Build(),
			wantErr: true,
		},
		// A large string is one value.
		"large string": {
			msg: usersv1.CreateAPITokenRequest_builder{Name: strings.Repeat("token ", 100_000)}.Build(),
		},
		// Packed scalars take no more memory decoded than encoded. No API
		// message has a repeated scalar, so this takes a location's path.
		"packed scalars": {
			msg: &descriptorpb.SourceCodeInfo_Location{Path: make([]int32, 2*MaxRequestValues)},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data, err := proto.Marshal(tt.msg)
			if err != nil {
				t.Fatal(err)
			}
			if err := countBinary(data, tt.msg.ProtoReflect().Descriptor()); (err != nil) != tt.wantErr {
				t.Errorf("countBinary returned %v, want error %v", err, tt.wantErr)
			}
		})
	}
}

func TestCountJSON(t *testing.T) {
	t.Parallel()
	repeat := func(s string, n int) string {
		return strings.TrimSuffix(strings.Repeat(s+",", n), ",")
	}
	for name, tt := range map[string]struct {
		json    string
		wantErr bool
	}{
		"empty objects":             {json: `{"tokens":[` + repeat("{}", MaxRequestValues-1) + `]}`},
		"too many empty objects":    {json: `{"tokens":[` + repeat("{}", MaxRequestValues) + `]}`, wantErr: true},
		"keys aren't counted":       {json: `{"tokens":[` + repeat(`{"id":1,"name":2}`, MaxRequestValues-1) + `]}`},
		"too many strings":          {json: `{"names":[` + repeat(`"TRAIL"`, MaxRequestValues) + `]}`, wantErr: true},
		"strings with spaces":       {json: `{"names": [` + repeat(` "TRAIL" `, MaxRequestValues) + `]}`, wantErr: true},
		"numbers aren't counted":    {json: `{"counts":[` + repeat("1", 2*MaxRequestValues) + `]}`},
		"braces in strings":         {json: `{"name":"` + strings.Repeat(`{\"}`, MaxRequestValues) + `"}`},
		"escaped quotes in strings": {json: `{"name":"` + strings.Repeat(`\"\\`, MaxRequestValues) + `"}`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := countJSON([]byte(tt.json), nil); (err != nil) != tt.wantErr {
				t.Errorf("countJSON returned %v, want error %v", err, tt.wantErr)
			}
		})
	}
}
