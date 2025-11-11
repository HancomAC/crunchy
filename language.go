package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type language string

const (
	langPNPM language = "pnpm"
	langNPM  language = "npm"
	langYarn language = "yarn"
	langGo   language = "go"
	langRust language = "rust"
)

func buildProject(cfg *config) error {
	switch cfg.lang {
	case langPNPM:
		if err := runCommandStreaming(cfg.workdir, "pnpm", "run", "build"); err != nil {
			return fmt.Errorf("build project with pnpm: %w", err)
		}
	case langNPM:
		if err := runCommandStreaming(cfg.workdir, "npm", "run", "build"); err != nil {
			return fmt.Errorf("build project with npm: %w", err)
		}
	case langYarn:
		if err := runCommandStreaming(cfg.workdir, "yarn", "build"); err != nil {
			return fmt.Errorf("build project with yarn: %w", err)
		}
	case langGo:
		if err := runCommandStreaming(cfg.workdir, "go", "build", "./..."); err != nil {
			return fmt.Errorf("build project with go: %w", err)
		}
	case langRust:
		if err := runCommandStreaming(cfg.workdir, "cargo", "build", "--release"); err != nil {
			return fmt.Errorf("build project with cargo: %w", err)
		}
	default:
		return fmt.Errorf("unsupported language %q", cfg.lang)
	}

	return nil
}

func determineLanguage(workdir, requested string) (language, error) {
	requested = strings.TrimSpace(strings.ToLower(requested))
	if requested == "" {
		return detectLanguage(workdir)
	}

	switch requested {
	case string(langNPM), "node", "ts", "typescript", "javascript", "nodejs":
		return langNPM, nil
	case string(langPNPM):
		return langPNPM, nil
	case string(langYarn):
		return langYarn, nil
	case string(langGo), "golang":
		return langGo, nil
	case string(langRust), "cargo":
		return langRust, nil
	default:
		return "", fmt.Errorf("unsupported --lang value %q", requested)
	}
}

func detectLanguage(workdir string) (language, error) {
	type indicator struct {
		lang language
		file string
	}

	orderedIndicators := []indicator{
		{lang: langPNPM, file: "pnpm-lock.yaml"},
		{lang: langYarn, file: "yarn.lock"},
		{lang: langNPM, file: "package-lock.json"},
		{lang: langGo, file: "go.mod"},
		{lang: langRust, file: "Cargo.toml"},
	}

	for _, ind := range orderedIndicators {
		if fileExists(filepath.Join(workdir, ind.file)) {
			return ind.lang, nil
		}
	}

	if fileExists(filepath.Join(workdir, "package.json")) {
		return langPNPM, nil
	}

	return langPNPM, nil
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	if _, err := os.Stat(path); err == nil {
		return true
	}
	return false
}
