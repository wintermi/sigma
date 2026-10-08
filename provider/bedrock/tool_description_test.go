// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package bedrock

import "testing"

func TestAWSToolsOmitEmptyDescription(t *testing.T) {
	t.Parallel()

	tools := awsTools([]ConverseTool{{Name: "bare"}, {Name: "described", Description: "Does things"}})
	bare := tools[0]["toolSpec"].(map[string]any)
	if _, ok := bare["description"]; ok {
		t.Fatalf("empty description sent: %#v", bare)
	}
	described := tools[1]["toolSpec"].(map[string]any)
	if got := described["description"]; got != "Does things" {
		t.Fatalf("description = %#v, want Does things", got)
	}
}
