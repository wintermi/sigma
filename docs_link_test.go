// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package sigma_test

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"

	"github.com/wintermi/sigma"
)

var markdownLinkPattern = regexp.MustCompile(`!?\[[^\]]+\]\(([^)]+)\)`)

func TestComparisonImageExampleResolves(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("docs/inspired-by-pi-ai.md")
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`sigma.GetImageModel\(sigma.ProviderOpenRouter, "([^"]+)"\)`)
	match := pattern.FindSubmatch(data)
	if len(match) != 2 {
		t.Fatal("missing image discovery example")
	}
	if _, ok := sigma.GetImageModel(sigma.ProviderOpenRouter, sigma.ModelID(match[1])); !ok {
		t.Fatalf("image example model %s is not registered", match[1])
	}
}

func TestMarkdownInternalLinksResolve(t *testing.T) {
	t.Parallel()

	files := markdownFiles(t)
	anchors := markdownAnchors(t, files)

	for _, file := range files {
		file := file
		t.Run(file, func(t *testing.T) {
			t.Parallel()

			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			for _, match := range markdownLinkPattern.FindAllStringSubmatch(string(data), -1) {
				target := cleanMarkdownTarget(match[1])
				if target == "" || externalTarget(target) {
					continue
				}

				pathPart, fragment, _ := strings.Cut(target, "#")
				targetFile := file
				if pathPart != "" {
					targetFile = filepath.Clean(filepath.Join(filepath.Dir(file), pathPart))
					if _, err := os.Stat(targetFile); err != nil {
						t.Fatalf("%s links to missing target %s", file, target)
					}
				}
				if fragment == "" {
					continue
				}

				fragment, err := url.QueryUnescape(fragment)
				if err != nil {
					t.Fatalf("%s has invalid link fragment %q: %v", file, fragment, err)
				}
				if !anchors[targetFile][githubAnchor(fragment)] {
					t.Fatalf("%s links to missing heading %s in %s", file, fragment, targetFile)
				}
			}
		})
	}
}

func markdownFiles(t *testing.T) []string {
	t.Helper()

	var files []string
	for _, root := range []string{"README.md", "CHANGELOG.md", "RELEASING.md", "docs", "examples", "tools/modeldata", "testdata/golden"} {
		info, err := os.Stat(root)
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() {
			files = append(files, root)
			continue
		}
		err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || filepath.Ext(path) != ".md" {
				return nil
			}
			files = append(files, path)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return files
}

func markdownAnchors(t *testing.T, files []string) map[string]map[string]bool {
	t.Helper()

	anchors := make(map[string]map[string]bool, len(files))
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		fileAnchors := make(map[string]bool)
		for _, line := range strings.Split(string(data), "\n") {
			if !strings.HasPrefix(line, "#") {
				continue
			}
			heading := strings.TrimSpace(strings.TrimLeft(line, "#"))
			if heading == "" {
				continue
			}
			fileAnchors[githubAnchor(heading)] = true
		}
		anchors[file] = fileAnchors
	}
	return anchors
}

func cleanMarkdownTarget(target string) string {
	target = strings.TrimSpace(target)
	target = strings.Trim(target, "<>")
	if before, _, ok := strings.Cut(target, " "); ok {
		target = before
	}
	return strings.TrimSpace(target)
}

func externalTarget(target string) bool {
	if strings.HasPrefix(target, "#") {
		return false
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return false
	}
	return parsed.Scheme != "" || strings.HasPrefix(target, "mailto:")
}

func githubAnchor(heading string) string {
	heading = strings.ToLower(strings.TrimSpace(heading))
	var builder strings.Builder
	lastHyphen := false
	for _, r := range heading {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			builder.WriteRune(r)
			lastHyphen = false
		case unicode.IsSpace(r) || r == '-':
			if !lastHyphen && builder.Len() > 0 {
				builder.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	return strings.Trim(builder.String(), "-")
}
