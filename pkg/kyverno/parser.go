package kyverno

import (
	"regexp"
	"strconv"
	"strings"
)

// TestSummary represents parsed test results
type TestSummary struct {
	Passed int
	Failed int
	Total  int
}

// ParseSummary parses the "Test Summary: X tests passed and Y tests failed" line
func ParseSummary(output string) TestSummary {
	summary := TestSummary{}

	// Pattern: "Test Summary: X tests passed and Y tests failed"
	re := regexp.MustCompile(`Test Summary:\s*(\d+)\s+tests?\s+passed\s+and\s+(\d+)\s+tests?\s+failed`)
	matches := re.FindStringSubmatch(output)

	if len(matches) == 3 {
		summary.Passed, _ = strconv.Atoi(matches[1])
		summary.Failed, _ = strconv.Atoi(matches[2])
		summary.Total = summary.Passed + summary.Failed
	}

	return summary
}

// DetailedResult represents a single test case result from detailed output
type DetailedResult struct {
	ID       int
	Policy   string
	Rule     string
	Resource string
	Result   string // "Pass" or "Fail"
	Reason   string
}

// ParseDetailedResults parses the table output from --detailed-results
func ParseDetailedResults(output string) []DetailedResult {
	var results []DetailedResult

	lines := strings.Split(output, "\n")
	inTable := false

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Detect table start (header row with ID | POLICY | RULE | ...)
		if strings.HasPrefix(line, "ID") && strings.Contains(line, "POLICY") && strings.Contains(line, "RESULT") {
			inTable = true
			continue
		}

		// Skip separator lines
		if strings.HasPrefix(line, "--") || strings.HasPrefix(line, "==") {
			continue
		}

		// Skip empty lines
		if line == "" {
			continue
		}

		// Parse table rows
		if inTable && strings.Contains(line, "|") {
			result := parseTableRow(line)
			if result != nil {
				results = append(results, *result)
			}
		}
	}

	return results
}

// parseTableRow parses a single row from the kyverno test output table
func parseTableRow(line string) *DetailedResult {
	// Split by | and trim whitespace
	parts := strings.Split(line, "|")
	if len(parts) < 5 {
		return nil
	}

	// Clean up each part
	cleaned := make([]string, len(parts))
	for i, p := range parts {
		cleaned[i] = strings.TrimSpace(p)
	}

	// Try to parse ID
	id, err := strconv.Atoi(cleaned[0])
	if err != nil {
		// Not a data row
		return nil
	}

	result := &DetailedResult{
		ID: id,
	}

	// Map remaining fields (order may vary)
	if len(cleaned) > 1 {
		result.Policy = cleaned[1]
	}
	if len(cleaned) > 2 {
		result.Rule = cleaned[2]
	}
	if len(cleaned) > 3 {
		result.Resource = cleaned[3]
	}
	if len(cleaned) > 4 {
		result.Result = cleaned[4]
	}
	if len(cleaned) > 5 {
		result.Reason = cleaned[5]
	}

	return result
}

// CountResults counts passed and failed results
func CountResults(results []DetailedResult) (passed, failed int) {
	for _, r := range results {
		switch strings.ToLower(r.Result) {
		case "pass":
			passed++
		case "fail":
			failed++
		}
	}
	return
}

// HasFailures checks if any results failed
func HasFailures(output string) bool {
	// Quick check for failure indicators
	if strings.Contains(output, "| Fail |") || strings.Contains(output, "|Fail|") {
		return true
	}

	// Parse summary
	summary := ParseSummary(output)
	if summary.Failed > 0 {
		return true
	}

	// Parse detailed results
	results := ParseDetailedResults(output)
	_, failed := CountResults(results)
	return failed > 0
}
