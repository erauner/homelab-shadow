package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/erauner/homelab-shadow/pkg/validate"
	"github.com/spf13/cobra"
)

var (
	clusterFilter string
	outputFormat  string
	strict        bool
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate cluster GitOps structure",
	Long: `Validates that clusters follow the expected multi-cluster GitOps layout.

Checks performed:
  - Required directories exist (bootstrap, argocd/apps, argocd/infrastructure, argocd/operators, argocd/security)
  - Required bootstrap files exist (kustomization.yaml, app-of-apps.yaml, infra-app-of-apps.yaml, operators-app-of-apps.yaml, security-app-of-apps.yaml)
  - Kustomize builds succeed for all cluster paths
  - Infrastructure/operators/security component structure (base/overlays pattern)
  - App overlay structure uses cluster layer (apps/<app>/overlays/<cluster>/<env>/) - issue #1256
  - ArgoCD Application paths match expected structure
  - Namespace definitions are in approved locations (security/namespaces/ only)
  - Legacy namespaces in infrastructure/namespaces/ (warns for migration)
  - No duplicate namespace definitions across the repo
  - Applications don't use CreateNamespace=true (namespaces should be platform-managed)

Examples:
  shadow validate --repo /path/to/homelab-k8s
  shadow validate --repo . --cluster home
  shadow validate --repo . --output json
  shadow validate --repo . --strict`,
	RunE: runValidate,
}

func init() {
	rootCmd.AddCommand(validateCmd)

	validateCmd.Flags().StringVarP(&clusterFilter, "cluster", "c", "", "Validate only this cluster")
	validateCmd.Flags().StringVarP(&outputFormat, "output", "o", "table", "Output format: table, json")
	validateCmd.Flags().BoolVar(&strict, "strict", false, "Treat warnings as errors")
}

func runValidate(cmd *cobra.Command, args []string) error {
	validator := validate.NewClusterValidator(repoDir, verbose)

	// Discover clusters
	clusters, err := validator.DiscoverClusters()
	if err != nil {
		return fmt.Errorf("failed to discover clusters: %w", err)
	}

	if len(clusters) == 0 {
		return fmt.Errorf("no clusters found in %s/clusters/", repoDir)
	}

	logInfo("Discovered %d cluster(s): %s", len(clusters), strings.Join(clusters, ", "))

	// Filter if requested
	if clusterFilter != "" {
		found := false
		for _, c := range clusters {
			if c == clusterFilter {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("cluster %q not found (available: %s)", clusterFilter, strings.Join(clusters, ", "))
		}
		clusters = []string{clusterFilter}
	}

	// Run validation
	allResults := []validate.Result{} // Initialize to empty slice for JSON output
	for _, cluster := range clusters {
		logInfo("Validating cluster: %s", cluster)
		results := validator.ValidateCluster(cluster)
		allResults = append(allResults, results...)
	}

	// Run infrastructure validation (new pattern enforcement)
	logInfo("Validating infrastructure structure...")
	infraResults := validator.ValidateInfrastructure(clusters)
	allResults = append(allResults, infraResults...)

	// Run namespace location validation (issue #950)
	logInfo("Validating namespace locations...")
	nsResults := validator.ValidateNamespaceLocations()
	allResults = append(allResults, nsResults...)

	// Run CreateNamespace validation (issue #950)
	logInfo("Validating CreateNamespace usage...")
	createNsResults := validator.ValidateCreateNamespace()
	allResults = append(allResults, createNsResults...)

	// Run app overlay structure validation (issue #1256)
	logInfo("Validating app overlay structure...")
	appOverlayResults := validator.ValidateAppOverlayStructure(clusters)
	allResults = append(allResults, appOverlayResults...)

	// Run ArgoCD app path validation (issue #1256)
	logInfo("Validating ArgoCD app paths...")
	argoCDPathResults := validator.ValidateArgoCDAppPaths(clusters)
	allResults = append(allResults, argoCDPathResults...)

	// Output results
	switch outputFormat {
	case "json":
		return outputJSON(allResults)
	case "table":
		return outputTable(allResults)
	default:
		return fmt.Errorf("unknown output format: %s", outputFormat)
	}
}

func outputJSON(results []validate.Result) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(results); err != nil {
		return fmt.Errorf("failed to encode JSON: %w", err)
	}
	return checkExitCode(results)
}

func outputTable(results []validate.Result) error {
	errors := validate.CountErrors(results)
	warnings := validate.CountWarnings(results)

	if len(results) == 0 {
		fmt.Println("\n✅ All validations passed!")
		return nil
	}

	// Print results table
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "\nSEVERITY\tCLUSTER\tRULE\tPATH\tMESSAGE")
	fmt.Fprintln(w, "--------\t-------\t----\t----\t-------")

	for _, r := range results {
		icon := "⚠️ "
		if r.Severity == "error" {
			icon = "❌"
		}
		fmt.Fprintf(w, "%s %s\t%s\t%s\t%s\t%s\n",
			icon, strings.ToUpper(r.Severity), r.Cluster, r.Rule, r.Path, r.Message)
	}
	w.Flush()

	// Print summary
	fmt.Printf("\nSummary: %d error(s), %d warning(s)\n", errors, warnings)

	return checkExitCode(results)
}

func checkExitCode(results []validate.Result) error {
	errors := validate.CountErrors(results)
	warnings := validate.CountWarnings(results)

	if errors > 0 {
		return fmt.Errorf("validation failed with %d error(s)", errors)
	}
	if strict && warnings > 0 {
		return fmt.Errorf("validation failed with %d warning(s) (strict mode)", warnings)
	}
	return nil
}
