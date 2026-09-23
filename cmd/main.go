package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/rafliandi13/data-engineer-test/internal/api"
	"github.com/rafliandi13/data-engineer-test/internal/config"
	"github.com/rafliandi13/data-engineer-test/internal/database"
	"github.com/rafliandi13/data-engineer-test/internal/export"
	"github.com/rafliandi13/data-engineer-test/internal/logger"
	"github.com/rafliandi13/data-engineer-test/internal/reports"
)

func main() {
	// CLI flags
	reportName := flag.String("report", "", "Report name: cohort-analysis, rfm-analysis, sales-trend, daily-sales-api")
	outputDir := flag.String("output", "reports/", "Output directory for result files")
	startDate := flag.String("start-date", "", "Start date (YYYY-MM-DD), optional")
	endDate := flag.String("end-date", "", "End date (YYYY-MM-DD), optional")
	configPath := flag.String("config", "", "Path to JSON config file")
	format := flag.String("format", "json", "Output format: json or csv")
	dryRun := flag.Bool("dry-run", false, "Show SQL without executing")
	flag.Parse()

	log := logger.New()

	if *reportName == "" {
		fmt.Println("Usage: go run cmd/main.go --report=<report-name> [options]")
		fmt.Println()
		fmt.Println("Available reports:")
		fmt.Println("  cohort-analysis    Customer cohort analysis with retention rates")
		fmt.Println("  rfm-analysis       Customer RFM segmentation")
		fmt.Println("  sales-trend        90-day sales trend with anomaly detection")
		fmt.Println("  daily-sales-api    Generate & send daily sales report to BI API")
		fmt.Println()
		fmt.Println("Options:")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// Handle daily-sales-api as a separate command
	if *reportName == "daily-sales-api" {
		runDailySalesAPI(log, *configPath, *startDate)
		return
	}

	// Load report from registry
	registry := reports.NewRegistry()
	report, err := registry.Get(*reportName)
	if err != nil {
		log.Error("%v", err)
		os.Exit(1)
	}

	// Dry run: show SQL and exit
	if *dryRun {
		fmt.Println("-- Dry Run: SQL query for report", report.Name())
		fmt.Println(report.SQL(*startDate, *endDate))
		return
	}

	// Load config
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Error("Failed to load config: %v", err)
		os.Exit(1)
	}

	// Connect to database
	db, err := database.New(cfg.Database, log)
	if err != nil {
		log.Error("Failed to connect to database: %v", err)
		os.Exit(1)
	}
	defer db.Close()

	// Execute report
	start := time.Now()
	result, err := reports.Generate(db, report, *startDate, *endDate)
	if err != nil {
		log.Error("Failed to generate report: %v", err)
		os.Exit(1)
	}

	// Export results
	var outputPath string
	switch *format {
	case "csv":
		outputPath, err = export.ToCSV(result, *outputDir, report.Name())
	default:
		outputPath, err = export.ToJSON(result, *outputDir, report.Name())
	}

	if err != nil {
		log.Error("Failed to export: %v", err)
		os.Exit(1)
	}

	log.LogExecution(report.Name(), time.Since(start), len(result.Rows), outputPath, nil)
	fmt.Printf("Report '%s' generated successfully: %s\n", report.Name(), outputPath)
}

// runDailySalesAPI fetches daily sales from the DB and sends them to the BI platform API.
func runDailySalesAPI(log *logger.Logger, configPath, date string) {
	if date == "" {
		date = time.Now().AddDate(0, 0, -1).Format("2006-01-02") // Default: yesterday
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Error("Failed to load config: %v", err)
		os.Exit(1)
	}

	if cfg.API.BaseURL == "" || cfg.API.BearerToken == "" {
		log.Error("API base_url and bearer_token are required in the config for daily-sales-api")
		os.Exit(1)
	}

	db, err := database.New(cfg.Database, log)
	if err != nil {
		log.Error("Failed to connect to database: %v", err)
		os.Exit(1)
	}
	defer db.Close()

	// Run daily sales query
	query := api.DailySalesQuery(date)
	result, err := db.ExecuteQuery(query)
	if err != nil {
		log.Error("Failed to query daily sales: %v", err)
		os.Exit(1)
	}

	// Build payload
	payload, err := api.BuildPayload(result, date)
	if err != nil {
		log.Error("Failed to build payload: %v", err)
		os.Exit(1)
	}

	// Send to API
	client := api.NewClient(cfg.API.BaseURL, cfg.API.BearerToken, log)
	if err := client.SendReport(payload); err != nil {
		log.Error("Failed to send report: %v", err)
		os.Exit(1)
	}

	fmt.Printf("Daily sales report for %s sent successfully to %s\n", date, cfg.API.BaseURL)
}
