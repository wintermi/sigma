// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package evals

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type artifactReference struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	ContentType string `json:"contentType"`
}

func validateAttachment(category AttachmentCategory, name, contentType string) error {
	switch category {
	case AttachmentTranscript, AttachmentSource, AttachmentFile:
	default:
		return fmt.Errorf("evals: unsupported attachment category %q", category)
	}
	if strings.TrimSpace(name) == "" || filepath.Base(name) != name || name == "." {
		return fmt.Errorf("evals: invalid attachment name %q", name)
	}
	if strings.TrimSpace(contentType) == "" {
		return errors.New("evals: attachment content type must not be empty")
	}
	return nil
}

func newRunID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("evals: generate run id: %w", err)
	}
	return hex.EncodeToString(random[:]), nil
}

func createDefaultArtifactDirectory() (string, string, error) {
	if configured := strings.TrimSpace(os.Getenv(ArtifactDirectoryEnvironmentVariable)); configured != "" {
		absolute, err := filepath.Abs(configured)
		if err != nil {
			return "", "", fmt.Errorf("evals: resolve artifact directory: %w", err)
		}
		moduleRoot, err := findModuleRoot()
		if err != nil {
			return "", "", err
		}
		return absolute, moduleRoot, nil
	}
	moduleRoot, err := findModuleRoot()
	if err != nil {
		return "", "", err
	}
	randomID, err := newRunID()
	if err != nil {
		return "", "", err
	}
	name := time.Now().UTC().Format("2006-01-02T15-04-05.000Z") + "_" + randomID
	return filepath.Join(moduleRoot, ".eval", name), moduleRoot, nil
}

func findModuleRoot() (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("evals: get working directory: %w", err)
	}
	for {
		if info, statErr := os.Stat(filepath.Join(directory, "go.mod")); statErr == nil && !info.IsDir() {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", errors.New("evals: could not find module root")
		}
		directory = parent
	}
}

func ensurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("evals: create artifact directory: %w", err)
	}
	// #nosec G302 -- private directories require owner execute permission.
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("evals: secure artifact directory: %w", err)
	}
	return nil
}

func persistAttachments(artifactDirectory, runID string, attachments []attachment) ([]artifactReference, error) {
	if len(attachments) == 0 {
		return nil, nil
	}
	digest := sha256.Sum256([]byte(runID))
	hashedRunID := hex.EncodeToString(digest[:])
	references := make([]artifactReference, 0, len(attachments))
	var persistErr error
	for _, item := range attachments {
		if err := validateAttachment(item.Category, item.Name, item.ContentType); err != nil {
			persistErr = errors.Join(persistErr, err)
			continue
		}
		directory := filepath.Join(artifactDirectory, string(item.Category), hashedRunID)
		if err := ensurePrivateDirectory(directory); err != nil {
			persistErr = errors.Join(persistErr, err)
			continue
		}
		path := filepath.Join(directory, item.Name)
		if err := writePrivateFile(path, item.Body); err != nil {
			persistErr = errors.Join(persistErr, err)
			continue
		}
		relativePath, err := filepath.Rel(artifactDirectory, path)
		if err != nil {
			persistErr = errors.Join(persistErr, fmt.Errorf("evals: make artifact path relative: %w", err))
			continue
		}
		references = append(references, artifactReference{
			Name:        item.Name,
			Path:        filepath.ToSlash(relativePath),
			ContentType: item.ContentType,
		})
	}
	return references, persistErr
}

func writePrivateFile(path string, body []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("evals: create artifact %q: %w", path, err)
	}
	_, writeErr := file.Write(body)
	closeErr := file.Close()
	chmodErr := os.Chmod(path, 0o600)
	if err := errors.Join(writeErr, closeErr, chmodErr); err != nil {
		return fmt.Errorf("evals: write artifact %q: %w", path, err)
	}
	return nil
}
