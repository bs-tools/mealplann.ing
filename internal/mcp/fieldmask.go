package mcp

import (
	"encoding/json"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// normalizeFieldMasks rewrites the field masks in the arguments to the form
// protojson accepts: a string of lowerCamelCase paths. protojson rejects
// snake_case paths, like "nutrients.energy_kcal", and the object form,
// {"paths": [...]}, but both are what the API's comments and the proto
// field names lead agents to write, and the services accept either case.
// Arguments it can't make sense of are returned as they are, for protojson
// to report what's wrong with them.
func normalizeFieldMasks(args json.RawMessage, md protoreflect.MessageDescriptor) json.RawMessage {
	var object map[string]json.RawMessage
	if json.Unmarshal(args, &object) != nil {
		return args
	}
	changed := false
	fields := md.Fields()
	for key, value := range object {
		fd := fields.ByJSONName(key)
		if fd == nil {
			fd = fields.ByName(protoreflect.Name(key))
		}
		if fd == nil || fd.Message() == nil || fd.IsMap() {
			continue
		}
		var normalized json.RawMessage
		switch {
		case fd.Message().FullName() == "google.protobuf.FieldMask" && !fd.IsList():
			normalized = normalizeFieldMask(value)
		case fd.Message().FullName().Parent() == "google.protobuf":
			continue
		case fd.IsList():
			var items []json.RawMessage
			if json.Unmarshal(value, &items) != nil {
				continue
			}
			for i, item := range items {
				items[i] = normalizeFieldMasks(item, fd.Message())
			}
			normalized, _ = json.Marshal(items)
		default:
			normalized = normalizeFieldMasks(value, fd.Message())
		}
		if normalized != nil && string(normalized) != string(value) {
			object[key] = normalized
			changed = true
		}
	}
	if !changed {
		return args
	}
	out, err := json.Marshal(object)
	if err != nil {
		return args
	}
	return out
}

// normalizeFieldMask returns a field mask's JSON as a string of
// lowerCamelCase paths, from a string or a {"paths": [...]} object, or nil
// if it's neither.
func normalizeFieldMask(value json.RawMessage) json.RawMessage {
	var paths []string
	var s string
	var object struct {
		Paths []string `json:"paths"`
	}
	switch {
	case json.Unmarshal(value, &s) == nil:
		if s != "" {
			paths = strings.Split(s, ",")
		}
	case json.Unmarshal(value, &object) == nil && object.Paths != nil:
		paths = object.Paths
	default:
		return nil
	}
	for i, path := range paths {
		parts := strings.Split(strings.TrimSpace(path), ".")
		for j, part := range parts {
			parts[j] = camelCase(part)
		}
		paths[i] = strings.Join(parts, ".")
	}
	out, err := json.Marshal(strings.Join(paths, ","))
	if err != nil {
		return nil
	}
	return out
}

// camelCase converts a snake_case field name to its lowerCamelCase JSON
// name, leaving a name that's already lowerCamelCase as it is.
func camelCase(name string) string {
	var b strings.Builder
	upper := false
	for _, r := range name {
		if r == '_' {
			upper = true
			continue
		}
		if upper && 'a' <= r && r <= 'z' {
			r -= 'a' - 'A'
		}
		upper = false
		b.WriteRune(r)
	}
	return b.String()
}
