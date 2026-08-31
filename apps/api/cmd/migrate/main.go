package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"aigc-3d-platform/apps/api/migrations"
	_ "github.com/go-sql-driver/mysql"
	"github.com/pressly/goose/v3"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: migrate <up|down|status|version>")
		os.Exit(2)
	}

	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		dsn = "root:root@tcp(localhost:3306)/aigc_platform?charset=utf8mb4&parseTime=True&loc=UTC"
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fatal(err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		fatal(err)
	}

	goose.SetBaseFS(migrations.Files)
	if err := goose.SetDialect("mysql"); err != nil {
		fatal(err)
	}
	if err := goose.RunContext(context.Background(), os.Args[1], db, "."); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
