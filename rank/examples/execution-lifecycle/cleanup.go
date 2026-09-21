// This example helper removes only the verifier's randomly named PostgreSQL schema.
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
	if len(os.Args) != 2 || !regexp.MustCompile(`^(snapshot|remote)_[0-9a-f]{16}$`).MatchString(os.Args[1]) {
		fmt.Fprintln(os.Stderr, "expected a verifier-owned snapshot schema")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connection, err := pgx.Connect(ctx, os.Getenv("EXECUTION_DATABASE_URL"))
	if err == nil {
		defer connection.Close(ctx)
		_, err = connection.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{os.Args[1]}.Sanitize()+" CASCADE")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "could not remove the verifier's PostgreSQL schema")
		os.Exit(1)
	}
}
