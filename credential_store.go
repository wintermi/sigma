// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// StoredCredential is provider-owned authentication material read from a
// caller-supplied CredentialStore.
type StoredCredential struct {
	Type         CredentialType
	Value        string
	RefreshToken string
	Expiry       time.Time
	Source       string
	ProviderEnv  map[string]string
	Metadata     map[string]any
}

// CredentialModifyFunc receives the current stored credential. Return ok=false
// to leave the existing credential unchanged.
type CredentialModifyFunc func(current StoredCredential, ok bool) (next StoredCredential, nextOK bool, err error)

// CredentialStore stores one credential per provider.
type CredentialStore interface {
	ReadCredential(context.Context, ProviderID) (StoredCredential, bool, error)
	ModifyCredential(context.Context, ProviderID, CredentialModifyFunc) (StoredCredential, bool, error)
	DeleteCredential(context.Context, ProviderID) error
}

// InMemoryCredentialStore is a process-local CredentialStore implementation.
type InMemoryCredentialStore struct {
	mu          sync.Mutex
	credentials map[ProviderID]StoredCredential
	locks       map[ProviderID]chan struct{}
}

// NewInMemoryCredentialStore constructs an empty in-memory credential store.
func NewInMemoryCredentialStore() *InMemoryCredentialStore {
	return &InMemoryCredentialStore{
		credentials: make(map[ProviderID]StoredCredential),
		locks:       make(map[ProviderID]chan struct{}),
	}
}

// ReadCredential returns a copied credential for provider.
func (s *InMemoryCredentialStore) ReadCredential(_ context.Context, provider ProviderID) (StoredCredential, bool, error) {
	if provider == "" {
		return StoredCredential{}, false, credentialStoreError("provider id is required")
	}
	s.ensure()
	s.mu.Lock()
	defer s.mu.Unlock()

	credential, ok := s.credentials[provider]
	return cloneStoredCredential(credential), ok, nil
}

// ModifyCredential serializes read-modify-write operations for one provider.
func (s *InMemoryCredentialStore) ModifyCredential(ctx context.Context, provider ProviderID, fn CredentialModifyFunc) (StoredCredential, bool, error) {
	if provider == "" {
		return StoredCredential{}, false, credentialStoreError("provider id is required")
	}
	if fn == nil {
		return StoredCredential{}, false, credentialStoreError("credential modify function is required")
	}
	s.ensure()
	lock, err := s.providerLock(ctx, provider)
	if err != nil {
		return StoredCredential{}, false, err
	}
	defer func() { <-lock }()

	s.mu.Lock()
	current, ok := s.credentials[provider]
	s.mu.Unlock()

	next, nextOK, err := fn(cloneStoredCredential(current), ok)
	if err != nil {
		return StoredCredential{}, false, err
	}
	if !nextOK {
		return cloneStoredCredential(current), ok, nil
	}

	copied := cloneStoredCredential(next)
	s.mu.Lock()
	s.credentials[provider] = copied
	s.mu.Unlock()
	return cloneStoredCredential(copied), true, nil
}

// DeleteCredential removes a provider credential.
func (s *InMemoryCredentialStore) DeleteCredential(ctx context.Context, provider ProviderID) error {
	if provider == "" {
		return credentialStoreError("provider id is required")
	}
	s.ensure()
	lock, err := s.providerLock(ctx, provider)
	if err != nil {
		return err
	}
	defer func() { <-lock }()

	s.mu.Lock()
	delete(s.credentials, provider)
	s.mu.Unlock()
	return nil
}

func (s *InMemoryCredentialStore) ensure() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.credentials == nil {
		s.credentials = make(map[ProviderID]StoredCredential)
	}
	if s.locks == nil {
		s.locks = make(map[ProviderID]chan struct{})
	}
}

// providerLock acquires ownership without making canceled callers wait for a refresh.
func (s *InMemoryCredentialStore) providerLock(ctx context.Context, provider ProviderID) (chan struct{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	lock := s.locks[provider]
	if lock == nil {
		lock = make(chan struct{}, 1)
		s.locks[provider] = lock
	}
	s.mu.Unlock()

	select {
	case lock <- struct{}{}:
		// Both ownership and cancellation may become ready together.
		if err := ctx.Err(); err != nil {
			<-lock
			return nil, err
		}
		return lock, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func cloneStoredCredential(credential StoredCredential) StoredCredential {
	credential.ProviderEnv = copyStringStringMap(credential.ProviderEnv)
	credential.Metadata = copyStringAnyMap(credential.Metadata)
	return credential
}

func credentialStoreError(message string) error {
	return &Error{Code: ErrorInvalidOptions, Message: "credential store: " + message}
}

func credentialStoreFailure(provider ProviderID, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("credential store: %s: %w", provider, err)
}
