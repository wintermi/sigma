// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package bedrock

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/wintermi/sigma"
)

func TestRegressionCredentialFileOverridesReplaceDefaults(t *testing.T) {
	credentials := filepath.Join(t.TempDir(), "selected-credentials")
	config := filepath.Join(t.TempDir(), "selected-config")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credentials)
	t.Setenv("AWS_CONFIG_FILE", config)
	paths := awsCredentialFiles()
	if len(paths) != 2 || paths[0] != credentials || paths[1] != config {
		t.Fatalf("explicit credential locations still include default files: %v", paths)
	}
}

type blockingConverseClient struct{ entered chan context.Context }

func (c blockingConverseClient) ConverseStream(ctx context.Context, _ ConverseRequest) (ConverseStream, error) {
	c.entered <- ctx
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestRegressionCloseCancelsBedrockRequest(t *testing.T) {
	t.Parallel()
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan context.Context, 1)
	provider := NewProvider(WithCredentialDetector(fakeCredentialDetector{}), WithConverseStreamClient(blockingConverseClient{entered: entered}))
	stream := provider.Stream(parent, bedrockTestModel("audit"), sigma.Request{Messages: []sigma.Message{sigma.UserText("hello")}}, sigma.Options{})
	defer stream.Close()
	var requestCtx context.Context
	select {
	case requestCtx = <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start")
	}
	stream.Close()
	<-stream.Done()
	select {
	case <-requestCtx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("public stream closed but in-flight Bedrock request was not canceled")
	}
}

type idleConverseStream struct {
	events          chan ConverseEvent
	entered, closed chan struct{}
	once            sync.Once
}

func (s *idleConverseStream) Events() <-chan ConverseEvent {
	s.once.Do(func() { close(s.entered) })
	return s.events
}
func (s *idleConverseStream) Close() error { close(s.closed); return nil }
func (s *idleConverseStream) Err() error   { return nil }

func TestRegressionCloseClosesIdleBedrockTransport(t *testing.T) {
	t.Parallel()
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	transport := &idleConverseStream{events: make(chan ConverseEvent), entered: make(chan struct{}), closed: make(chan struct{})}
	provider := NewProvider(WithCredentialDetector(fakeCredentialDetector{}), WithConverseStreamClient(&fakeConverseClient{stream: transport}))
	stream := provider.Stream(parent, bedrockTestModel("audit"), sigma.Request{Messages: []sigma.Message{sigma.UserText("hello")}}, sigma.Options{})
	defer stream.Close()
	select {
	case <-transport.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("parser did not start")
	}
	stream.Close()
	<-stream.Done()
	select {
	case <-transport.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("public stream closed but idle Bedrock transport remained open")
	}
}

func TestAWSCredentialFilesSelectEffectiveSources(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".aws"), 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name, section, key string) string {
		path := filepath.Join(home, name)
		if err := os.WriteFile(path, []byte("["+section+"]\naws_access_key_id="+key+"\naws_secret_access_key=fake\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	defaultCredentials := write(".aws/credentials", "dev", "default-credentials")
	defaultConfig := write(".aws/config", "profile dev", "default-config")
	selectedCredentials := write("selected-credentials", "dev", "selected-credentials")
	selectedConfig := write("selected-config", "profile dev", "selected-config")
	missing := filepath.Join(home, "missing")
	wrongProfile := write("wrong-profile", "other", "wrong")
	for _, tc := range []struct {
		name, credentials, config, want string
		paths                           []string
	}{
		{"defaults", "", "", "default-credentials", []string{defaultCredentials, defaultConfig}},
		{"credentials override", selectedCredentials, "", "selected-credentials", []string{selectedCredentials, defaultConfig}},
		{"config override", "", selectedConfig, "default-credentials", []string{defaultCredentials, selectedConfig}},
		{"both overrides", selectedCredentials, selectedConfig, "selected-credentials", []string{selectedCredentials, selectedConfig}},
		{"missing credentials", missing, selectedConfig, "selected-config", []string{missing, selectedConfig}},
		{"wrong profile", wrongProfile, selectedConfig, "selected-config", []string{wrongProfile, selectedConfig}},
		{"no effective credentials", missing, wrongProfile, "", []string{missing, wrongProfile}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			paths := resolveAWSCredentialFiles(home, tc.credentials, tc.config)
			if !reflect.DeepEqual(paths, tc.paths) {
				t.Fatalf("paths=%v; want %v", paths, tc.paths)
			}
			got, ok := profileAWSCredentialsFromFiles(paths, "dev")
			if ok != (tc.want != "") || got.AccessKeyID != tc.want {
				t.Fatalf("resolved %q, %v; want %q", got.AccessKeyID, ok, tc.want)
			}
		})
	}
	if got := resolveAWSCredentialFiles("", selectedCredentials, ""); !reflect.DeepEqual(got, []string{selectedCredentials}) {
		t.Fatalf("without home: %v", got)
	}
}

func TestBedrockBlockedConsumerCloseReleasesTransport(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	upstream := &idleConverseStream{events: make(chan ConverseEvent), entered: make(chan struct{}), closed: make(chan struct{})}
	provider := NewProvider(WithCredentialDetector(fakeCredentialDetector{}), WithConverseStreamClient(&fakeConverseClient{stream: upstream}))
	stream := provider.Stream(ctx, bedrockTestModel("close"), sigma.Request{Messages: []sigma.Message{sigma.UserText("hello")}}, sigma.Options{})
	defer stream.Close()
	for _, event := range []ConverseEvent{{Kind: ConverseEventMessageStart}, {Kind: ConverseEventContentBlockDelta, TextDelta: "blocked"}} {
		select {
		case upstream.events <- event:
		case <-time.After(2 * time.Second):
			t.Fatal("provider did not read event")
		}
	}
	stream.Close()
	select {
	case <-upstream.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("blocked producer retained transport")
	}
}

func TestBedrockIdleCancellationReleasesTransport(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"parent", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			upstream := &idleConverseStream{events: make(chan ConverseEvent), entered: make(chan struct{}), closed: make(chan struct{})}
			provider := NewProvider(WithCredentialDetector(fakeCredentialDetector{}), WithConverseStreamClient(&fakeConverseClient{stream: upstream}))
			opts := sigma.Options{}
			if mode == "deadline" {
				timeout := 100 * time.Millisecond
				opts.Timeout = &timeout
			}
			stream := provider.Stream(ctx, bedrockTestModel("cancel"), sigma.Request{Messages: []sigma.Message{sigma.UserText("hello")}}, opts)
			defer stream.Close()
			select {
			case <-upstream.entered:
			case <-time.After(2 * time.Second):
				t.Fatal("parser did not start")
			}
			if mode == "parent" {
				cancel()
			}
			select {
			case <-upstream.closed:
			case <-time.After(2 * time.Second):
				t.Fatal("cancellation retained transport")
			}
		})
	}
}
