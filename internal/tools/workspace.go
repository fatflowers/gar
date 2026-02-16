package tools

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrPathOutsideWorkspace = errors.New("path is outside workspace")

func normalizeWorkspaceRoot(root string) (string, error) {
	trimmed := strings.TrimSpace(root)
	if trimmed == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve working directory: %w", err)
		}
		trimmed = cwd
	}

	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("resolve absolute workspace root %s: %w", trimmed, err)
	}

	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve workspace symlinks %s: %w", abs, err)
	}
	return filepath.Clean(resolved), nil
}

func resolveWorkspacePath(workspaceRoot, inputPath string, allowCreate bool) (string, error) {
	rawPath := strings.TrimSpace(inputPath)
	if rawPath == "" {
		return "", errors.New("path is required")
	}

	root, err := normalizeWorkspaceRoot(workspaceRoot)
	if err != nil {
		return "", err
	}

	candidate := rawPath
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	candidate = filepath.Clean(candidate)

	resolved, err := resolvePathWithOptionalMissing(candidate, allowCreate)
	if err != nil {
		return "", fmt.Errorf("resolve path %s: %w", rawPath, err)
	}
	if !isWithinWorkspace(root, resolved) {
		return "", fmt.Errorf("%w: %s (workspace: %s)", ErrPathOutsideWorkspace, rawPath, root)
	}
	return resolved, nil
}

func resolvePathWithOptionalMissing(path string, allowCreate bool) (string, error) {
	if !allowCreate {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", err
		}
		return filepath.Clean(resolved), nil
	}

	missing := make([]string, 0, 8)
	probe := filepath.Clean(path)
	for {
		resolved, err := filepath.EvalSymlinks(probe)
		if err == nil {
			out := resolved
			for i := len(missing) - 1; i >= 0; i-- {
				out = filepath.Join(out, missing[i])
			}
			return filepath.Clean(out), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}

		parent := filepath.Dir(probe)
		if parent == probe {
			return "", err
		}

		missing = append(missing, filepath.Base(probe))
		probe = parent
	}
}

func isWithinWorkspace(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
