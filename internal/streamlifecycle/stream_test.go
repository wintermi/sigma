// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package streamlifecycle

import (
	"context"
	"testing"
	"time"

	"github.com/wintermi/sigma"
)

func TestImageLifecycleReleasesContext(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"completion", "close", "cleanup"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			timeout := time.Hour
			ctx, stream, writer, cleanup := NewImageStream(context.Background(), sigma.Options{Timeout: &timeout})
			defer cleanup()
			defer stream.Close()
			switch mode {
			case "completion":
				// Observe lifecycle cancellation independently of the writer context.
				if err := writer.Done(context.Background(), sigma.AssistantImages{StopReason: sigma.StopReasonEndTurn}); err != nil {
					t.Fatal(err)
				}
			case "close":
				stream.Close()
				stream.Close()
			case "cleanup":
				cleanup()
				cleanup()
			}
			select {
			case <-ctx.Done():
			case <-time.After(2 * time.Second):
				t.Fatal("provider context retained after lifecycle ended")
			}
			if mode == "completion" {
				final, err := sigma.CollectImages(context.Background(), stream)
				if err != nil || final.StopReason != sigma.StopReasonEndTurn {
					t.Fatalf("completion changed: %#v, %v", final, err)
				}
			}
		})
	}
}
