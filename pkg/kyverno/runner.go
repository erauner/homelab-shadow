// Package kyverno provides Kyverno CLI integration for policy testing
package kyverno

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SkipPolicies are policies that cannot be tested with the Kyverno CLI
// - httproute-cross-namespace: Complex JMESPath causes CLI panic
// - namespace-argocd-ownership: OR pattern with wildcards
// - httproute-hostname-uniqueness: Uses apiCall context (requires live cluster)
var SkipPolicies = map[string]string{
	"httproute-cross-namespace":    "Complex JMESPath causes CLI panic",
	"namespace-argocd-ownership":   "OR pattern with wildcards",
	"httproute-hostname-uniqueness": "Uses apiCall context (requires live cluster)",
}

// kyvernoTestFile represents the structure of a kyverno-test.yaml file
type kyvernoTestFile struct {
	Policies []string `yaml:"policies"`
}

// TestRunner runs Kyverno policy tests
type TestRunner struct {
	RepoPath string
	Verbose  bool
}

// TestResult represents the result of a single policy test
type TestResult struct {
	PolicyName string
	Passed     bool
	Output     string
	Error      error
	Skipped    bool
	SkipReason string
}

// NewTestRunner creates a new Kyverno test runner
func NewTestRunner(repoPath string, verbose bool) *TestRunner {
	return &TestRunner{
		RepoPath: repoPath,
		Verbose:  verbose,
	}
}

// testsDirs returns all test directories (base + overlays)
func (r *TestRunner) testsDirs() []string {
	return []string{
		filepath.Join(r.RepoPath, "policies", "kyverno", "base", "tests"),
		filepath.Join(r.RepoPath, "policies", "kyverno", "overlays", "erauner-home", "tests"),
	}
}

// clusterDirs returns all cluster policy directories (base + overlays)
func (r *TestRunner) clusterDirs() []string {
	return []string{
		filepath.Join(r.RepoPath, "policies", "kyverno", "base", "cluster"),
		filepath.Join(r.RepoPath, "policies", "kyverno", "overlays", "erauner-home", "cluster"),
	}
}

// DiscoverTests finds all Kyverno test directories
func (r *TestRunner) DiscoverTests() ([]string, error) {
	var tests []string

	for _, testsDir := range r.testsDirs() {
		entries, err := os.ReadDir(testsDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue // Directory doesn't exist, skip
			}
			return nil, fmt.Errorf("failed to read tests directory %s: %w", testsDir, err)
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			// Check if directory has a kyverno-test.yaml
			testFile := filepath.Join(testsDir, entry.Name(), "kyverno-test.yaml")
			if _, err := os.Stat(testFile); err == nil {
				tests = append(tests, entry.Name())
			}
		}
	}

	return tests, nil
}

// DiscoverPolicies finds all policy files in the cluster policies directories
func (r *TestRunner) DiscoverPolicies() ([]string, error) {
	var policies []string
	seen := make(map[string]bool)

	for _, policiesDir := range r.clusterDirs() {
		entries, err := os.ReadDir(policiesDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue // Directory doesn't exist, skip
			}
			return nil, fmt.Errorf("failed to read policies directory %s: %w", policiesDir, err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if name == "kustomization.yaml" {
				continue
			}
			if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
				policyName := strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml")
				if !seen[policyName] {
					seen[policyName] = true
					policies = append(policies, policyName)
				}
			}
		}
	}

	return policies, nil
}

// DiscoverTestedPolicies parses all kyverno-test.yaml files and returns policy names that have tests
func (r *TestRunner) DiscoverTestedPolicies() (map[string]bool, error) {
	testedPolicies := make(map[string]bool)

	for _, testsDir := range r.testsDirs() {
		entries, err := os.ReadDir(testsDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue // Directory doesn't exist, skip
			}
			return nil, fmt.Errorf("failed to read tests directory %s: %w", testsDir, err)
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			testFile := filepath.Join(testsDir, entry.Name(), "kyverno-test.yaml")
			data, err := os.ReadFile(testFile)
			if err != nil {
				continue // Skip directories without kyverno-test.yaml
			}

			var testConfig kyvernoTestFile
			if err := yaml.Unmarshal(data, &testConfig); err != nil {
				continue // Skip invalid YAML
			}

			// Extract policy names from paths like "../../cluster/policy-name.yaml"
			for _, policyPath := range testConfig.Policies {
				// Get the base filename and strip .yaml extension
				base := filepath.Base(policyPath)
				policyName := strings.TrimSuffix(strings.TrimSuffix(base, ".yaml"), ".yml")
				testedPolicies[policyName] = true
			}
		}
	}

	return testedPolicies, nil
}

// CheckCoverage checks which policies have tests
func (r *TestRunner) CheckCoverage() (covered []string, missing []string, skipped []string, err error) {
	policies, err := r.DiscoverPolicies()
	if err != nil {
		return nil, nil, nil, err
	}

	// Parse kyverno-test.yaml files to find which policies are actually tested
	testedPolicies, err := r.DiscoverTestedPolicies()
	if err != nil {
		return nil, nil, nil, err
	}

	for _, policy := range policies {
		if reason, ok := SkipPolicies[policy]; ok {
			skipped = append(skipped, fmt.Sprintf("%s (%s)", policy, reason))
		} else if testedPolicies[policy] {
			covered = append(covered, policy)
		} else {
			missing = append(missing, policy)
		}
	}

	return covered, missing, skipped, nil
}

// findTestDir locates the test directory for a policy across base and overlays
func (r *TestRunner) findTestDir(policyName string) string {
	for _, testsDir := range r.testsDirs() {
		testDir := filepath.Join(testsDir, policyName)
		if _, err := os.Stat(testDir); err == nil {
			return testDir
		}
	}
	return ""
}

// RunTest runs a single policy test
func (r *TestRunner) RunTest(policyName string) TestResult {
	// Check if policy should be skipped
	if reason, ok := SkipPolicies[policyName]; ok {
		return TestResult{
			PolicyName: policyName,
			Passed:     true, // Skipped tests are considered passed
			Skipped:    true,
			SkipReason: reason,
		}
	}

	testDir := r.findTestDir(policyName)

	// Check if test directory exists
	if testDir == "" {
		return TestResult{
			PolicyName: policyName,
			Passed:     false,
			Error:      fmt.Errorf("test directory not found for policy: %s", policyName),
		}
	}

	// Run kyverno test
	cmd := exec.Command("kyverno", "test", testDir, "--detailed-results")
	output, err := cmd.CombinedOutput()

	result := TestResult{
		PolicyName: policyName,
		Output:     string(output),
	}

	if err != nil {
		result.Passed = false
		result.Error = fmt.Errorf("kyverno test failed: %w", err)
	} else {
		// Parse output to determine pass/fail
		result.Passed = r.parseTestOutput(string(output))
		if !result.Passed {
			result.Error = fmt.Errorf("test assertions failed")
		}
	}

	return result
}

// RunAllTests runs all discovered policy tests
func (r *TestRunner) RunAllTests() ([]TestResult, error) {
	tests, err := r.DiscoverTests()
	if err != nil {
		return nil, err
	}

	var results []TestResult
	for _, test := range tests {
		result := r.RunTest(test)
		results = append(results, result)
	}

	return results, nil
}

// RunTestsDir runs kyverno test on all tests directories (base + overlays)
func (r *TestRunner) RunTestsDir() TestResult {
	var allOutput strings.Builder
	allPassed := true
	var firstError error

	for _, testsDir := range r.testsDirs() {
		// Skip if directory doesn't exist
		if _, err := os.Stat(testsDir); os.IsNotExist(err) {
			continue
		}

		cmd := exec.Command("kyverno", "test", testsDir, "--detailed-results")
		output, err := cmd.CombinedOutput()

		allOutput.WriteString(fmt.Sprintf("=== Tests from %s ===\n", testsDir))
		allOutput.Write(output)
		allOutput.WriteString("\n")

		if err != nil {
			allPassed = false
			if firstError == nil {
				firstError = fmt.Errorf("kyverno test failed in %s: %w", testsDir, err)
			}
		} else if !r.parseTestOutput(string(output)) {
			allPassed = false
			if firstError == nil {
				firstError = fmt.Errorf("test assertions failed in %s", testsDir)
			}
		}
	}

	result := TestResult{
		PolicyName: "all",
		Output:     allOutput.String(),
		Passed:     allPassed,
		Error:      firstError,
	}

	return result
}

// parseTestOutput parses kyverno test output to determine pass/fail
func (r *TestRunner) parseTestOutput(output string) bool {
	// Kyverno outputs a summary line like:
	// Test Summary: 4 tests passed and 0 tests failed
	// Or individual test results with Pass/Fail markers

	// Check for explicit failure markers
	if strings.Contains(output, "tests failed") {
		// Parse the "X tests failed" part
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			if strings.Contains(line, "Test Summary:") {
				if strings.Contains(line, "0 tests failed") {
					return true
				}
				return false
			}
		}
	}

	// Check for any "Fail" in the detailed results
	if strings.Contains(output, "| Fail |") || strings.Contains(output, "|Fail|") {
		return false
	}

	// If we see the success pattern and no failures, it passed
	if strings.Contains(output, "tests passed") {
		return true
	}

	// Default to checking exit code (already handled by caller)
	return true
}

// KyvernoVersion returns the installed kyverno CLI version
func KyvernoVersion() (string, error) {
	cmd := exec.Command("kyverno", "version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get kyverno version: %w", err)
	}
	lines := strings.Split(string(output), "\n")
	if len(lines) > 0 {
		return strings.TrimSpace(lines[0]), nil
	}
	return "", fmt.Errorf("empty version output")
}

// IsKyvernoInstalled checks if kyverno CLI is installed
func IsKyvernoInstalled() bool {
	_, err := exec.LookPath("kyverno")
	return err == nil
}
