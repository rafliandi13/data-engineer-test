package database

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"

	"github.com/rafliandi13/data-engineer-test/internal/config"
	"github.com/rafliandi13/data-engineer-test/internal/logger"
)

// DB wraps sql.DB and provides methods for query execution.
type DB struct {
	conn   *sql.DB
	logger *logger.Logger
}

// New creates a new database connection with connection pooling.
func New(cfg config.DatabaseConfig, log *logger.Logger) (*DB, error) {
	conn, err := sql.Open("postgres", cfg.ConnectionString())
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}

	// Connection pool settings
	conn.SetMaxOpenConns(10)
	conn.SetMaxIdleConns(5)
	conn.SetConnMaxLifetime(5 * time.Minute)

	// Verify connection
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Info("Database connection established: %s:%d/%s", cfg.Host, cfg.Port, cfg.DBName)

	return &DB{conn: conn, logger: log}, nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	db.logger.Info("Closing database connection")
	return db.conn.Close()
}

// QueryResult stores query results as a slice of maps.
// Each map represents a single row with column names as keys.
type QueryResult struct {
	Columns []string
	Rows    []map[string]interface{}
}

// ExecuteQuery runs a SQL query and returns its results.
// It logs the execution time and the number of rows returned.
func (db *DB) ExecuteQuery(query string) (*QueryResult, error) {
	start := time.Now()

	rows, err := db.conn.Query(query)
	if err != nil {
		db.logger.Error("Query failed after %v: %v", time.Since(start), err)
		return nil, fmt.Errorf("query execution failed: %w", err)
	}
	defer rows.Close()

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to read columns: %w", err)
	}

	result := &QueryResult{
		Columns: columns,
		Rows:    make([]map[string]interface{}, 0),
	}

	for rows.Next() {
		// Create a slice of interface{} for scanning
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Convert to map
		row := make(map[string]interface{})
		for i, col := range columns {
			val := values[i]
			// Handle byte slices (PostgreSQL text types)
			if b, ok := val.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = val
			}
		}
		result.Rows = append(result.Rows, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	elapsed := time.Since(start)
	db.logger.Info("Query completed: %d rows in %v", len(result.Rows), elapsed)

	return result, nil
}
