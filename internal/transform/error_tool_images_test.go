// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package transform

import (
	"reflect"
	"testing"

	"github.com/wintermi/sigma"
)

func TestSplitErrorToolResultImages(t *testing.T) {
	t.Parallel()

	image := sigma.ImageBase64("image/png", "aW1hZ2U=")
	other := sigma.ImageBase64("image/jpeg", "b3RoZXI=")
	tests := []struct {
		name        string
		message     sigma.Message
		wantOK      bool
		wantContent []sigma.ContentBlock
		wantImages  []sigma.ContentBlock
	}{
		{
			name:    "successful result keeps its images",
			message: sigma.Message{Role: sigma.RoleTool, Content: []sigma.ContentBlock{sigma.Text("ok"), image}},
		},
		{
			name:    "error result without images needs no split",
			message: sigma.Message{Role: sigma.RoleTool, IsError: true, Content: []sigma.ContentBlock{sigma.Text("failed")}},
		},
		{
			name: "adjacent images share one placeholder",
			message: sigma.Message{Role: sigma.RoleTool, IsError: true, Content: []sigma.ContentBlock{
				sigma.Text("failed"), image, other, sigma.Text("see above"), image,
			}},
			wantOK: true,
			wantContent: []sigma.ContentBlock{
				sigma.Text("failed"),
				sigma.Text(ErrorToolImagePlaceholder),
				sigma.Text("see above"),
				sigma.Text(ErrorToolImagePlaceholder),
			},
			wantImages: []sigma.ContentBlock{image, other, image},
		},
		{
			name: "existing placeholder text is not repeated",
			message: sigma.Message{Role: sigma.RoleTool, IsError: true, Content: []sigma.ContentBlock{
				sigma.Text(ErrorToolImagePlaceholder), image,
			}},
			wantOK:      true,
			wantContent: []sigma.ContentBlock{sigma.Text(ErrorToolImagePlaceholder)},
			wantImages:  []sigma.ContentBlock{image},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			split, ok := SplitErrorToolResultImages(tt.message)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !reflect.DeepEqual(split.Content, tt.wantContent) {
				t.Fatalf("Content = %#v, want %#v", split.Content, tt.wantContent)
			}
			if !reflect.DeepEqual(split.Images, tt.wantImages) {
				t.Fatalf("Images = %#v, want %#v", split.Images, tt.wantImages)
			}
		})
	}
}

func TestSplitErrorToolResultImagesCaptionNamesToolCall(t *testing.T) {
	t.Parallel()

	split, ok := SplitErrorToolResultImages(sigma.Message{
		Role:       sigma.RoleTool,
		ToolCallID: "toolu_1",
		ToolName:   "screenshot",
		IsError:    true,
		Content:    []sigma.ContentBlock{sigma.ImageBase64("image/png", "aW1hZ2U=")},
	})
	if !ok {
		t.Fatal("ok = false, want split")
	}
	if want := "Images from the error result of tool call toolu_1 (screenshot):"; split.Caption != want {
		t.Fatalf("Caption = %q, want %q", split.Caption, want)
	}
}
