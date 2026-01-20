package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/erauner/homelab-shadow/pkg/validate"
	"github.com/spf13/cobra"
)

var listOutputFormat string

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List discovered clusters",
	Long:  `Lists all clusters found in the clusters/ directory.`,
	RunE:  runList,
}

func init() {
	rootCmd.AddCommand(listCmd)
	listCmd.Flags().StringVarP(&listOutputFormat, "output", "o", "text", "Output format: text, json")
}

func runList(cmd *cobra.Command, args []string) error {
	validator := validate.NewClusterValidator(repoDir, verbose)

	clusters, err := validator.DiscoverClusters()
	if err != nil {
		return fmt.Errorf("failed to discover clusters: %w", err)
	}

	switch listOutputFormat {
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(clusters)
	case "text":
		for _, c := range clusters {
			fmt.Println(c)
		}
		return nil
	default:
		return fmt.Errorf("unknown output format: %s", listOutputFormat)
	}
}
