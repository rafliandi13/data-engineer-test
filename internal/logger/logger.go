package logger

import (
	"fmt"
	"log"
	"os"
	"time"
)

// Logger provides structured logging for the application.
type Logger struct {
	infoLog  *log.Logger
	errorLog *log.Logger
}

// New creates a new Logger that writes to stdout and stderr.
func New() *Logger {
	return &Logger{
		infoLog:  log.New(os.Stdout, "[INFO]  ", log.Ldate|log.Ltime),
		errorLog: log.New(os.Stderr, "[ERROR] ", log.Ldate|log.Ltime),
	}
}

// Info logs an informational message.
func (l *Logger) Info(format string, args ...interface{}) {
	l.infoLog.Printf(format, args...)
}

// Error logs an error message.
func (l *Logger) Error(format string, args ...interface{}) {
	l.errorLog.Printf(format, args...)
}

// LogExecution logs report execution details:
// report name, duration, row count, and output path.
func (l *Logger) LogExecution(reportName string, duration time.Duration, rowCount int, outputPath string, err error) {
	if err != nil {
		l.Error("Report '%s' failed after %v: %v", reportName, duration, err)
		return
	}
	l.Info("Report '%s' completed: %d rows, duration %v, output: %s",
		reportName, rowCount, duration, outputPath)
}

// Timestamp generates a timestamp string for filenames.
func Timestamp() string {
	return fmt.Sprintf(time.Now().Format("2006-01-02_150405"))
}
