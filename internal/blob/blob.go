// Package blob stores files, such as photos, in a local directory or a
// Google Cloud Storage bucket.
package blob

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
)

// ErrNotFound is returned when an object doesn't exist.
var ErrNotFound = errors.New("object not found")

// Store stores objects by key.
type Store interface {
	// Put stores an object, replacing any object with the same key.
	Put(ctx context.Context, key string, data []byte, contentType string) error
	// Get returns the contents of an object, or ErrNotFound.
	Get(ctx context.Context, key string) ([]byte, error)
	// Delete deletes an object. Deleting an object that doesn't exist is not
	// an error.
	Delete(ctx context.Context, key string) error
}

// keyRe matches valid keys: slash separated segments of letters, digits,
// dots, dashes and underscores, that don't start with a dot.
var keyRe = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9._-]*(/[A-Za-z0-9_-][A-Za-z0-9._-]*)*$`)

// ValidateKey checks that a key is safe to use as a file path and an object
// name.
func ValidateKey(key string) error {
	if len(key) > 512 || !keyRe.MatchString(key) {
		return fmt.Errorf("invalid object key %q", key)
	}
	return nil
}

// PutJSON stores a value as gzipped JSON, as kept caches are: JSON
// compresses to a fraction of its size, and Cloud Storage bills by the
// byte stored.
func PutJSON(ctx context.Context, s Store, key string, v any) error {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if err := json.NewEncoder(zw).Encode(v); err != nil {
		return fmt.Errorf("failed to encode %s: %w", key, err)
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("failed to compress %s: %w", key, err)
	}
	return s.Put(ctx, key, buf.Bytes(), "application/gzip")
}

// GetJSON reads a value stored by PutJSON into v. It returns false if
// there's none.
func GetJSON(ctx context.Context, s Store, key string, v any) (bool, error) {
	data, err := s.Get(ctx, key)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return false, fmt.Errorf("failed to decompress %s: %w", key, err)
	}
	if err := json.NewDecoder(zr).Decode(v); err != nil {
		return false, fmt.Errorf("failed to decode %s: %w", key, err)
	}
	return true, nil
}

// Fingerprint returns a short hash of the fields of the types of the
// values, to name what's stored of them: a migration adding a column
// changes the rows' types, and so the name, so rows stored before it,
// without the column, aren't read as if they had it empty.
func Fingerprint(values ...any) string {
	h := sha256.New()
	for _, v := range values {
		t := reflect.TypeOf(v)
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		writeType(h, t)
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// writeType writes a type's name, and its fields' names and types, to w.
func writeType(w io.Writer, t reflect.Type) {
	fmt.Fprintf(w, "%s{", t.String())
	if t.Kind() == reflect.Struct {
		for i := range t.NumField() {
			f := t.Field(i)
			fmt.Fprintf(w, "%s:", f.Name)
			ft := f.Type
			for ft.Kind() == reflect.Pointer || ft.Kind() == reflect.Slice {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct && ft.PkgPath() == t.PkgPath() {
				writeType(w, ft)
			} else {
				fmt.Fprintf(w, "%s;", f.Type.String())
			}
		}
	}
	fmt.Fprint(w, "}")
}
