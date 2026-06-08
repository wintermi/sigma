// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/wintermi/sigma"
	"github.com/wintermi/sigma/sigmatest"
)

func main() {
	os.Exit(run())
}

func run() int {
	provider := sigmatest.NewFauxProvider(sigmatest.Script{
		WaitForCancel: true,
		Final: sigma.AssistantMessage{
			Content: []sigma.ContentBlock{sigma.Text("partial text before cancellation")},
		},
	})
	registry, err := sigmatest.Registry(provider)
	if err != nil {
		fmt.Fprintf(os.Stderr, "setup failed: %v\n", err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	client := sigma.NewClient(sigma.WithRegistry(registry))
	stream := client.Stream(ctx, sigmatest.TextModel(), sigma.Request{
		Messages: []sigma.Message{sigma.UserText("This request will time out.")},
	})

	final, err := sigma.Collect(context.Background(), stream)
	if err == nil {
		fmt.Fprintf(os.Stderr, "expected cancellation error\n")
		return 1
	}
	if !errors.Is(err, sigma.ErrAborted) {
		fmt.Fprintf(os.Stderr, "expected cancellation error: %v\n", err)
		return 1
	}

	fmt.Printf("stop reason: %s\n", final.StopReason)
	fmt.Printf("final content blocks: %d\n", len(final.Content))

	var generationErr *sigma.GenerationError
	if errors.As(err, &generationErr) {
		aborted, ok := generationErr.FinalMessage()
		fmt.Printf("aborted final available: %t (%s)\n", ok, aborted.StopReason)
	}
	return 0
}
