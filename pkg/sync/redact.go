package sync

import (
	"regexp"
	"strings"
)

// RedactSecrets removes sensitive data from Kubernetes Secret resources
// while preserving the rest of the manifest structure for stable diffs.
//
// This uses text-based processing to avoid YAML re-serialization which
// would cause key reordering and diff noise.
func RedactSecrets(manifest string) string {
	// Split into YAML documents
	docs := splitYAMLDocuments(manifest)

	var result []string
	for _, doc := range docs {
		if isSecretDocument(doc) {
			doc = redactSecretDocument(doc)
		}
		result = append(result, doc)
	}

	return joinYAMLDocuments(result)
}

// splitYAMLDocuments splits a multi-document YAML string on --- boundaries
func splitYAMLDocuments(manifest string) []string {
	// Split on document separator
	parts := strings.Split(manifest, "\n---")

	var docs []string
	for i, part := range parts {
		if i == 0 {
			// First part doesn't have leading ---
			docs = append(docs, part)
		} else {
			// Restore the separator for consistency
			docs = append(docs, "---"+part)
		}
	}

	return docs
}

// joinYAMLDocuments joins YAML documents back together
func joinYAMLDocuments(docs []string) string {
	if len(docs) == 0 {
		return ""
	}

	var result strings.Builder
	for i, doc := range docs {
		if i > 0 {
			// Ensure previous document ends with newline so separator is on its own line
			str := result.String()
			if len(str) > 0 && !strings.HasSuffix(str, "\n") {
				result.WriteString("\n")
			}
			if !strings.HasPrefix(doc, "---") {
				result.WriteString("---\n")
			}
		}
		result.WriteString(doc)
	}

	return result.String()
}

// isSecretDocument checks if a YAML document is a Kubernetes Secret
func isSecretDocument(doc string) bool {
	// Match "kind: Secret" at the beginning of a line
	kindPattern := regexp.MustCompile(`(?m)^kind:\s*Secret\s*$`)
	return kindPattern.MatchString(doc)
}

// redactSecretDocument redacts data/stringData/binaryData from a Secret document
func redactSecretDocument(doc string) string {
	// Patterns for secret data fields
	// These match the field and its entire block (indented content below)
	dataPatterns := []string{
		`data:`,
		`stringData:`,
		`binaryData:`,
	}

	lines := strings.Split(doc, "\n")
	var result []string

	skipUntilIndent := -1 // -1 means not skipping
	redactedField := ""

	for i, line := range lines {
		// Check if we're currently skipping a block
		if skipUntilIndent >= 0 {
			// Calculate current line's indentation
			currentIndent := countIndent(line)

			// Empty lines or lines with greater indentation are part of the block
			if line == "" || currentIndent > skipUntilIndent {
				continue // Skip this line
			}

			// We've reached a line with equal or less indentation - stop skipping
			skipUntilIndent = -1
		}

		// Check if this line starts a data block to redact
		trimmed := strings.TrimSpace(line)
		for _, pattern := range dataPatterns {
			if trimmed == pattern || strings.HasPrefix(trimmed, pattern+" ") {
				indent := countIndent(line)

				// Add the redacted field header
				result = append(result, line)

				// Add a REDACTED placeholder at the next indentation level
				nextIndent := strings.Repeat(" ", indent+2)
				result = append(result, nextIndent+"# REDACTED - secrets are not included in shadow diffs")

				redactedField = pattern
				skipUntilIndent = indent

				// Check if this is an inline empty value like "data: {}"
				if strings.Contains(trimmed, "{}") || strings.Contains(trimmed, "{ }") {
					skipUntilIndent = -1 // Don't skip, it's inline
				}

				break
			}
		}

		// If we started skipping, continue to next line
		if skipUntilIndent >= 0 && redactedField != "" {
			redactedField = ""
			continue
		}

		// Check if this is the next line after we started a block
		if i > 0 && skipUntilIndent >= 0 {
			continue
		}

		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

// countIndent returns the number of leading spaces in a line
func countIndent(line string) int {
	count := 0
	for _, ch := range line {
		if ch == ' ' {
			count++
		} else if ch == '\t' {
			count += 2 // Treat tabs as 2 spaces
		} else {
			break
		}
	}
	return count
}
