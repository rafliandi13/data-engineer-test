package export

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rafliandi13/data-engineer-test/internal/database"
	"github.com/rafliandi13/data-engineer-test/internal/logger"
)

// ToJSON exports a QueryResult to a neatly formatted JSON file.
// The filename is automatically timestamped.
func ToJSON(result *database.QueryResult, outputDir, reportName string) (string, error) {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory %s: %w", outputDir, err)
	}

	filename := fmt.Sprintf("%s_%s.json", reportName, logger.Timestamp())
	path := filepath.Join(outputDir, filename)

	data, err := json.MarshalIndent(result.Rows, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write file %s: %w", path, err)
	}

	return path, nil
}

// ToCSV exports a QueryResult to a CSV file.
func ToCSV(result *database.QueryResult, outputDir, reportName string) (string, error) {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory %s: %w", outputDir, err)
	}

	filename := fmt.Sprintf("%s_%s.csv", reportName, logger.Timestamp())
	path := filepath.Join(outputDir, filename)

	file, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("failed to create file %s: %w", path, err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Header
	if err := writer.Write(result.Columns); err != nil {
		return "", fmt.Errorf("failed to write CSV header: %w", err)
	}

	// Rows
	for _, row := range result.Rows {
		record := make([]string, len(result.Columns))
		for i, col := range result.Columns {
			record[i] = fmt.Sprintf("%v", row[col])
		}
		if err := writer.Write(record); err != nil {
			return "", fmt.Errorf("failed to write CSV row: %w", err)
		}
	}

	return path, nil
}
