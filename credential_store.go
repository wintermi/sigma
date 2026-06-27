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
	locks       map[ProviderID]*sync.Mutex
}

// NewInMemoryCredentialStore constructs an empty in-memory credential store.
func NewInMemoryCredentialStore() *InMemoryCredentialStore {
	return &InMemoryCredentialStore{
		credentials: make(map[ProviderID]StoredCredential),
		locks:       make(map[ProviderID]*sync.Mutex),
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
	lock := s.providerLock(provider)
	lock.Lock()
	defer lock.Unlock()

	select {
	case <-ctx.Done():
		return StoredCredential{}, false, ctx.Err()
	default:
	}

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
func (s *InMemoryCredentialStore) DeleteCredential(_ context.Context, provider ProviderID) error {
	if provider == "" {
		return credentialStoreError("provider id is required")
	}
	s.ensure()
	lock := s.providerLock(provider)
	lock.Lock()
	defer lock.Unlock()

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
		s.locks = make(map[ProviderID]*sync.Mutex)
	}
}

func (s *InMemoryCredentialStore) providerLock(provider ProviderID) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locks == nil {
		s.locks = make(map[ProviderID]*sync.Mutex)
	}
	lock := s.locks[provider]
	if lock == nil {
		lock = &sync.Mutex{}
		s.locks[provider] = lock
	}
	return lock
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
