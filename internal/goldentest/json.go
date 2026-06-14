// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package goldentest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
)

const updateEnv = "UPDATE_GOLDEN"

// AssertJSON compares got with a canonical JSON fixture under testdata/golden.
// Set UPDATE_GOLDEN=1 to rewrite the fixture from got.
func AssertJSON(t testing.TB, got any, name string) {
	t.Helper()

	actual, err := CanonicalJSON(got)
	if err != nil {
		t.Fatalf("canonicalize actual JSON: %v", err)
	}

	path := GoldenPath(t, name)
	if os.Getenv(updateEnv) == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { // #nosec G301 -- golden fixtures are checked-in test files.
			t.Fatalf("create golden directory: %v", err)
		}
		if err := os.WriteFile(path, actual, 0o644); err != nil { // #nosec G306 -- golden fixtures are checked-in test files.
			t.Fatalf("update golden %s: %v", path, err)
		}
		return
	}

	expected, err := os.ReadFile(path) // #nosec G304 -- golden path is derived from the current test name.
	if err != nil {
		t.Fatalf("read golden %s: %v\nset %s=1 to create or update it", path, err, updateEnv)
	}
	canonicalExpected, err := CanonicalJSON(expected)
	if err != nil {
		t.Fatalf("canonicalize golden %s: %v", path, err)
	}
	if !bytes.Equal(actual, canonicalExpected) {
		t.Fatalf("JSON golden mismatch: %s\n%s", path, diff(canonicalExpected, actual))
	}
}

// AssertNoJSONPath fails if a provider payload includes an unsupported field.
func AssertNoJSONPath(t testing.TB, got any, path ...string) {
	t.Helper()

	value, err := jsonValue(got)
	if err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	current := value
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return
		}
		next, ok := object[key]
		if !ok {
			return
		}
		current = next
	}
	t.Fatalf("JSON path %s present with value %s", strings.Join(path, "."), mustJSON(current))
}

// CanonicalJSON returns indented JSON with deterministic map key ordering.
func CanonicalJSON(value any) ([]byte, error) {
	decoded, err := jsonValue(value)
	if err != nil {
		return nil, err
	}
	ordered := sortedObjectValue(decoded)
	data, err := json.MarshalIndent(ordered, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func GoldenPath(t testing.TB, name string) string {
	t.Helper()

	if filepath.IsAbs(name) {
		return name
	}
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("find repo root: %v", err)
	}
	return filepath.Join(root, "testdata", "golden", filepath.FromSlash(name))
}

func jsonValue(value any) (any, error) {
	switch v := value.(type) {
	case []byte:
		return decodeJSON(v)
	case string:
		return decodeJSON([]byte(v))
	case json.RawMessage:
		return decodeJSON(v)
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return decodeJSON(data)
	}
}

func decodeJSON(data []byte) (any, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return nil, err
	}
	if decoder.More() {
		return nil, fmt.Errorf("multiple JSON values")
	}
	return normalize(value), nil
}

func normalize(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, nested := range v {
			out[key] = normalize(nested)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, nested := range v {
			out[i] = normalize(nested)
		}
		return out
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i
		}
		if f, err := v.Float64(); err == nil {
			return f
		}
		return v.String()
	default:
		return v
	}
}

type sortedObject map[string]any

func (o sortedObject) MarshalJSON() ([]byte, error) {
	if o == nil {
		return []byte("null"), nil
	}
	keys := make([]string, 0, len(o))
	for k := range o {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		b.Write(kb)
		b.WriteByte(':')
		vb, err := json.Marshal(sortedObjectValue(o[k]))
		if err != nil {
			return nil, err
		}
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func sortedObjectValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return sortedObject(x)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = sortedObjectValue(e)
		}
		return out
	default:
		return x
	}
}

func repoRoot() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller failed to determine source location for goldentest")
	}
	dir := filepath.Dir(filename)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from goldentest source %s", filename)
		}
		dir = parent
	}
}

func diff(expected, actual []byte) string {
	expectedLines := strings.Split(strings.TrimSuffix(string(expected), "\n"), "\n")
	actualLines := strings.Split(strings.TrimSuffix(string(actual), "\n"), "\n")
	var b strings.Builder
	max := max(len(expectedLines), len(actualLines))
	for i := 0; i < max; i++ {
		var want, got string
		if i < len(expectedLines) {
			want = expectedLines[i]
		}
		if i < len(actualLines) {
			got = actualLines[i]
		}
		if reflect.DeepEqual(want, got) {
			continue
		}
		start := i - 3
		if start < 0 {
			start = 0
		}
		end := i + 4
		if end > max {
			end = max
		}
		fmt.Fprintf(&b, "@@ line %d @@\n", i+1)
		for line := start; line < end; line++ {
			if line < len(expectedLines) && line < len(actualLines) && expectedLines[line] == actualLines[line] {
				fmt.Fprintf(&b, "  %s\n", expectedLines[line])
				continue
			}
			if line < len(expectedLines) {
				fmt.Fprintf(&b, "- %s\n", expectedLines[line])
			}
			if line < len(actualLines) {
				fmt.Fprintf(&b, "+ %s\n", actualLines[line])
			}
		}
		return b.String()
	}
	return "no textual diff"
}

func mustJSON(value any) string {
	data, err := CanonicalJSON(value)
	if err != nil {
		return fmt.Sprintf("%#v", value)
	}
	return strings.TrimSpace(string(data))
}
