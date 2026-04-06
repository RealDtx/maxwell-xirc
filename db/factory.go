package db

import "fmt"

// NewStore creates a Store backed by the given driver.
// For "sqlite", dsn is the file path.
// For "mysql", dsn is a go-sql-driver/mysql DSN string.
func NewStore(driver, dsn string) (Store, error) {
	switch driver {
	case "sqlite":
		return NewSQLiteStore(dsn)
	case "mysql":
		return NewMySQLStore(dsn)
	default:
		return nil, fmt.Errorf("unsupported database driver: %s (expected 'sqlite' or 'mysql')", driver)
	}
}
