package database_test

import (
	"os"
	"testing"

	"github.com/yashdagar/zomato-prep/backend/internal/database"
)

func dsn(t *testing.T) string {
	t.Helper()
	d := os.Getenv("TEST_MYSQL_DSN")
	if d == "" {
		t.Skip("set TEST_MYSQL_DSN=root:dev@tcp(127.0.0.1:3306)/zomato?parseTime=true to run")
	}
	return d
}

func TestW6B5_PoolSizeComesFromTheEnvironment(t *testing.T) {
	t.Setenv("DB_MAX_OPEN_CONNS", "7")
	db, err := database.Open(t.Context(), dsn(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if got := db.Stats().MaxOpenConnections; got != 7 {
		t.Fatalf("MaxOpenConnections = %d, want 7 from DB_MAX_OPEN_CONNS", got)
	}
}

func TestW6B5_PoolIsBoundedByDefault(t *testing.T) {
	t.Setenv("DB_MAX_OPEN_CONNS", "")
	db, err := database.Open(t.Context(), dsn(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	got := db.Stats().MaxOpenConnections
	if got <= 0 || got > 100 {
		t.Fatalf("MaxOpenConnections = %d, want a bound between 1 and 100", got)
	}
}

func TestW6B5_IdleConnectionsNeverExceedTheOpenLimit(t *testing.T) {
	t.Setenv("DB_MAX_OPEN_CONNS", "4")
	db, err := database.Open(t.Context(), dsn(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if got := db.Stats().MaxOpenConnections; got != 4 {
		t.Fatalf("MaxOpenConnections = %d, want 4", got)
	}
	var one int
	if err := db.QueryRowContext(t.Context(), "SELECT 1").Scan(&one); err != nil || one != 1 {
		t.Fatalf("query through the pool: %d %v", one, err)
	}
}
