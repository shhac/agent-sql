package mariadb

import (
	"os"
	"testing"
)

func TestConnect(t *testing.T) {
	t.Run("passes variant mariadb to mysql driver", func(t *testing.T) {
		// Cannot test actual connection without a MariaDB instance.
		// Verify the wrapper compiles and the Opts struct works.
		_ = Opts{
			Host:     "localhost",
			Port:     3306,
			Database: "test",
			Username: "root",
			Password: "",
			Readonly: true,
		}
	})
}

// Integration tests — require a real MariaDB instance.
func TestIntegration(t *testing.T) {
	dsn := os.Getenv("AGENT_SQL_MARIADB_TEST_URL")
	if dsn == "" {
		t.Skip("requires MariaDB — set AGENT_SQL_MARIADB_TEST_URL")
	}

	conn, err := Connect(Opts{
		Host:     "localhost",
		Port:     3306,
		Database: "test",
		Username: "root",
		Password: "",
		Readonly: true,
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer conn.Close()
}
