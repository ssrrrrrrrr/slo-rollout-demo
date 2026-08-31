package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ArtifactLocator centralizes bounded report-artifact resolution. It only
// resolves existing files; it does not create artifacts or encode workflow
// behaviour.
type ArtifactLocator struct {
	reportDir string
}

func NewArtifactLocator(reportDir string) ArtifactLocator {
	return ArtifactLocator{reportDir: filepath.Clean(reportDir)}
}

func (locator ArtifactLocator) Resolve(candidates []string, fallbackGlob string) (string, os.FileInfo, bool) {
	for _, candidate := range candidates {
		path, ok := locator.pathFor(candidate)
		if !ok {
			continue
		}
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return path, info, true
		}
	}
	if fallbackGlob == "" || strings.Contains(fallbackGlob, string(filepath.Separator)+"..") || strings.Contains(fallbackGlob, "/..") {
		return "", nil, false
	}
	matches, err := filepath.Glob(filepath.Join(locator.reportDir, fallbackGlob))
	if err != nil {
		return "", nil, false
	}
	type artifactFile struct {
		path string
		info os.FileInfo
	}
	files := []artifactFile{}
	for _, path := range matches {
		if !locator.contains(path) || strings.Contains(filepath.Base(path), "-latest.") {
			continue
		}
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			files = append(files, artifactFile{path: path, info: info})
		}
	}
	if len(files) == 0 {
		return "", nil, false
	}
	sort.Slice(files, func(i, j int) bool { return files[i].info.ModTime().After(files[j].info.ModTime()) })
	return files[0].path, files[0].info, true
}

func (locator ArtifactLocator) pathFor(name string) (string, bool) {
	if strings.TrimSpace(name) == "" || filepath.IsAbs(name) {
		return "", false
	}
	path := filepath.Join(locator.reportDir, name)
	return path, locator.contains(path)
}

func (locator ArtifactLocator) contains(path string) bool {
	relative, err := filepath.Rel(locator.reportDir, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
