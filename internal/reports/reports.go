package reports

import (
	"fmt"
	"strings"

	"github.com/rafliandi13/data-engineer-test/internal/database"
)

// Report defines the interface for all reports.
type Report interface {
	Name() string
	Description() string
	SQL(startDate, endDate string) string
}

// Registry stores all available reports.
type Registry struct {
	reports map[string]Report
}

// NewRegistry creates a registry with all registered reports.
func NewRegistry() *Registry {
	r := &Registry{
		reports: make(map[string]Report),
	}

	r.Register(&CohortAnalysis{})
	r.Register(&RFMAnalysis{})
	r.Register(&SalesTrend{})

	return r
}

// Register adds a report to the registry.
func (r *Registry) Register(report Report) {
	r.reports[report.Name()] = report
}

// Get retrieves a report by name.
func (r *Registry) Get(name string) (Report, error) {
	report, ok := r.reports[name]
	if !ok {
		return nil, fmt.Errorf("report '%s' not found. Available reports: %s",
			name, r.ListNames())
	}
	return report, nil
}

// ListNames returns a list of available report names.
func (r *Registry) ListNames() string {
	names := make([]string, 0, len(r.reports))
	for name := range r.reports {
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

// Generate executes a report and returns its results.
func Generate(db *database.DB, report Report, startDate, endDate string) (*database.QueryResult, error) {
	query := report.SQL(startDate, endDate)
	return db.ExecuteQuery(query)
}

// ---------------------------------------------------------------------------
// Report: Customer Cohort Analysis
// ---------------------------------------------------------------------------

// CohortAnalysis calculates monthly customer acquisition, retention, and running totals.
type CohortAnalysis struct{}

func (c *CohortAnalysis) Name() string        { return "cohort-analysis" }
func (c *CohortAnalysis) Description() string { return "Monthly customer cohort analysis with retention rates" }

func (c *CohortAnalysis) SQL(startDate, endDate string) string {
	if startDate == "" {
		startDate = "2024-01-01"
	}
	if endDate == "" {
		endDate = "2025-01-01"
	}

	return fmt.Sprintf(`
WITH first_orders AS (
    SELECT
        o.customer_id,
        DATE_TRUNC('month', MIN(o.order_date)) AS cohort_month
    FROM orders o
    WHERE o.order_date >= '%s'
      AND o.order_date < '%s'
    GROUP BY o.customer_id
),
cohort_revenue AS (
    SELECT
        fo.customer_id,
        fo.cohort_month,
        SUM(o.total_amount) AS first_month_revenue
    FROM first_orders fo
    JOIN orders o
        ON o.customer_id = fo.customer_id
       AND DATE_TRUNC('month', o.order_date) = fo.cohort_month
       AND o.status = 'completed'
    GROUP BY fo.customer_id, fo.cohort_month
),
cohort_metrics AS (
    SELECT
        fo.cohort_month,
        COUNT(DISTINCT fo.customer_id)          AS new_customers,
        COALESCE(SUM(cr.first_month_revenue), 0) AS cohort_first_month_revenue
    FROM first_orders fo
    LEFT JOIN cohort_revenue cr
        ON cr.customer_id = fo.customer_id
       AND cr.cohort_month = fo.cohort_month
    GROUP BY fo.cohort_month
),
retention AS (
    SELECT
        fo.cohort_month,
        COUNT(DISTINCT fo.customer_id) AS retained_customers
    FROM first_orders fo
    WHERE EXISTS (
        SELECT 1 FROM orders o2
        WHERE o2.customer_id = fo.customer_id
          AND DATE_TRUNC('month', o2.order_date) = fo.cohort_month + INTERVAL '1 month'
    )
    GROUP BY fo.cohort_month
)
SELECT
    TO_CHAR(cm.cohort_month, 'YYYY-MM')        AS cohort_month,
    cm.new_customers,
    cm.cohort_first_month_revenue,
    SUM(cm.new_customers) OVER (
        ORDER BY cm.cohort_month
        ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
    )                                            AS running_total_customers,
    COALESCE(r.retained_customers, 0)            AS retained_next_month,
    ROUND(
        COALESCE(r.retained_customers, 0)::NUMERIC
        / NULLIF(cm.new_customers, 0) * 100, 2
    )                                            AS retention_rate_pct
FROM cohort_metrics cm
LEFT JOIN retention r ON r.cohort_month = cm.cohort_month
ORDER BY cm.cohort_month`, startDate, endDate)
}

// ---------------------------------------------------------------------------
// Report: RFM Customer Segmentation
// ---------------------------------------------------------------------------

// RFMAnalysis performs Recency-Frequency-Monetary segmentation.
type RFMAnalysis struct{}

func (r *RFMAnalysis) Name() string        { return "rfm-analysis" }
func (r *RFMAnalysis) Description() string { return "Customer RFM segmentation analysis" }

func (r *RFMAnalysis) SQL(_, _ string) string {
	return `
WITH rfm_raw AS (
    SELECT
        c.id AS customer_id, c.name, c.email,
        EXTRACT(DAY FROM CURRENT_TIMESTAMP - MAX(o.order_date)) AS recency_days,
        COUNT(o.id) AS frequency,
        SUM(o.total_amount) AS monetary
    FROM customers c
    JOIN orders o ON o.customer_id = c.id
    WHERE o.status = 'completed'
    GROUP BY c.id, c.name, c.email
),
rfm_scored AS (
    SELECT *, 
        CASE WHEN recency_days <= 30 THEN 5 WHEN recency_days <= 90 THEN 4
             WHEN recency_days <= 180 THEN 3 WHEN recency_days <= 365 THEN 2 ELSE 1 END AS r_score,
        CASE WHEN frequency >= 20 THEN 5 WHEN frequency >= 10 THEN 4
             WHEN frequency >= 5 THEN 3 WHEN frequency >= 3 THEN 2 ELSE 1 END AS f_score,
        NTILE(5) OVER (ORDER BY monetary ASC) AS m_score
    FROM rfm_raw
)
SELECT name, email, recency_days, frequency, ROUND(monetary, 2) AS monetary,
    r_score, f_score, m_score,
    (r_score + f_score + m_score) AS rfm_score,
    CASE
        WHEN (r_score + f_score + m_score) >= 12 THEN 'Champions'
        WHEN (r_score + f_score + m_score) >= 9  THEN 'Loyal'
        WHEN (r_score + f_score + m_score) >= 6  THEN 'At Risk'
        ELSE 'Lost'
    END AS segment
FROM rfm_scored
ORDER BY rfm_score DESC, monetary DESC`
}

// ---------------------------------------------------------------------------
// Report: Sales Trend Analysis
// ---------------------------------------------------------------------------

// SalesTrend calculates daily sales metrics with moving averages and anomaly detection.
type SalesTrend struct{}

func (s *SalesTrend) Name() string        { return "sales-trend" }
func (s *SalesTrend) Description() string { return "90-day sales trend with 7-day moving averages and anomaly flags" }

func (s *SalesTrend) SQL(_, _ string) string {
	return `
WITH date_series AS (
    SELECT d::DATE AS sale_date
    FROM generate_series(
        CURRENT_DATE - INTERVAL '89 days', CURRENT_DATE, '1 day'::INTERVAL
    ) AS d
),
daily_sales AS (
    SELECT d.sale_date,
        COUNT(o.id) AS total_orders,
        COALESCE(SUM(o.total_amount), 0) AS total_revenue
    FROM date_series d
    LEFT JOIN orders o
        ON o.order_date::DATE = d.sale_date AND o.status = 'completed'
    GROUP BY d.sale_date
),
with_moving_avg AS (
    SELECT sale_date, total_orders, total_revenue,
        ROUND(AVG(total_revenue) OVER (
            ORDER BY sale_date ROWS BETWEEN 6 PRECEDING AND CURRENT ROW
        ), 2) AS revenue_7day_avg,
        ROUND(AVG(total_orders) OVER (
            ORDER BY sale_date ROWS BETWEEN 6 PRECEDING AND CURRENT ROW
        ), 2) AS orders_7day_avg,
        TRIM(TO_CHAR(sale_date, 'Day')) AS day_of_week
    FROM daily_sales
)
SELECT sale_date, day_of_week, total_orders, total_revenue,
    revenue_7day_avg, orders_7day_avg,
    CASE WHEN revenue_7day_avg > 0
        THEN ROUND((total_revenue - revenue_7day_avg) / revenue_7day_avg * 100, 2)
        ELSE 0 END AS pct_diff_from_avg,
    CASE
        WHEN revenue_7day_avg > 0 AND total_revenue > revenue_7day_avg * 1.30 THEN 'ABOVE +30%'
        WHEN revenue_7day_avg > 0 AND total_revenue < revenue_7day_avg * 0.70 THEN 'BELOW -30%'
        ELSE 'Normal'
    END AS anomaly_flag
FROM with_moving_avg
ORDER BY sale_date`
}
