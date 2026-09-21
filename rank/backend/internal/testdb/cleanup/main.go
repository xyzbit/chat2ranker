// Command cleanup removes only random Rank snapshot schemas from the explicitly
// configured test database. It does not accept production schema names.
package main

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
)

func main() {
	if len(os.Args) != 2 || !regexp.MustCompile(`^rank_snapshot_[a-f0-9]{16}$`).MatchString(os.Args[1]) || os.Getenv("RANK_TEST_DATABASE_URL") == "" {
		fmt.Fprintln(os.Stderr, "expected a Rank snapshot schema and RANK_TEST_DATABASE_URL")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connection, err := pgx.Connect(ctx, os.Getenv("RANK_TEST_DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "test database connection failed")
		os.Exit(1)
	}
	defer connection.Close(ctx)
	if _, err = connection.Exec(ctx, "DROP SCHEMA IF EXISTS "+pgx.Identifier{os.Args[1]}.Sanitize()+" CASCADE"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
