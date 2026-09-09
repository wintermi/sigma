// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestCloneTypedContainers(t *testing.T) {
	t.Parallel()
	type numbers []int
	type objects map[string][]map[string]any
	value := objects{"items": {{"numbers": numbers{1, 2}, "bytes": json.RawMessage(`{"n":9007199254740993}`), "array": [1][]float64{{0.5}}}}}
	before := cloneAnyValue(value)
	copy := cloneHandoffAny(value).(objects)
	copy["items"][0]["numbers"].(numbers)[0] = 99
	copy["items"][0]["bytes"].(json.RawMessage)[0] = 'x'
	copy["items"][0]["array"].([1][]float64)[0][0] = 99
	copy["items"] = append(copy["items"], map[string]any{"added": true})
	if !reflect.DeepEqual(value, before) {
		t.Fatal("typed container mutation escaped clone")
	}
	if reflect.DeepEqual(copy, before) {
		t.Fatal("test did not mutate copy")
	}
}

func TestCloneOptionsAndCredentialsIsolateTypedMetadata(t *testing.T) {
	t.Parallel()
	metadata := map[string]any{"rows": []map[string]string{{"key": "original"}}, "raw": json.RawMessage(`{"x":1}`)}
	opts := cloneOptions(Options{Metadata: metadata})
	credential := cloneStoredCredential(StoredCredential{Metadata: metadata})
	opts.Metadata["rows"].([]map[string]string)[0]["key"] = "option"
	credential.Metadata["raw"].(json.RawMessage)[0] = 'x'
	if metadata["rows"].([]map[string]string)[0]["key"] != "original" || string(metadata["raw"].(json.RawMessage)) != `{"x":1}` {
		t.Fatal("metadata copy changed original")
	}
}

func TestRegistryCopiesTypedMetadataOnInsertAndClone(t *testing.T) {
	t.Parallel()
	registry := NewRegistry()
	values := []int{1}
	model := Model{Provider: "copy-test", ID: "model", API: APIOpenAICompletions, ProviderMetadata: map[string]any{"values": values}}
	if err := registry.RegisterModel(model, WithMetadataOnly()); err != nil {
		t.Fatal(err)
	}
	values[0] = 2
	snapshot := registry.Snapshot()
	snapshot.Models[0].ProviderMetadata["values"].([]int)[0] = 4
	for _, r := range []*Registry{registry, registry.Clone()} {
		got, ok := r.Model(model.Provider, model.ID)
		if !ok || got.ProviderMetadata["values"].([]int)[0] != 1 {
			t.Fatal("insertion retained mutable metadata")
		}
		got.ProviderMetadata["values"].([]int)[0] = 3
	}
	got, _ := registry.Model(model.Provider, model.ID)
	if got.ProviderMetadata["values"].([]int)[0] != 1 {
		t.Fatal("registry copy mutation escaped")
	}
}

func TestClonePreservesWireShapesAndOpaqueValues(t *testing.T) {
	t.Parallel()
	pointer := new(int)
	callback := func() int { return 42 }
	type opaque struct{ Values []int }
	value := map[string]any{"pointer": pointer, "callback": callback, "struct": opaque{Values: []int{1}}, "nil": nil, "typedNil": ([]int)(nil), "empty": []int{}}
	copy := cloneAnyValue(value).(map[string]any)
	if copy["pointer"] != pointer || copy["callback"].(func() int)() != 42 {
		t.Fatal("opaque value changed")
	}
	copy["struct"].(opaque).Values[0] = 2
	if value["struct"].(opaque).Values[0] != 2 {
		t.Fatal("opaque struct unexpectedly deep cloned")
	}
	if copy["nil"] != nil || !reflect.ValueOf(copy["typedNil"]).IsNil() || reflect.ValueOf(copy["empty"]).IsNil() {
		t.Fatal("typed container nil/empty shape changed")
	}
	options := map[string]any{"empty": map[string]any{}, "nil": map[string]any(nil), "strings": []string{}, "array": []any{}}
	cloned := cloneHandoffProviderDefinedOptions(options)
	if !reflect.DeepEqual(options["empty"], cloned["empty"]) || !reflect.ValueOf(cloned["nil"]).IsNil() || !reflect.ValueOf(cloned["strings"]).IsNil() || reflect.ValueOf(cloned["array"]).IsNil() {
		t.Fatal("provider option wire shape changed")
	}
	if !reflect.ValueOf(cloneAnyValue(map[string]any{})).IsNil() {
		t.Fatal("general empty-map normalization changed")
	}
	for _, value := range []any{[]byte{}, json.RawMessage{}} {
		if reflect.ValueOf(cloneAnyValue(value)).IsNil() || !reflect.ValueOf(cloneHandoffAny(value)).IsNil() {
			t.Fatal("existing metadata/content empty-byte conventions changed")
		}
	}
	if reflect.ValueOf(cloneHandoffAny(map[string]string{})).IsNil() {
		t.Fatal("typed empty content map changed to nil")
	}
}

func TestRegressionContentCloneIsolation(t *testing.T) {
	t.Parallel()
	original := ToolCallBlock("id", "test", map[string]string{"city": "Paris"})
	cloned := original.Clone()
	cloned.ToolArguments.(map[string]string)["city"] = "Rome"
	if original.ToolArguments.(map[string]string)["city"] != "Paris" {
		t.Fatal("clone mutation changed original tool arguments")
	}
}

func TestRegressionRegistryMetadataIsolation(t *testing.T) {
	t.Parallel()
	registry := NewRegistry()
	model := Model{Provider: "audit", ID: "test", API: APIOpenAICompletions, ProviderMetadata: map[string]any{"values": []int{1, 2}}}
	if err := registry.RegisterModel(model, WithMetadataOnly()); err != nil {
		t.Fatal(err)
	}
	retrieved, _ := registry.Model(model.Provider, model.ID)
	retrieved.ProviderMetadata["values"].([]int)[0] = 99
	again, _ := registry.Model(model.Provider, model.ID)
	if again.ProviderMetadata["values"].([]int)[0] != 1 {
		t.Fatal("read-copy mutation changed registry metadata")
	}
}
