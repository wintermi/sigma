// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package evals

import (
	"path/filepath"
	"testing"
)

// Not parallel: t.Chdir and t.Setenv change process-wide state.
func TestNewRunnerAcceptsExplicitArtifactDirectoryOutsideModule(t *testing.T) {
	outside := t.TempDir()
	t.Chdir(outside)

	t.Run("flag", func(t *testing.T) {
		t.Setenv(ArtifactDirectoryEnvironmentVariable, "")
		want := filepath.Join(outside, "flag-artifacts")
		runner, err := NewRunner(RunnerConfig{ArtifactDir: want})
		if err != nil {
			t.Fatalf("NewRunner returned error: %v", err)
		}
		if runner.ArtifactDir() != want {
			t.Fatalf("artifact dir = %q, want %q", runner.ArtifactDir(), want)
		}
	})
	t.Run("environment", func(t *testing.T) {
		want := filepath.Join(outside, "env-artifacts")
		t.Setenv(ArtifactDirectoryEnvironmentVariable, want)
		runner, err := NewRunner(RunnerConfig{})
		if err != nil {
			t.Fatalf("NewRunner returned error: %v", err)
		}
		if runner.ArtifactDir() != want {
			t.Fatalf("artifact dir = %q, want %q", runner.ArtifactDir(), want)
		}
	})
	t.Run("default still needs a module", func(t *testing.T) {
		t.Setenv(ArtifactDirectoryEnvironmentVariable, "")
		if _, err := NewRunner(RunnerConfig{}); err == nil {
			t.Fatal("NewRunner returned nil error without a module or explicit directory")
		}
	})
}
