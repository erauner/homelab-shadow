package kustomize

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// KubeconformSummary represents parsed kubeconform summary
type KubeconformSummary struct {
	Resources int
	Valid     int
	Invalid   int
	Errors    int
	Skipped   int
}

// ParseKubeconformSummary parses the kubeconform summary line
// Example: "Summary: 4 resources found in 1 file - Valid: 4, Invalid: 0, Errors: 0, Skipped: 0"
func ParseKubeconformSummary(output string) KubeconformSummary {
	summary := KubeconformSummary{}

	// Pattern for summary line
	re := regexp.MustCompile(`Summary:\s*(\d+)\s+resources?\s+found.*Valid:\s*(\d+),\s*Invalid:\s*(\d+),\s*Errors:\s*(\d+),\s*Skipped:\s*(\d+)`)
	matches := re.FindStringSubmatch(output)

	if len(matches) == 6 {
		summary.Resources, _ = strconv.Atoi(matches[1])
		summary.Valid, _ = strconv.Atoi(matches[2])
		summary.Invalid, _ = strconv.Atoi(matches[3])
		summary.Errors, _ = strconv.Atoi(matches[4])
		summary.Skipped, _ = strconv.Atoi(matches[5])
	}

	return summary
}

// KubeconformError represents a single validation error
type KubeconformError struct {
	Level    string // ERRO, WARN
	Resource string
	Message  string
}

// ParseKubeconformErrors extracts error lines from kubeconform output
func ParseKubeconformErrors(output string) []KubeconformError {
	var errors []KubeconformError

	lines := strings.Split(output, "\n")
	// Pattern: ERRO - resource: message or WARN - resource: message
	re := regexp.MustCompile(`^(ERRO|WARN)\s+-\s+([^:]+):\s*(.*)$`)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		matches := re.FindStringSubmatch(line)
		if len(matches) == 4 {
			errors = append(errors, KubeconformError{
				Level:    matches[1],
				Resource: matches[2],
				Message:  matches[3],
			})
		}
	}

	return errors
}

// HasKubeconformErrors checks if the output contains validation errors
func HasKubeconformErrors(output string) bool {
	summary := ParseKubeconformSummary(output)
	if summary.Invalid > 0 || summary.Errors > 0 {
		return true
	}

	// Also check for ERRO lines
	return strings.Contains(output, "ERRO -")
}

// ExtractKustomizeBuildError extracts the error message from kustomize build output
func ExtractKustomizeBuildError(output string) string {
	lines := strings.Split(output, "\n")
	var errorLines []string

	// Look for Error: lines or the last few lines which usually contain the error
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Error:") || strings.HasPrefix(line, "error:") {
			errorLines = append(errorLines, line)
		}
	}

	if len(errorLines) > 0 {
		return strings.Join(errorLines, "\n")
	}

	// If no explicit error lines, return the last few non-empty lines
	var lastLines []string
	for i := len(lines) - 1; i >= 0 && len(lastLines) < 5; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			lastLines = append([]string{line}, lastLines...)
		}
	}

	return strings.Join(lastLines, "\n")
}

// FormatValidationError creates a concise error message for a validation failure
func FormatValidationError(result ValidationResult) string {
	var parts []string

	parts = append(parts, fmt.Sprintf("Directory: %s", result.Directory))

	if !result.BuildPassed {
		parts = append(parts, "Build: FAILED")
		errorMsg := ExtractKustomizeBuildError(result.BuildOutput)
		if errorMsg != "" {
			parts = append(parts, errorMsg)
		}
	} else if !result.SchemaPassed {
		parts = append(parts, "Build: OK")
		parts = append(parts, "Schema: FAILED")

		errors := ParseKubeconformErrors(result.SchemaOutput)
		if len(errors) > 0 {
			for _, e := range errors {
				parts = append(parts, fmt.Sprintf("  %s: %s - %s", e.Level, e.Resource, e.Message))
			}
		} else {
			// Fall back to raw output
			summary := ParseKubeconformSummary(result.SchemaOutput)
			parts = append(parts, fmt.Sprintf("  Invalid: %d, Errors: %d", summary.Invalid, summary.Errors))
		}
	}

	return strings.Join(parts, "\n")
}
