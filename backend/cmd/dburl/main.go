// dburl prints only a credential-safe URI for consumption by dbmate or test env.
package main

import (
	"flag"
	"fmt"
	"github.com/977ADAM/marketing-agents/internal/core/config"
	"github.com/joho/godotenv"
	"os"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func main() {
	test := flag.Bool("test", false, "build local test admin URI")
	flag.Parse()
	_ = godotenv.Load()
	if !*test && os.Getenv("DATABASE_URL") != "" {
		fmt.Print(os.Getenv("DATABASE_URL"))
		return
	}
	user, pass, host, port, db := os.Getenv("MARIADB_USER"), os.Getenv("MARIADB_PASSWORD"), env("MARIADB_HOST", "127.0.0.1"), env("MARIADB_PORT", "3306"), env("MARIADB_DATABASE", "marketing")
	if *test {
		user = "root"
		pass = os.Getenv("MARIADB_ROOT_PASSWORD")
		host = "127.0.0.1"
		db = ""
	}
	uri, err := config.BuildDatabaseURL(user, pass, host, port, db)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Print(uri)
}
