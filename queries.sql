-- =============================================================================
-- E-Commerce Order Analytics — SQL Queries
-- =============================================================================
-- Author: Rafli
-- Database: PostgreSQL
-- Schema: customers, products, orders, order_items
-- =============================================================================


-- =============================================================================
-- QUERY 1: Customer Cohort Analysis with Running Totals
-- =============================================================================
-- Idea: figure out which month each customer first ordered (their "cohort"),
-- then see how many came back the next month. CTEs made sense here because
-- each step builds on the previous one, and the retention check needs to
-- reference the first_orders CTE separately.
--
-- I used SUM() OVER for the running total instead of a correlated subquery —
-- same result, but the window function doesn't re-scan for every row.
--
-- Note: I'm counting all order statuses for cohort assignment (even cancelled),
-- since a cancelled order still shows the customer was acquired that month.
-- Revenue only counts completed orders though.
-- =============================================================================

WITH first_orders AS (
    -- when did each customer first order?
    SELECT
        o.customer_id,
        DATE_TRUNC('month', MIN(o.order_date)) AS cohort_month
    FROM orders o
    WHERE o.order_date >= '2024-01-01'
      AND o.order_date < '2025-01-01'
    GROUP BY o.customer_id
),

cohort_revenue AS (
    -- how much did they spend in that first month? (completed only)
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
    -- did they come back the following month?
    SELECT
        fo.cohort_month,
        COUNT(DISTINCT fo.customer_id) AS retained_customers
    FROM first_orders fo
    WHERE EXISTS (
        SELECT 1
        FROM orders o2
        WHERE o2.customer_id = fo.customer_id
          AND DATE_TRUNC('month', o2.order_date) = fo.cohort_month + INTERVAL '1 month'
    )
    GROUP BY fo.cohort_month
)

SELECT
    TO_CHAR(cm.cohort_month, 'YYYY-MM')                      AS cohort_month,
    cm.new_customers,
    cm.cohort_first_month_revenue,
    SUM(cm.new_customers) OVER (
        ORDER BY cm.cohort_month
        ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
    )                                                          AS running_total_customers,
    COALESCE(r.retained_customers, 0)                          AS retained_next_month,
    ROUND(
        COALESCE(r.retained_customers, 0)::NUMERIC
        / NULLIF(cm.new_customers, 0) * 100,
        2
    )                                                          AS retention_rate_pct
FROM cohort_metrics cm
LEFT JOIN retention r ON r.cohort_month = cm.cohort_month
ORDER BY cm.cohort_month;


-- =============================================================================
-- QUERY 2: Product Performance with Ranking and Comparison
-- =============================================================================
-- This one has a lot going on. The goal is a full product performance report:
-- all-time revenue, rank within category, % of category total, and MoM change.
--
-- I split it into 3 CTEs:
--   1. product_totals — all-time aggregates (one join to order_items + orders)
--   2. monthly_revenue — revenue per product per month (for MoM)
--   3. last_two_months — LAG() to get previous month's revenue
--
-- PERCENT_RANK() handles the "top 20%" flag. It ranks ASC so >= 0.80 = top 20%.
-- Using revenue from order_items (qty * unit_price) not orders.total_amount,
-- since total_amount is the whole order, not per-product.
-- =============================================================================

WITH product_totals AS (
    SELECT
        p.id                             AS product_id,
        p.name                           AS product_name,
        p.category,
        SUM(oi.quantity * oi.unit_price)  AS total_revenue,
        SUM(oi.quantity)                  AS total_units_sold
    FROM products p
    JOIN order_items oi ON oi.product_id = p.id
    JOIN orders o       ON o.id = oi.order_id
    WHERE o.status = 'completed'
    GROUP BY p.id, p.name, p.category
),

monthly_revenue AS (
    SELECT
        oi.product_id,
        DATE_TRUNC('month', o.order_date) AS sale_month,
        SUM(oi.quantity * oi.unit_price)   AS month_revenue
    FROM order_items oi
    JOIN orders o ON o.id = oi.order_id
    WHERE o.status = 'completed'
    GROUP BY oi.product_id, DATE_TRUNC('month', o.order_date)
),

last_two_months AS (
    -- grab last 2 completed months so we can compare MoM
    SELECT
        product_id,
        sale_month,
        month_revenue,
        LAG(month_revenue) OVER (
            PARTITION BY product_id
            ORDER BY sale_month
        ) AS prev_month_revenue
    FROM monthly_revenue
    WHERE sale_month >= DATE_TRUNC('month', CURRENT_DATE) - INTERVAL '2 months'
      AND sale_month < DATE_TRUNC('month', CURRENT_DATE)
)

SELECT
    pt.product_name,
    pt.category,
    pt.total_revenue,
    pt.total_units_sold,
    RANK() OVER (
        PARTITION BY pt.category
        ORDER BY pt.total_revenue DESC
    )                                                            AS category_revenue_rank,
    ROUND(
        pt.total_revenue
        / NULLIF(SUM(pt.total_revenue) OVER (PARTITION BY pt.category), 0) * 100,
        2
    )                                                            AS pct_of_category_revenue,
    ltm.month_revenue                                            AS last_month_revenue,
    ltm.prev_month_revenue,
    CASE
        WHEN ltm.prev_month_revenue IS NOT NULL AND ltm.prev_month_revenue > 0
        THEN ROUND(
            (ltm.month_revenue - ltm.prev_month_revenue)
            / ltm.prev_month_revenue * 100,
            2
        )
        ELSE NULL
    END                                                          AS mom_revenue_change_pct,
    CASE
        WHEN PERCENT_RANK() OVER (
            PARTITION BY pt.category
            ORDER BY pt.total_revenue ASC
        ) >= 0.80
        THEN 'Top 20%'
        ELSE NULL
    END                                                          AS top_performer_flag
FROM product_totals pt
LEFT JOIN last_two_months ltm
    ON ltm.product_id = pt.product_id
   AND ltm.sale_month = DATE_TRUNC('month', CURRENT_DATE) - INTERVAL '1 month'
ORDER BY pt.category, RANK() OVER (PARTITION BY pt.category ORDER BY pt.total_revenue DESC);


-- =============================================================================
-- QUERY 3: RFM Customer Segmentation
-- =============================================================================
-- Classic RFM analysis. Two CTEs:
--   1. rfm_raw — get recency (days since last order), frequency, monetary
--   2. rfm_scored — assign 1-5 scores per dimension
--
-- Recency and Frequency use CASE with fixed thresholds (from the requirements).
-- For Monetary I used NTILE(5) — splits customers into 5 equal buckets by spend.
-- This felt cleaner than writing manual percentile logic, and it matches the
-- "top 20% = 5, bottom 20% = 1" requirement naturally.
--
-- Only completed orders count. Customers with zero completed orders won't show up
-- because of the INNER JOIN.
-- =============================================================================

WITH rfm_raw AS (
    SELECT
        c.id                                                     AS customer_id,
        c.name,
        c.email,
        EXTRACT(DAY FROM CURRENT_TIMESTAMP - MAX(o.order_date))  AS recency_days,
        COUNT(o.id)                                              AS frequency,
        SUM(o.total_amount)                                      AS monetary
    FROM customers c
    JOIN orders o ON o.customer_id = c.id
    WHERE o.status = 'completed'
    GROUP BY c.id, c.name, c.email
),

rfm_scored AS (
    SELECT
        customer_id,
        name,
        email,
        recency_days,
        frequency,
        monetary,

        -- recency: 5 = recent (<=30d), 1 = stale (>365d)
        CASE
            WHEN recency_days <= 30  THEN 5
            WHEN recency_days <= 90  THEN 4
            WHEN recency_days <= 180 THEN 3
            WHEN recency_days <= 365 THEN 2
            ELSE 1
        END AS r_score,

        -- frequency: 5 = 20+ orders, 1 = 1-2 orders
        CASE
            WHEN frequency >= 20 THEN 5
            WHEN frequency >= 10 THEN 4
            WHEN frequency >= 5  THEN 3
            WHEN frequency >= 3  THEN 2
            ELSE 1
        END AS f_score,

        -- monetary: NTILE splits into 5 equal buckets
        NTILE(5) OVER (ORDER BY monetary ASC) AS m_score
    FROM rfm_raw
)

SELECT
    name,
    email,
    recency_days,
    frequency,
    ROUND(monetary, 2)                AS monetary,
    r_score,
    f_score,
    m_score,
    (r_score + f_score + m_score)     AS rfm_score,
    CASE
        WHEN (r_score + f_score + m_score) >= 12 THEN 'Champions'
        WHEN (r_score + f_score + m_score) >= 9  THEN 'Loyal'
        WHEN (r_score + f_score + m_score) >= 6  THEN 'At Risk'
        ELSE 'Lost'
    END                               AS segment
FROM rfm_scored
ORDER BY rfm_score DESC, monetary DESC;


-- =============================================================================
-- QUERY 4: Sales Trend with Moving Averages & Anomaly Detection
-- =============================================================================
-- The tricky part here is that days with zero orders still need to show up
-- (not just disappear from the results). So I generate a date series for 90
-- days and LEFT JOIN orders onto it.
--
-- The 7-day moving average uses ROWS BETWEEN 6 PRECEDING AND CURRENT ROW.
-- At the start of the window there won't be 7 days of data yet, so the
-- average is computed from whatever's available — that's fine for our purposes.
--
-- Anomaly threshold is ±30% from the moving average. I picked 1.30 and 0.70
-- as multipliers since that directly maps to "30% above" and "30% below".
-- =============================================================================

WITH date_series AS (
    SELECT d::DATE AS sale_date
    FROM generate_series(
        CURRENT_DATE - INTERVAL '89 days',
        CURRENT_DATE,
        '1 day'::INTERVAL
    ) AS d
),

daily_sales AS (
    SELECT
        d.sale_date,
        COUNT(o.id)                      AS total_orders,
        COALESCE(SUM(o.total_amount), 0) AS total_revenue
    FROM date_series d
    LEFT JOIN orders o
        ON o.order_date::DATE = d.sale_date
       AND o.status = 'completed'
    GROUP BY d.sale_date
),

with_moving_avg AS (
    SELECT
        sale_date,
        total_orders,
        total_revenue,
        ROUND(
            AVG(total_revenue) OVER (
                ORDER BY sale_date
                ROWS BETWEEN 6 PRECEDING AND CURRENT ROW
            ), 2
        )                                 AS revenue_7day_avg,
        ROUND(
            AVG(total_orders) OVER (
                ORDER BY sale_date
                ROWS BETWEEN 6 PRECEDING AND CURRENT ROW
            ), 2
        )                                 AS orders_7day_avg,
        TRIM(TO_CHAR(sale_date, 'Day'))   AS day_of_week
    FROM daily_sales
)

SELECT
    sale_date,
    day_of_week,
    total_orders,
    total_revenue,
    revenue_7day_avg,
    orders_7day_avg,
    CASE
        WHEN revenue_7day_avg > 0
        THEN ROUND(
            (total_revenue - revenue_7day_avg) / revenue_7day_avg * 100,
            2
        )
        ELSE 0
    END                                   AS pct_diff_from_avg,
    CASE
        WHEN revenue_7day_avg > 0
         AND total_revenue > revenue_7day_avg * 1.30
        THEN 'ABOVE +30%'
        WHEN revenue_7day_avg > 0
         AND total_revenue < revenue_7day_avg * 0.70
        THEN 'BELOW -30%'
        ELSE 'Normal'
    END                                   AS anomaly_flag
FROM with_moving_avg
ORDER BY sale_date;


-- =============================================================================
-- QUERY 5: Inventory Turnover & Stock Analysis
-- =============================================================================
-- Steps:
--   1. Get units sold per product in the last 90 days
--   2. Divide by 90 for daily rate, then estimate days until stockout
--   3. Classify into stock status buckets + calculate reorder qty
--
-- LEFT JOIN is important here — products with zero sales in 90 days still
-- need to show up so we can flag them as "Dead Stock".
--
-- Reorder target is 45 days of stock. If current stock already covers that,
-- GREATEST(..., 0) makes sure we don't recommend a negative reorder.
--
-- The ORDER BY uses a CASE to prioritize Critical items first. Dead Stock
-- goes near the bottom since it's not urgent, just worth knowing about.
-- =============================================================================

WITH sales_90d AS (
    SELECT
        oi.product_id,
        SUM(oi.quantity)         AS units_sold_90d,
        MAX(o.order_date)        AS last_order_date
    FROM order_items oi
    JOIN orders o ON o.id = oi.order_id
    WHERE o.status = 'completed'
      AND o.order_date >= CURRENT_DATE - INTERVAL '90 days'
    GROUP BY oi.product_id
),

stock_analysis AS (
    SELECT
        p.id                                             AS product_id,
        p.name                                           AS product_name,
        p.category,
        p.stock_quantity,
        COALESCE(s.units_sold_90d, 0)                    AS units_sold_90d,
        s.last_order_date,

        ROUND(
            COALESCE(s.units_sold_90d, 0)::NUMERIC / 90,
            2
        )                                                AS avg_daily_rate,

        CASE
            WHEN COALESCE(s.units_sold_90d, 0) > 0
            THEN ROUND(
                p.stock_quantity::NUMERIC / (s.units_sold_90d::NUMERIC / 90),
                1
            )
            ELSE NULL  -- no sales = can't estimate
        END                                              AS days_until_stockout
    FROM products p
    LEFT JOIN sales_90d s ON s.product_id = p.id
    WHERE p.stock_quantity > 0
       OR s.product_id IS NOT NULL
)

SELECT
    product_name,
    category,
    stock_quantity,
    units_sold_90d,
    avg_daily_rate,
    days_until_stockout,
    last_order_date,

    CASE
        WHEN units_sold_90d = 0 AND stock_quantity > 0
            THEN 'Dead Stock'
        WHEN days_until_stockout < 7
            THEN 'Critical'
        WHEN days_until_stockout BETWEEN 7 AND 30
            THEN 'Low'
        WHEN days_until_stockout BETWEEN 30 AND 90
            THEN 'Adequate'
        WHEN days_until_stockout > 90
            THEN 'Overstocked'
        ELSE 'No Stock'
    END                                                   AS stock_status,

    -- how much to reorder to maintain 45 days of stock
    CASE
        WHEN avg_daily_rate > 0
        THEN GREATEST(
            CEIL(avg_daily_rate * 45 - stock_quantity),
            0
        )
        ELSE 0
    END                                                   AS reorder_quantity

FROM stock_analysis
ORDER BY
    CASE
        WHEN units_sold_90d = 0 AND stock_quantity > 0 THEN 5  -- Dead Stock
        WHEN days_until_stockout < 7                   THEN 1  -- Critical
        WHEN days_until_stockout BETWEEN 7 AND 30      THEN 2  -- Low
        WHEN days_until_stockout BETWEEN 30 AND 90     THEN 3  -- Adequate
        WHEN days_until_stockout > 90                  THEN 4  -- Overstocked
        ELSE 6
    END,
    days_until_stockout ASC NULLS LAST;


-- =============================================================================
-- QUERY 6: Customer Purchase Pattern Analysis
-- =============================================================================
-- This is the most CTE-heavy query. It answers: for customers who ordered
-- in 2024 (with 3+ orders), what does their purchasing behavior look like?
--
-- The trickiest parts:
--   - LAG() to get days between consecutive orders, then AVG + STDDEV on those gaps
--     (low stddev = consistent buyer)
--   - Finding favorite category: I used DISTINCT ON + GROUP BY + ORDER BY COUNT DESC.
--     Considered MODE() WITHIN GROUP but DISTINCT ON felt more straightforward.
--   - Trend: compare avg order value of first 3 orders vs last 3.
--     ROW_NUMBER from both ends (ASC + DESC) makes this easy.
--     Edge case: if someone has exactly 3 orders, first 3 = last 3, so trend = Stable.
--
-- I'm counting ALL orders for pattern analysis (even cancelled ones),
-- since the pattern of ordering matters regardless of outcome.
-- An index on orders(customer_id, order_date) would help a lot here.
-- =============================================================================

WITH customer_orders_2024 AS (
    SELECT
        c.id          AS customer_id,
        c.name,
        c.email,
        o.id          AS order_id,
        o.order_date,
        o.total_amount,
        COUNT(o.id) OVER (PARTITION BY c.id) AS total_orders
    FROM customers c
    JOIN orders o ON o.customer_id = c.id
    WHERE o.order_date >= '2024-01-01'
      AND o.order_date < '2025-01-01'
),

filtered_customers AS (
    -- only customers with 3+ orders
    SELECT *
    FROM customer_orders_2024
    WHERE total_orders >= 3
),

order_gaps AS (
    SELECT
        customer_id,
        order_date,
        total_amount,
        order_id,
        EXTRACT(DAY FROM
            order_date - LAG(order_date) OVER (
                PARTITION BY customer_id
                ORDER BY order_date
            )
        ) AS days_since_prev_order
    FROM filtered_customers
),

gap_stats AS (
    SELECT
        customer_id,
        ROUND(AVG(days_since_prev_order), 1)    AS avg_days_between_orders,
        ROUND(STDDEV(days_since_prev_order), 1)  AS stddev_days_between_orders
    FROM order_gaps
    WHERE days_since_prev_order IS NOT NULL
    GROUP BY customer_id
),

favorite_category AS (
    SELECT DISTINCT ON (fc.customer_id)
        fc.customer_id,
        p.category                                AS top_category
    FROM filtered_customers fc
    JOIN order_items oi ON oi.order_id = fc.order_id
    JOIN products p     ON p.id = oi.product_id
    GROUP BY fc.customer_id, p.category
    ORDER BY fc.customer_id, COUNT(*) DESC, p.category
),

order_numbered AS (
    -- number from both ends for trend comparison
    SELECT
        customer_id,
        total_amount,
        ROW_NUMBER() OVER (PARTITION BY customer_id ORDER BY order_date ASC)  AS rn_asc,
        ROW_NUMBER() OVER (PARTITION BY customer_id ORDER BY order_date DESC) AS rn_desc,
        total_orders
    FROM filtered_customers
),

trend_calc AS (
    SELECT
        customer_id,
        AVG(CASE WHEN rn_asc  <= 3 THEN total_amount END) AS avg_first_3,
        AVG(CASE WHEN rn_desc <= 3 THEN total_amount END) AS avg_last_3
    FROM order_numbered
    GROUP BY customer_id
),

customer_summary AS (
    SELECT
        fc.customer_id,
        fc.name,
        fc.email,
        fc.total_orders,
        ROUND(AVG(fc.total_amount), 2)                                   AS avg_order_value,
        EXTRACT(DAY FROM MAX(fc.order_date) - MIN(fc.order_date))        AS customer_lifetime_days
    FROM filtered_customers fc
    GROUP BY fc.customer_id, fc.name, fc.email, fc.total_orders
)

SELECT
    cs.name,
    cs.email,
    cs.total_orders,
    gs.avg_days_between_orders,
    gs.stddev_days_between_orders,
    fcat.top_category                                             AS most_purchased_category,
    cs.avg_order_value,
    CASE
        WHEN tc.avg_last_3 > tc.avg_first_3  THEN 'Increasing'
        WHEN tc.avg_last_3 < tc.avg_first_3  THEN 'Decreasing'
        ELSE 'Stable'
    END                                                           AS trend_indicator,
    cs.customer_lifetime_days
FROM customer_summary cs
JOIN gap_stats gs          ON gs.customer_id = cs.customer_id
LEFT JOIN favorite_category fcat ON fcat.customer_id = cs.customer_id
JOIN trend_calc tc         ON tc.customer_id = cs.customer_id
ORDER BY cs.total_orders DESC, cs.avg_order_value DESC;
