package main

import (
	"flag"
	"io"
	"testing"
)

func TestSetFlagsFromEnv(t *testing.T) {
	// Secrets added from a command's output end with a newline, which must
	// not become part of the value.
	t.Setenv("FOODPLANNING_ADMIN_BOOTSTRAP_TOKEN", "0123abcd\n")
	t.Setenv("FOODPLANNING_PORT", " 9090 ")
	// Flags set on the command line win over the environment.
	t.Setenv("FOODPLANNING_LOG_LEVEL", "debug")

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
