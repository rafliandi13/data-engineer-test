# E-Commerce Order Analytics System

My solution for the Data Automation & Retrieval Engineer screening test. Covers all 3 parts: SQL queries, Go CLI tool, and system design.

## Quick Start

**What you need:** Go 1.22+, PostgreSQL 14+, and the e-commerce schema from the test.

```bash
git clone https://github.com/rafliandi13/data-engineer-test.git
cd data-engineer-test

cp config.json.example config.json
# fill in your DB credentials

go mod tidy
go run cmd/main.go --report=cohort-analysis --config=config.json --output=reports/
```

---

## Part 1: Schema Analysis & SQL Queries

### 1.1 Schema Analysis

#### Performance Issues

At scale (1M+ customers, 10M+ orders, 50M+ order_items), this schema has a few problems:

1. **No indexes besides PKs** — any filter on `order_date`, `customer_id`, `product_id`, or `status` ends up doing a full table scan. On 10M+ rows that's painful.

2. **JOINs without index support** — joining `orders` to `order_items` (10M × 50M) without FK indexes forces PostgreSQL into either nested loops or memory-heavy hash joins.

3. **VARCHAR for status** — `status VARCHAR(50)` stores the same 3 strings over and over. An ENUM or lookup table would be smaller and faster to compare.

4. **No partitioning** — `orders` and `order_items` keep growing. A query for "last month's data" still scans the whole table.

5. **`total_amount` can drift out of sync** — if it's derived from `order_items` but not recalculated when items change, you get inconsistent data.

#### Index Recommendations

```sql
-- date filter (most analytical queries filter by date)
CREATE INDEX idx_orders_order_date ON orders (order_date);

-- FK join: orders → customers
CREATE INDEX idx_orders_customer_id ON orders (customer_id);

-- FK joins: order_items → orders and products
CREATE INDEX idx_order_items_order_id ON order_items (order_id);
CREATE INDEX idx_order_items_product_id ON order_items (product_id);

-- composite: status + date (common pattern: "completed orders this month")
CREATE INDEX idx_orders_status_date ON orders (status, order_date);

-- category grouping/filtering
CREATE INDEX idx_products_category ON products (category);
```

#### Schema Improvements

**1. ENUM for status:**

```sql
CREATE TYPE order_status AS ENUM ('pending', 'completed', 'cancelled');
ALTER TABLE orders ALTER COLUMN status TYPE order_status USING status::order_status;
```

Gives you DB-level validation, smaller storage (4 bytes), and faster comparisons.

**2. Add `updated_at`:**

```sql
ALTER TABLE orders ADD COLUMN updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP;
```

Without this there's no way to know *when* an order was completed or cancelled — you need that for things like fulfillment time analysis.

**3. Tighter NOT NULL constraints:**

```sql
ALTER TABLE orders ALTER COLUMN order_date SET NOT NULL;
ALTER TABLE orders ALTER COLUMN customer_id SET NOT NULL;

ALTER TABLE customers ALTER COLUMN country SET DEFAULT 'Unknown';
ALTER TABLE customers ALTER COLUMN country SET NOT NULL;
```

Prevents dirty data from sneaking in and causing weird NULLs in your reports.

### 1.2 SQL Queries

All 6 queries live in [`queries.sql`](queries.sql). Each one has comments explaining the approach, why I picked certain techniques, optimization notes, and assumptions.

| # | Query | Techniques |
|---|-------|------------|
| 1 | Customer Cohort Analysis | CTE, `SUM() OVER`, retention calc |
| 2 | Product Performance Ranking | `RANK()`, `PERCENT_RANK()`, `SUM() OVER PARTITION`, `LAG()` |
| 3 | RFM Customer Segmentation | `CASE`, `NTILE(5)`, multi-step CTE |
| 4 | Sales Trend & Anomaly Detection | `generate_series`, moving average, anomaly flags |
| 5 | Inventory Turnover | Complex `CASE`, stock math, reorder calc |
| 6 | Customer Purchase Patterns | `LAG()`, `STDDEV()`, `ROW_NUMBER()`, trend indicator |

---

## Part 2: Go Automation Tool

### How it's structured

```
cmd/main.go           → CLI entry point, flag parsing
internal/
  config/config.go    → reads JSON config + env var overrides
  database/database.go→ PostgreSQL pool + generic query runner
  reports/reports.go  → Report interface + 3 implementations
  export/export.go    → JSON and CSV writers
  api/client.go       → REST client with retry (for BI platform)
  logger/logger.go    → simple structured logger
```

**Why this layout?** Each report implements a `Report` interface (`Name()`, `Description()`, `SQL()`), so adding a new one is just a struct + one `Register()` call — no touching main.go. Query results are `[]map[string]interface{}` which isn't type-safe but works well when you're running arbitrary SQL and don't know the columns ahead of time.

### Usage

```bash
# cohort analysis → JSON
go run cmd/main.go --report=cohort-analysis --config=config.json --output=reports/

# RFM → CSV
go run cmd/main.go --report=rfm-analysis --config=config.json --format=csv

# sales trend
go run cmd/main.go --report=sales-trend --config=config.json

# just show the SQL, don't run it
go run cmd/main.go --report=cohort-analysis --dry-run

# send daily sales to BI API
go run cmd/main.go --report=daily-sales-api --config=config.json

# specific date
go run cmd/main.go --report=daily-sales-api --config=config.json --start-date=2024-11-29
```

### Configuration

You can use a JSON config file, env vars, or both (env vars override the file).

**Config file** (`config.json`):

```json
{
  "database": {
    "host": "localhost",
    "port": 5432,
    "user": "postgres",
    "password": "secret",
    "dbname": "ecommerce",
    "sslmode": "disable"
  },
  "api": {
    "base_url": "https://api.bi-platform.com",
    "bearer_token": "your_token"
  }
}
```

**Env vars:**

```bash
export DB_HOST=localhost
export DB_USER=postgres
export DB_PASSWORD=secret
export DB_NAME=ecommerce
```

### What it does

- Outputs JSON or CSV (`--format=csv`)
- `--dry-run` prints the SQL without hitting the database
- Files get timestamped names like `cohort-analysis_2024-11-29_080000.json`
- Connection pooling (10 open / 5 idle / 5min lifetime)
- Logs execution time, row count, and errors
- API mode sends daily sales to a BI platform with 3 retries + backoff
- `--start-date` / `--end-date` for date-filtered queries

---

## Part 3: System Design

Full writeup is in [`AUTOMATION_DESIGN.md`](AUTOMATION_DESIGN.md):

- **3.1** — How I'd automate the 4 recurring reports (scheduler, worker pool, notifications, and why I'd start with the low stock alert)
- **3.2** — Debugging the slow query (why it's slow, quick index fixes, and a rewritten version)
- **3.3** — REST API integration (already implemented in the Go tool, plus scheduling with cron)

---

## Folder Structure

```
.
├── README.md
├── AUTOMATION_DESIGN.md
├── queries.sql                   # 6 SQL queries (Part 1.2)
├── go.mod / go.sum
├── config.json.example
├── .env.example
├── cmd/
│   └── main.go
├── internal/
│   ├── config/config.go
│   ├── database/database.go
│   ├── reports/reports.go
│   ├── export/export.go
│   ├── api/client.go
│   └── logger/logger.go
└── sample_output/
    ├── cohort_analysis_sample.json
    ├── rfm_analysis_sample.json
    └── sales_trend_sample.json
```
