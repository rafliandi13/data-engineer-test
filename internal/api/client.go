package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rafliandi13/data-engineer-test/internal/database"
	"github.com/rafliandi13/data-engineer-test/internal/logger"
)

// DailySalesPayload represents the payload sent to the BI platform.
type DailySalesPayload struct {
	ReportType  string         `json:"report_type"`
	Date        string         `json:"date"`
	Data        DailySalesData `json:"data"`
	GeneratedAt string         `json:"generated_at"`
}

// DailySalesData contains daily sales metrics.
type DailySalesData struct {
	TotalRevenue      float64 `json:"total_revenue"`
	TotalOrders       int     `json:"total_orders"`
	AverageOrderValue float64 `json:"average_order_value"`
	TopCategory       string  `json:"top_category"`
}

// Client handles communication with the BI platform API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	logger     *logger.Logger
	maxRetries int
}

// NewClient creates a new API client.
func NewClient(baseURL, token string, log *logger.Logger) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger:     log,
		maxRetries: 3,
	}
}

// DailySalesQuery returns the SQL for fetching a daily sales summary.
func DailySalesQuery(date string) string {
	return fmt.Sprintf(`
SELECT
    COALESCE(SUM(o.total_amount), 0)                 AS total_revenue,
    COUNT(o.id)                                       AS total_orders,
    CASE WHEN COUNT(o.id) > 0
        THEN ROUND(SUM(o.total_amount) / COUNT(o.id), 2)
        ELSE 0
    END                                               AS average_order_value,
    (
        SELECT p.category
        FROM order_items oi
        JOIN products p ON p.id = oi.product_id
        JOIN orders o2 ON o2.id = oi.order_id
        WHERE o2.order_date::DATE = '%s'
          AND o2.status = 'completed'
        GROUP BY p.category
        ORDER BY SUM(oi.quantity * oi.unit_price) DESC
        LIMIT 1
    )                                                 AS top_category
FROM orders o
WHERE o.order_date::DATE = '%s'
  AND o.status = 'completed'`, date, date)
}

// BuildPayload creates a DailySalesPayload from a query result.
func BuildPayload(result *database.QueryResult, date string) (*DailySalesPayload, error) {
	if len(result.Rows) == 0 {
		return nil, fmt.Errorf("query returned no data for date %s", date)
	}

	row := result.Rows[0]

	toFloat := func(v interface{}) float64 {
		switch val := v.(type) {
		case float64:
			return val
		case int64:
			return float64(val)
		case []byte:
			var f float64
			fmt.Sscanf(string(val), "%f", &f)
			return f
		case string:
			var f float64
			fmt.Sscanf(val, "%f", &f)
			return f
		default:
			return 0
		}
	}

	toInt := func(v interface{}) int {
		switch val := v.(type) {
		case int64:
			return int(val)
		case float64:
			return int(val)
		default:
			return 0
		}
	}

	topCategory := "N/A"
	if v, ok := row["top_category"]; ok && v != nil {
		topCategory = fmt.Sprintf("%v", v)
	}

	return &DailySalesPayload{
		ReportType: "daily_sales",
		Date:       date,
		Data: DailySalesData{
			TotalRevenue:      toFloat(row["total_revenue"]),
			TotalOrders:       toInt(row["total_orders"]),
			AverageOrderValue: toFloat(row["average_order_value"]),
			TopCategory:       topCategory,
		},
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// SendReport sends a daily sales report to the BI platform with retry logic.
func (c *Client) SendReport(payload *DailySalesPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= c.maxRetries; attempt++ {
		c.logger.Info("Sending report to API (attempt %d/%d)", attempt, c.maxRetries)

		req, err := http.NewRequest("POST", c.baseURL+"/v1/reports", bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.token)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			c.logger.Error("Attempt %d failed: %v", attempt, lastErr)
			time.Sleep(time.Duration(attempt) * 2 * time.Second) // exponential-ish backoff
			continue
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			c.logger.Info("Report sent successfully (HTTP %d)", resp.StatusCode)
			return nil
		}

		lastErr = fmt.Errorf("API returned HTTP %d: %s", resp.StatusCode, string(respBody))
		c.logger.Error("Attempt %d: %v", attempt, lastErr)

		// Do not retry for client errors (4xx) except 429 (rate limit)
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 429 {
			return lastErr
		}

		time.Sleep(time.Duration(attempt) * 2 * time.Second)
	}

	return fmt.Errorf("failed after %d attempts: %w", c.maxRetries, lastErr)
}
