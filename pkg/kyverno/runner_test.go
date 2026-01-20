package kyverno

// IMPORTANT: Go Test Cache Behavior
//
// Go caches test results based on the test binary and its inputs. However, these tests
// invoke external files (kyverno-test.yaml, policy files, etc.) that Go's cache doesn't track.
//
// If you modify kyverno test files and re-run tests, you may get stale cached results.
// To clear the cache and force tests to re-run:
//
//     go clean -testcache
//
// Or run with the -count flag:
//
//     go test -count=1 ./pkg/kyverno/...
//
// For CI, this isn't an issue because each build starts fresh.

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// getRepoRoot returns the path to the homelab-k8s repository root
// This allows tests to run from any directory
func getRepoRoot(t *testing.T) string {
	t.Helper()

	// Get the directory of this test file
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("Failed to get test file path")
	}

	// Walk up from tools/shadow/pkg/kyverno to repo root
	repoRoot := filepath.Join(filepath.Dir(filename), "..", "..", "..", "..")

	// Verify it's the right directory by checking for base/overlays structure
	if _, err := os.Stat(filepath.Join(repoRoot, "policies", "kyverno", "base")); err != nil {
		t.Fatalf("Failed to find repo root from %s: %v", filename, err)
	}

	absPath, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatalf("Failed to get absolute path: %v", err)
	}

	return absPath
}

// TestKyvernoInstalled verifies kyverno CLI is available
func TestKyvernoInstalled(t *testing.T) {
	if !IsKyvernoInstalled() {
		t.Skip("kyverno CLI not installed - skipping tests")
	}

	version, err := KyvernoVersion()
	if err != nil {
		t.Fatalf("Failed to get kyverno version: %v", err)
	}
	t.Logf("Kyverno version: %s", version)
}

// TestAllKyvernoPolicies runs kyverno test on the entire tests directory
// This is the main test that produces JUnit output via go-junit-report
func TestAllKyvernoPolicies(t *testing.T) {
	if !IsKyvernoInstalled() {
		t.Skip("kyverno CLI not installed")
	}

	repoRoot := getRepoRoot(t)
	runner := NewTestRunner(repoRoot, testing.Verbose())

	// Run all tests at once
	result := runner.RunTestsDir()

	if testing.Verbose() {
		t.Logf("Kyverno output:\n%s", result.Output)
	}

	if !result.Passed {
		t.Errorf("Kyverno policy tests failed:\n%s", result.Output)
		if result.Error != nil {
			t.Errorf("Error: %v", result.Error)
		}
	}
}

// TestKyvernoPolicyCoverage checks that all policies have tests
func TestKyvernoPolicyCoverage(t *testing.T) {
	if !IsKyvernoInstalled() {
		t.Skip("kyverno CLI not installed")
	}

	repoRoot := getRepoRoot(t)
	runner := NewTestRunner(repoRoot, testing.Verbose())

	covered, missing, skipped, err := runner.CheckCoverage()
	if err != nil {
		t.Fatalf("Failed to check coverage: %v", err)
	}

	t.Logf("Coverage: %d covered, %d missing, %d skipped",
		len(covered), len(missing), len(skipped))

	if len(skipped) > 0 {
		t.Logf("Skipped policies (cannot test offline):")
		for _, s := range skipped {
			t.Logf("  - %s", s)
		}
	}

	if len(missing) > 0 {
		t.Errorf("Policies missing tests:")
		for _, m := range missing {
			t.Errorf("  - %s", m)
		}
	}
}

// TestKyvernoPolicy_ApplicationMultiSourceOrdering tests the application-multi-source-ordering policy
func TestKyvernoPolicy_ApplicationMultiSourceOrdering(t *testing.T) {
	runSinglePolicyTest(t, "application-multi-source-ordering")
}

// TestKyvernoPolicy_ApplicationNoCreateNamespace tests the application-no-create-namespace policy
func TestKyvernoPolicy_ApplicationNoCreateNamespace(t *testing.T) {
	runSinglePolicyTest(t, "application-no-create-namespace")
}

// TestKyvernoPolicy_GatewayRequireCert tests the gateway-require-cert policy
func TestKyvernoPolicy_GatewayRequireCert(t *testing.T) {
	runSinglePolicyTest(t, "gateway-require-cert")
}

// TestKyvernoPolicy_HTTPRouteParentGateway tests the httproute-parent-gateway policy
func TestKyvernoPolicy_HTTPRouteParentGateway(t *testing.T) {
	runSinglePolicyTest(t, "httproute-parent-gateway")
}

// runSinglePolicyTest is a helper for running individual policy tests
func runSinglePolicyTest(t *testing.T, policyName string) {
	t.Helper()

	if !IsKyvernoInstalled() {
		t.Skip("kyverno CLI not installed")
	}

	// Check if this policy is in the skip list
	if reason, ok := SkipPolicies[policyName]; ok {
		t.Skipf("Policy cannot be tested offline: %s", reason)
	}

	repoRoot := getRepoRoot(t)
	runner := NewTestRunner(repoRoot, testing.Verbose())

	result := runner.RunTest(policyName)

	if result.Skipped {
		t.Skipf("Skipped: %s", result.SkipReason)
	}

	if testing.Verbose() {
		t.Logf("Output:\n%s", result.Output)
	}

	if !result.Passed {
		t.Errorf("Policy test failed:\n%s", result.Output)
		if result.Error != nil {
			t.Errorf("Error: %v", result.Error)
		}
	}
}

// TestParserSummary tests the output parser
func TestParserSummary(t *testing.T) {
	testCases := []struct {
		name     string
		output   string
		expected TestSummary
	}{
		{
			name:     "all passed",
			output:   "Test Summary: 4 tests passed and 0 tests failed",
			expected: TestSummary{Passed: 4, Failed: 0, Total: 4},
		},
		{
			name:     "some failed",
			output:   "Test Summary: 3 tests passed and 2 tests failed",
			expected: TestSummary{Passed: 3, Failed: 2, Total: 5},
		},
		{
			name:     "singular test",
			output:   "Test Summary: 1 test passed and 0 tests failed",
			expected: TestSummary{Passed: 1, Failed: 0, Total: 1},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ParseSummary(tc.output)
			if result.Passed != tc.expected.Passed {
				t.Errorf("Passed: got %d, want %d", result.Passed, tc.expected.Passed)
			}
			if result.Failed != tc.expected.Failed {
				t.Errorf("Failed: got %d, want %d", result.Failed, tc.expected.Failed)
			}
			if result.Total != tc.expected.Total {
				t.Errorf("Total: got %d, want %d", result.Total, tc.expected.Total)
			}
		})
	}
}

// TestHasFailures tests failure detection
func TestHasFailures(t *testing.T) {
	testCases := []struct {
		name     string
		output   string
		expected bool
	}{
		{
			name:     "no failures",
			output:   "Test Summary: 4 tests passed and 0 tests failed",
			expected: false,
		},
		{
			name:     "has failures in summary",
			output:   "Test Summary: 3 tests passed and 1 tests failed",
			expected: true,
		},
		{
			name:     "has Fail in table",
			output:   "1 | policy | rule | resource | Fail | reason",
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := HasFailures(tc.output)
			if result != tc.expected {
				t.Errorf("HasFailures: got %v, want %v", result, tc.expected)
			}
		})
	}
}
