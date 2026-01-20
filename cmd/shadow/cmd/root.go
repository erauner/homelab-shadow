// Package cmd implements the shadow CLI commands
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	verbose bool
	repoDir string
)

var rootCmd = &cobra.Command{
	Use:   "shadow",
	Short: "GitOps structure validation for multi-cluster deployments",
	Long: `Shadow validates the GitOps repository structure for multi-cluster
ArgoCD deployments. It ensures clusters follow the expected layout and
that all kustomize paths build successfully.

Example usage:
  shadow validate --repo /path/to/homelab-k8s
  shadow validate --repo . --cluster home
  shadow validate --repo . --strict`,
}

// Execute runs the root command
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
	rootCmd.PersistentFlags().StringVar(&repoDir, "repo", ".", "Path to homelab-k8s repository")

	rootCmd.SetOut(os.Stdout)
	rootCmd.SetErr(os.Stderr)
}

func logVerbose(format string, args ...interface{}) {
	if verbose {
		fmt.Fprintf(os.Stderr, "[shadow] "+format+"\n", args...)
	}
}

func logInfo(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}
