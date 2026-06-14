// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package goldentest

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalJSON_DeterministicAcrossKeyOrder(t *testing.T) {
	t.Parallel()

	j1 := `{"z":1,"a":2,"n":{"y":9,"x":8}}`
	j2 := `{"a":2,"z":1,"n":{"x":8,"y":9}}`

	c1, err1 := CanonicalJSON([]byte(j1))
	c2, err2 := CanonicalJSON(j2)
	if err1 != nil || err2 != nil || !bytes.Equal(c1, c2) {
		t.Fatalf("not deterministic or err: %v %v\nc1=%s\nc2=%s", err1, err2, c1, c2)
	}

	s := string(c1)
	if ia, iz := strings.Index(s, `"a"`), strings.Index(s, `"z"`); ia < 0 || iz < 0 || ia > iz {
		t.Errorf("top-level keys not sorted a before z: %s", s)
	}
	if ix, iy := strings.Index(s, `"x"`), strings.Index(s, `"y"`); ix < 0 || iy < 0 || ix > iy {
		t.Errorf("nested keys not sorted x before y: %s", s)
	}
}

func TestCanonicalJSON_SortsNestedAndArrayObjects(t *testing.T) {
	t.Parallel()

	in := map[string]any{
		"z": 0,
		"a": []any{
			map[string]any{"z2": 2, "a2": 1},
			map[string]any{"m": "mid", "b": "early"},
		},
	}
	out, err := CanonicalJSON(in)
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	s := string(out)

	// top
	if ia, iz := strings.Index(s, `"a"`), strings.Index(s, `"z"`); ia < 0 || iz < 0 || ia > iz {
		t.Errorf("top not a<z: %s", s)
	}
	// nested in array[0]
	if ia2, iz2 := strings.Index(s, `"a2"`), strings.Index(s, `"z2"`); ia2 < 0 || iz2 < 0 || ia2 > iz2 {
		t.Errorf("array[0] not a2<z2: %s", s)
	}
	// nested in array[1]
	if ib, im := strings.Index(s, `"b"`), strings.Index(s, `"m"`); ib < 0 || im < 0 || ib > im {
		t.Errorf("array[1] not b<m: %s", s)
	}
}

func TestCanonicalJSON_NumberNormalizationAndEdges(t *testing.T) {
	t.Parallel()

	// numbers via json.Number path + primitives + empty
	j := `{"i":1,"f":1.25,"s":"str","empty":{},"arr":[]}`
	out, err := CanonicalJSON(j)
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	s := string(out)
	if !bytes.Contains(out, []byte(`"i": 1`)) || !bytes.Contains(out, []byte(`"f": 1.25`)) {
		t.Errorf("numbers not canonical: %s", s)
	}
	if !strings.Contains(s, `"empty": {}`) || !strings.Contains(s, `"arr": []`) {
		t.Errorf("empty not preserved: %s", s)
	}
}

func TestGoldenPath(t *testing.T) {
	t.Parallel()

	if got := GoldenPath(t, "/tmp/x.json"); got != "/tmp/x.json" {
		t.Errorf("abs passthrough got %q", got)
	}

	rel := GoldenPath(t, "provider/x/y.json")
	if !filepath.IsAbs(rel) {
		t.Errorf("rel must be abs: %s", rel)
	}
	wantSuffix := filepath.Join("testdata", "golden", "provider", "x", "y.json")
	if !strings.HasSuffix(rel, wantSuffix) {
		t.Errorf("rel must end with %s, got %s", wantSuffix, rel)
	}
}
