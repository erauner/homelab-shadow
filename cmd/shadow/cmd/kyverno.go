package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/erauner/homelab-shadow/pkg/kyverno"
	"github.com/spf13/cobra"
)

var (
	kyvernoCheckCoverage bool
)

var kyvernoCmd = &cobra.Command{
	Use:   "kyverno",
	Short: "Kyverno policy testing commands",
	Long: `Commands for testing Kyverno policies.

This provides a wrapper around the kyverno CLI that:
- Auto-discovers policy tests
- Checks test coverage
- Produces reliable output for CI integration

Note: For JUnit XML output in CI, use go test with go-junit-report:
  go test ./tools/shadow/pkg/kyverno/... -v 2>&1 | go-junit-report > results.xml`,
}

var kyvernoTestCmd = &cobra.Command{
	Use:   "test [policy-name]",
	Short: "Run Kyverno policy tests",
	Long: `Run Kyverno policy tests.

Without arguments, runs all discovered policy tests.
With a policy name, runs only that specific policy's tests.

Examples:
  shadow kyverno test
  shadow kyverno test application-multi-source-ordering
  shadow kyverno test --coverage`,
	RunE: runKyvernoTest,
}

func init() {
	rootCmd.AddCommand(kyvernoCmd)
	kyvernoCmd.AddCommand(kyvernoTestCmd)

	kyvernoTestCmd.Flags().BoolVar(&kyvernoCheckCoverage, "coverage", false, "Check test coverage and fail if policies are missing tests")
}

func runKyvernoTest(cmd *cobra.Command, args []string) error {
	// Check if kyverno is installed
	if !kyverno.IsKyvernoInstalled() {
		return fmt.Errorf("kyverno CLI is not installed\n  Install: brew install kyverno")
	}

	// Print version
	version, err := kyverno.KyvernoVersion()
	if err != nil {
		logInfo("Warning: could not get kyverno version: %v", err)
	} else {
		logInfo("Using: %s", version)
	}

	runner := kyverno.NewTestRunner(repoDir, verbose)

	// Check coverage if requested
	if kyvernoCheckCoverage {
		return runCoverageCheck(runner)
	}

	// Run specific test if provided
	if len(args) > 0 {
		return runSingleTest(runner, args[0])
	}

	// Run all tests
	return runAllTests(runner)
}

func runCoverageCheck(runner *kyverno.TestRunner) error {
	logInfo("\n=== Kyverno Policy Test Coverage ===\n")

	covered, missing, skipped, err := runner.CheckCoverage()
	if err != nil {
		return fmt.Errorf("failed to check coverage: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	if len(covered) > 0 {
		fmt.Fprintln(w, "COVERED:")
		for _, p := range covered {
			fmt.Fprintf(w, "  ✅\t%s\n", p)
		}
	}

	if len(skipped) > 0 {
		fmt.Fprintln(w, "\nSKIPPED (cannot test offline):")
		for _, p := range skipped {
			fmt.Fprintf(w, "  ⏭️\t%s\n", p)
		}
	}

	if len(missing) > 0 {
		fmt.Fprintln(w, "\nMISSING TESTS:")
		for _, p := range missing {
			fmt.Fprintf(w, "  ❌\t%s\n", p)
		}
	}

	w.Flush()

	fmt.Printf("\nSummary: %d covered, %d missing, %d skipped\n",
		len(covered), len(missing), len(skipped))

	if len(missing) > 0 {
		return fmt.Errorf("%d policies missing tests", len(missing))
	}

	return nil
}

func runSingleTest(runner *kyverno.TestRunner, policyName string) error {
	logInfo("\n=== Testing policy: %s ===\n", policyName)

	result := runner.RunTest(policyName)

	if result.Skipped {
		logInfo("⏭️  Skipped: %s", result.SkipReason)
		return nil
	}

	// Print output
	fmt.Println(result.Output)

	if !result.Passed {
		return fmt.Errorf("policy test failed: %v", result.Error)
	}

	logInfo("\n✅ Policy test passed")
	return nil
}

func runAllTests(runner *kyverno.TestRunner) error {
	logInfo("\n=== Kyverno Policy Tests ===\n")

	// First check coverage
	covered, missing, skipped, err := runner.CheckCoverage()
	if err != nil {
		return fmt.Errorf("failed to check coverage: %w", err)
	}

	logInfo("Test coverage: %d policies covered, %d missing, %d skipped",
		len(covered), len(missing), len(skipped))

	if len(skipped) > 0 {
		logInfo("Skipped policies (cannot test offline):")
		for _, s := range skipped {
			logInfo("  - %s", s)
		}
	}

	if len(missing) > 0 {
		logInfo("Policies missing tests:")
		for _, m := range missing {
			logInfo("  - %s", m)
		}
	}

	logInfo("\nRunning tests...\n")

	// Run all tests
	result := runner.RunTestsDir()

	// Print output
	fmt.Println(result.Output)

	// Check for failures
	if !result.Passed {
		// Parse detailed results for better error message
		if strings.Contains(result.Output, "Fail") {
			return fmt.Errorf("policy tests failed - see output above")
		}
		return fmt.Errorf("policy tests failed: %v", result.Error)
	}

	logInfo("\n✅ All policy tests passed")
	return nil
}
