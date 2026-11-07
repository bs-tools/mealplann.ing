package services_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"math/rand/v2"
	"net/http"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/foodplann.ing/internal/apilimit"
	usersv1 "github.com/bs-tools/foodplann.ing/internal/gen/users/v1"
)

// noisePNG returns a PNG image of random pixels, which don't compress, so
// it's larger than apilimit.MaxRequestBytes.
func noisePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 700, 700))
	rng := rand.New(rand.NewPCG(1, 2))
	for y := range 700 {
		for x := range 700 {
			img.Set(x, y, color.RGBA{uint8(rng.UintN(256)), uint8(rng.UintN(256)), uint8(rng.UintN(256)), 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if buf.Len() <= apilimit.MaxRequestBytes {
		t.Fatalf("the picture is %d bytes, want more than %d", buf.Len(), apilimit.MaxRequestBytes)
	}
	return buf.Bytes()
}

func TestRequestLimits(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()

	// Requests larger than a megabyte are rejected before they're read.
	_, err := e.users.CreateAPIToken(ctx, usersv1.CreateAPITokenRequest_builder{
		Name: strings.Repeat("a", apilimit.MaxRequestBytes),
	}.Build())
	wantCode(t, err, connect.CodeResourceExhausted)
	if err == nil || !strings.Contains(err.Error(), "send less data") {
		t.Errorf("got error %v, want it to say to send less data", err)
	}

	// Signed in users can upload larger files, like a picture from a phone.
	picture := noisePNG(t)
	uploaded, err := e.users.UploadAvatar(ctx, usersv1.UploadAvatarRequest_builder{Data: picture}.Build())
	if err != nil {
		t.Fatalf("UploadAvatar of a large picture: %v", err)
	}
	if uploaded.GetUser().GetAvatarUrl() == "" {
		t.Error("the user has no avatar after uploading one")
	}

	// Callers that aren't signed in can't, and are told to sign in without
	// reading their request.
	_, err = e.anonUsers.UploadAvatar(ctx, usersv1.UploadAvatarRequest_builder{Data: picture}.Build())
	wantCode(t, err, connect.CodeUnauthenticated)
	_, err = e.anonUsers.UploadAvatar(ctx, usersv1.UploadAvatarRequest_builder{Data: []byte("small")}.Build())
	wantCode(t, err, connect.CodeUnauthenticated)

	// Requests with more values than any needs, which take far more memory
	// decoded than encoded, are rejected before they're decoded, however
	// small. No request of the API has a repeated field yet, so the values
	// are in a field JSON decoding would reject, which they're counted
	// before.
	body := `{"name":"t","values":[` + strings.TrimSuffix(strings.Repeat("{},", apilimit.MaxRequestValues+1), ",") + `]}`
	if len(body) > apilimit.MaxRequestBytes {
		t.Fatalf("the request is %d bytes, want at most %d", len(body), apilimit.MaxRequestBytes)
	}
	req, err := http.NewRequest(http.MethodPost, e.srv.URL+"/users.v1.UserService/CreateAPIToken", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "__Host-session="+e.session)
	resp, err := e.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var answer bytes.Buffer
	if _, err := answer.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(answer.String(), "invalid_argument") || !strings.Contains(answer.String(), "send less data") {
		t.Errorf("got %d %s, want an invalid argument error saying to send less data", resp.StatusCode, answer.String())
	}
}
