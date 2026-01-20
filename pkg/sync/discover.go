package sync

import (
	"path/filepath"
	"sort"
	"strings"
)

// DiscoverKustomizationsForSync finds kustomization directories suitable for sync
// These are deployment-relevant overlays, not base directories
//
// Patterns (using wildcards, similar to kustomize runner):
//
// New cluster-aware patterns (issue #1256):
//   - apps/*/overlays/*/*          (e.g., apps/coder/overlays/erauner-home/production)
//   - apps/*/stack/*/*             (e.g., apps/coder/stack/erauner-home/production)
//   - apps/*/db/overlays/*/*       (e.g., apps/coder/db/overlays/erauner-home/production)
//
// Legacy patterns (for backward compatibility during migration):
//   - apps/*/overlays/*            (e.g., apps/coder/overlays/production)
//   - apps/*/stack/*               (e.g., apps/coder/stack/production)
//   - apps/*/db/overlays/*         (e.g., apps/coder/db/overlays/production)
//
// Infrastructure/Operators/Security (already cluster-aware):
//   - infrastructure/*/overlays/*
//   - operators/*/overlays/*
//   - security/*/overlays/*
//
// Optional cluster filter limits overlays to specific cluster names (e.g., "erauner-home", "erauner-cloud")
// When cluster filter is specified:
//   - For new app patterns: filters by the cluster segment (apps/*/overlays/<cluster>/*)
//   - For legacy app patterns: no filtering (legacy patterns don't have cluster layer)
//   - For infrastructure/operators/security: filters by overlay name
func DiscoverKustomizationsForSync(repoPath string, clusters []string) ([]string, error) {
	// Patterns to discover - ordered from most specific to least specific
	// New cluster-aware app patterns (issue #1256)
	newAppPatterns := []string{
		"apps/*/overlays/*/*",
		"apps/*/stack/*/*",
		"apps/*/db/overlays/*/*",
	}

	// Legacy app patterns (for backward compatibility)
	legacyAppPatterns := []string{
		"apps/*/overlays/*",
		"apps/*/stack/*",
		"apps/*/db/overlays/*",
	}

	// Infrastructure/Operators/Security patterns (already cluster-aware)
	infraPatterns := []string{
		"infrastructure/*/overlays/*",
		"operators/*/overlays/*",
		"security/*/overlays/*",
	}

	dirSet := make(map[string]bool)

	// Process new cluster-aware app patterns first
	for _, pattern := range newAppPatterns {
		fullPattern := filepath.Join(repoPath, pattern, "kustomization.yaml")
		matches, err := filepath.Glob(fullPattern)
		if err != nil {
			continue // Skip invalid patterns
		}

		for _, match := range matches {
			dir := filepath.Dir(match)
			relDir, err := filepath.Rel(repoPath, dir)
			if err != nil {
				relDir = dir
			}

			// Apply cluster filter for new app patterns
			if len(clusters) > 0 {
				clusterName, ok := extractClusterFromAppPath(relDir)
				if ok && !containsString(clusters, clusterName) {
					continue
				}
			}

			dirSet[relDir] = true
		}
	}

	// Process legacy app patterns (for backward compatibility during migration)
	for _, pattern := range legacyAppPatterns {
		fullPattern := filepath.Join(repoPath, pattern, "kustomization.yaml")
		matches, err := filepath.Glob(fullPattern)
		if err != nil {
			continue
		}

		for _, match := range matches {
			dir := filepath.Dir(match)
			relDir, err := filepath.Rel(repoPath, dir)
			if err != nil {
				relDir = dir
			}

			// Skip if this path was already discovered by new patterns
			// (e.g., apps/coder/overlays/home would match legacy pattern but home/production matches new)
			if dirSet[relDir] {
				continue
			}

			// Check if this is actually a cluster directory that contains environment subdirs
			// If so, skip it - the environment subdirs will be discovered by new patterns
			if isClusterDirectory(repoPath, relDir) {
				continue
			}

			// Legacy patterns don't have cluster layer, so no cluster filtering
			dirSet[relDir] = true
		}
	}

	// Process infrastructure/operators/security patterns
	for _, pattern := range infraPatterns {
		fullPattern := filepath.Join(repoPath, pattern, "kustomization.yaml")
		matches, err := filepath.Glob(fullPattern)
		if err != nil {
			continue
		}

		for _, match := range matches {
			dir := filepath.Dir(match)
			relDir, err := filepath.Rel(repoPath, dir)
			if err != nil {
				relDir = dir
			}

			// Apply cluster filter
			if len(clusters) > 0 {
				overlayName := filepath.Base(relDir)
				if !containsString(clusters, overlayName) {
					continue
				}
			}

			dirSet[relDir] = true
		}
	}

	// Convert to sorted slice
	dirs := make([]string, 0, len(dirSet))
	for dir := range dirSet {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	return dirs, nil
}

// extractClusterFromAppPath extracts the cluster name from an app overlay path
// Returns (cluster, true) for paths like:
//   - apps/<app>/overlays/<cluster>/<env>  -> returns <cluster>
//   - apps/<app>/stack/<cluster>/<env>     -> returns <cluster>
//   - apps/<app>/db/overlays/<cluster>/<env> -> returns <cluster>
//
// Returns ("", false) for paths that don't match the expected structure
func extractClusterFromAppPath(relDir string) (string, bool) {
	parts := strings.Split(relDir, string(filepath.Separator))
	if len(parts) < 4 || parts[0] != "apps" {
		return "", false
	}

	// Pattern: apps/<app>/overlays/<cluster>/<env>
	// parts[0]=apps, parts[1]=<app>, parts[2]=overlays, parts[3]=<cluster>, parts[4]=<env>
	if len(parts) >= 5 && (parts[2] == "overlays" || parts[2] == "stack") {
		return parts[3], true
	}

	// Pattern: apps/<app>/db/overlays/<cluster>/<env>
	// parts[0]=apps, parts[1]=<app>, parts[2]=db, parts[3]=overlays, parts[4]=<cluster>, parts[5]=<env>
	if len(parts) >= 6 && parts[2] == "db" && parts[3] == "overlays" {
		return parts[4], true
	}

	return "", false
}

// isClusterDirectory checks if a legacy-pattern-matched directory is actually
// a cluster directory (contains environment subdirs with kustomization.yaml)
// This helps avoid discovering cluster directories when we should discover their children
func isClusterDirectory(repoPath, relDir string) bool {
	dirPath := filepath.Join(repoPath, relDir)

	// Check if this directory contains subdirectories with kustomization.yaml
	entries, err := filepath.Glob(filepath.Join(dirPath, "*", "kustomization.yaml"))
	if err != nil {
		return false
	}

	// If subdirectories have kustomizations, this is likely a cluster directory
	return len(entries) > 0
}

// containsString checks if a string slice contains a value
func containsString(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}
