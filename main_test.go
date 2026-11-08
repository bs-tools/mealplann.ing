package main

import (
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSetFlagsFromEnv(t *testing.T) {
	// Secrets added from a command's output end with a newline, which must
	// not become part of the value.
	t.Setenv("MEALPLANNING_ADMIN_BOOTSTRAP_TOKEN", "0123abcd\n")
	t.Setenv("MEALPLANNING_PORT", " 9090 ")
	// Flags set on the command line win over the environment.
	t.Setenv("MEALPLANNING_LOG_LEVEL", "debug")

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	token := fs.String("admin-bootstrap-token", "", "")
	port := fs.String("port", "8080", "")
	logLevel := fs.String("log-level", "info", "")
	unset := fs.String("public-url", "default", "")
	if err := fs.Parse([]string{"-log-level", "warn"}); err != nil {
		t.Fatal(err)
	}
	if err := setFlagsFromEnv(fs); err != nil {
		t.Fatalf("setFlagsFromEnv: %v", err)
	}
	if *token != "0123abcd" {
		t.Errorf("admin bootstrap token = %q, want it without the newline", *token)
	}
	if *port != "9090" {
		t.Errorf("port = %q, want 9090", *port)
	}
	if *logLevel != "warn" {
		t.Errorf("log level = %q, want the command line's warn", *logLevel)
	}
	if *unset != "default" {
		t.Errorf("public URL = %q, want its default", *unset)
	}
}

func TestRedirectToPublicURL(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	h, err := redirectToPublicURL(next, "https://mealplann.ing", []string{" foodplann.ing", "", "www.foodplann.ing"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		method, url  string
		wantCode     int
		wantLocation string
	}{
		{http.MethodGet, "https://foodplann.ing/invite?token=abc", http.StatusMovedPermanently, "https://mealplann.ing/invite?token=abc"},
		{http.MethodHead, "https://FOODPLANN.ING:443/", http.StatusMovedPermanently, "https://mealplann.ing/"},
		{http.MethodPost, "https://www.foodplann.ing/mcp", http.StatusPermanentRedirect, "https://mealplann.ing/mcp"},
		{http.MethodGet, "https://foodplann.ing/recipes/a%2Fb", http.StatusMovedPermanently, "https://mealplann.ing/recipes/a%2Fb"},
		{http.MethodGet, "https://mealplann.ing/plans", http.StatusTeapot, ""},
		{http.MethodGet, "https://foodplanning-abc-uw.a.run.app/", http.StatusTeapot, ""},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.url, nil))
		if rec.Code != tt.wantCode {
			t.Errorf("%s %s: code = %d, want %d", tt.method, tt.url, rec.Code, tt.wantCode)
		}
		if got := rec.Header().Get("Location"); got != tt.wantLocation {
			t.Errorf("%s %s: location = %q, want %q", tt.method, tt.url, got, tt.wantLocation)
		}
	}

	if _, err := redirectToPublicURL(next, "https://mealplann.ing", []string{"mealplann.ing"}); err == nil {
		t.Error("redirecting the public URL's own host succeeded, want an error")
	}
}
