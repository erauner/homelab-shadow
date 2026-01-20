package kustomize

// IMPORTANT: Go Test Cache Behavior
//
// Go caches test results based on the test binary and its inputs. However, these tests
// invoke external files (kustomization.yaml, values.yaml, etc.) that Go's cache doesn't track.
//
// If you modify kustomization files and re-run tests, you may get stale cached results.
// To clear the cache and force tests to re-run:
//
//     go clean -testcache
//
// Or run with the -count flag:
//
//     go test -count=1 ./pkg/kustomize/...
//
// For CI, this isn't an issue because each build starts fresh.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// getRepoRoot returns the path to the homelab-k8s repository root
func getRepoRoot(t *testing.T) string {
	t.Helper()

	// Get the directory of this test file
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("Failed to get test file path")
	}

	// Walk up from tools/shadow/pkg/kustomize to repo root
	repoRoot := filepath.Join(filepath.Dir(filename), "..", "..", "..", "..")

	// Verify it's the right directory
	if _, err := os.Stat(filepath.Join(repoRoot, "apps")); err != nil {
		t.Fatalf("Failed to find repo root from %s: %v", filename, err)
	}

	absPath, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatalf("Failed to get absolute path: %v", err)
	}

	return absPath
}

// getKubernetesVersion returns the Kubernetes version to validate against
func getKubernetesVersion() string {
	if v := os.Getenv("KUBERNETES_VERSION"); v != "" {
		return v
	}
	return "1.31.0" // Default version
}

// TestKustomizeInstalled verifies kustomize CLI is available
func TestKustomizeInstalled(t *testing.T) {
	if !IsKustomizeInstalled() {
		t.Skip("kustomize CLI not installed - skipping tests")
	}

	version, err := KustomizeVersion()
	if err != nil {
		t.Fatalf("Failed to get kustomize version: %v", err)
	}
	t.Logf("Kustomize version: %s", version)
}

// TestKubeconformInstalled verifies kubeconform CLI is available
func TestKubeconformInstalled(t *testing.T) {
	if !IsKubeconformInstalled() {
		t.Skip("kubeconform CLI not installed - skipping tests")
	}

	version, err := KubeconformVersion()
	if err != nil {
		t.Fatalf("Failed to get kubeconform version: %v", err)
	}
	t.Logf("Kubeconform version: %s", version)
}

// TestDiscoverDirectories tests directory discovery
func TestDiscoverDirectories(t *testing.T) {
	if !IsKustomizeInstalled() {
		t.Skip("kustomize CLI not installed")
	}

	repoRoot := getRepoRoot(t)
	runner := NewRunner(repoRoot, getKubernetesVersion(), testing.Verbose())

	dirs, err := runner.DiscoverDirectories()
	if err != nil {
		t.Fatalf("Failed to discover directories: %v", err)
	}

	t.Logf("Discovered %d kustomization directories", len(dirs))

	if len(dirs) == 0 {
		t.Error("Expected to find some kustomization directories")
	}

	// Log first few directories
	for i, dir := range dirs {
		if i < 5 {
			t.Logf("  - %s", dir)
		}
	}
	if len(dirs) > 5 {
		t.Logf("  ... and %d more", len(dirs)-5)
	}
}

// TestAllKustomizeDirectories validates all kustomization directories
// Each directory runs as a subtest for granular JUnit output
func TestAllKustomizeDirectories(t *testing.T) {
	if !IsKustomizeInstalled() {
		t.Skip("kustomize CLI not installed")
	}
	if !IsKubeconformInstalled() {
		t.Skip("kubeconform CLI not installed")
	}
	if !IsHelmInstalled() {
		t.Skip("helm CLI not installed (required for --enable-helm)")
	}

	repoRoot := getRepoRoot(t)
	runner := NewRunner(repoRoot, getKubernetesVersion(), testing.Verbose())

	dirs, err := runner.DiscoverDirectories()
	if err != nil {
		t.Fatalf("Failed to discover directories: %v", err)
	}

	t.Logf("Validating %d kustomization directories", len(dirs))

	for _, dir := range dirs {
		dir := dir // capture for closure
		// Convert path separators to underscores for test name
		testName := strings.ReplaceAll(dir, "/", "_")

		t.Run(testName, func(t *testing.T) {
			// Note: We intentionally don't use t.Parallel() here because
			// kustomize build can be resource-intensive and may cause issues
			// when running 200+ builds concurrently

			result := runner.ValidateDirectory(dir)

			if result.Skipped {
				t.Skipf("Skipped: %s", result.SkipReason)
			}

			if !result.BuildPassed {
				errorMsg := ExtractKustomizeBuildError(result.BuildOutput)
				t.Errorf("Build failed for %s:\n%s", dir, errorMsg)
				if testing.Verbose() {
					t.Logf("Full output:\n%s", result.BuildOutput)
				}
				return
			}

			if !result.SchemaPassed {
				t.Errorf("Schema validation failed for %s:\n%s", dir, result.SchemaOutput)
				errors := ParseKubeconformErrors(result.SchemaOutput)
				for _, e := range errors {
					t.Errorf("  %s: %s - %s", e.Level, e.Resource, e.Message)
				}
				return
			}

			if testing.Verbose() {
				summary := ParseKubeconformSummary(result.SchemaOutput)
				t.Logf("OK: %d resources (valid: %d, skipped: %d)",
					summary.Resources, summary.Valid, summary.Skipped)
			}
		})
	}
}

// TestKustomizeSummary runs all validations and reports a summary
func TestKustomizeSummary(t *testing.T) {
	if !IsKustomizeInstalled() {
		t.Skip("kustomize CLI not installed")
	}
	if !IsKubeconformInstalled() {
		t.Skip("kubeconform CLI not installed")
	}
	if !IsHelmInstalled() {
		t.Skip("helm CLI not installed (required for --enable-helm)")
	}

	repoRoot := getRepoRoot(t)
	runner := NewRunner(repoRoot, getKubernetesVersion(), testing.Verbose())

	results, err := runner.ValidateAll()
	if err != nil {
		t.Fatalf("Failed to validate: %v", err)
	}

	summary := Summarize(results)
	t.Logf("Summary: %d total, %d passed, %d build failed, %d schema failed, %d skipped",
		summary.Total, summary.Passed, summary.BuildFailed, summary.SchemaFailed, summary.Skipped)

	failed := FailedResults(results)
	if len(failed) > 0 {
		t.Errorf("%d directories failed validation:", len(failed))
		for _, r := range failed {
			t.Errorf("  - %s", FormatValidationError(r))
		}
	}
}

// TestParseKubeconformSummary tests the kubeconform summary parser
func TestParseKubeconformSummary(t *testing.T) {
	testCases := []struct {
		name     string
		output   string
		expected KubeconformSummary
	}{
		{
			name:   "all valid",
			output: "Summary: 4 resources found in 1 file - Valid: 4, Invalid: 0, Errors: 0, Skipped: 0",
			expected: KubeconformSummary{
				Resources: 4, Valid: 4, Invalid: 0, Errors: 0, Skipped: 0,
			},
		},
		{
			name:   "some skipped",
			output: "Summary: 5 resources found in 1 file - Valid: 4, Invalid: 0, Errors: 0, Skipped: 1",
			expected: KubeconformSummary{
				Resources: 5, Valid: 4, Invalid: 0, Errors: 0, Skipped: 1,
			},
		},
		{
			name:   "with errors",
			output: "Summary: 10 resources found in 1 file - Valid: 8, Invalid: 1, Errors: 1, Skipped: 0",
			expected: KubeconformSummary{
				Resources: 10, Valid: 8, Invalid: 1, Errors: 1, Skipped: 0,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ParseKubeconformSummary(tc.output)
			if result.Resources != tc.expected.Resources {
				t.Errorf("Resources: got %d, want %d", result.Resources, tc.expected.Resources)
			}
			if result.Valid != tc.expected.Valid {
				t.Errorf("Valid: got %d, want %d", result.Valid, tc.expected.Valid)
			}
			if result.Invalid != tc.expected.Invalid {
				t.Errorf("Invalid: got %d, want %d", result.Invalid, tc.expected.Invalid)
			}
			if result.Errors != tc.expected.Errors {
				t.Errorf("Errors: got %d, want %d", result.Errors, tc.expected.Errors)
			}
			if result.Skipped != tc.expected.Skipped {
				t.Errorf("Skipped: got %d, want %d", result.Skipped, tc.expected.Skipped)
			}
		})
	}
}

// TestHasKubeconformErrors tests error detection
func TestHasKubeconformErrors(t *testing.T) {
	testCases := []struct {
		name     string
		output   string
		expected bool
	}{
		{
			name:     "no errors",
			output:   "Summary: 4 resources found in 1 file - Valid: 4, Invalid: 0, Errors: 0, Skipped: 0",
			expected: false,
		},
		{
			name:     "has invalid",
			output:   "Summary: 4 resources found in 1 file - Valid: 3, Invalid: 1, Errors: 0, Skipped: 0",
			expected: true,
		},
		{
			name:     "has errors",
			output:   "Summary: 4 resources found in 1 file - Valid: 3, Invalid: 0, Errors: 1, Skipped: 0",
			expected: true,
		},
		{
			name:     "has ERRO line",
			output:   "ERRO - /tmp/manifest.yaml: invalid resource",
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := HasKubeconformErrors(tc.output)
			if result != tc.expected {
				t.Errorf("HasKubeconformErrors: got %v, want %v", result, tc.expected)
			}
		})
	}
}
