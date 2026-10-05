// Command migrate — отдельный шаг применения миграций схемы БД.
//
// Миграции применяет не сервер, а этот бинарь: в docker-compose он запускается
// одноразовым сервисом migrate, локально — `make migrate`. Сервер на старте
// только проверяет готовность схемы, поэтому падение миграций не превращается в
// полуработающее приложение.
//
// Использование:
//
//	migrate [up | down | version | force <N>]
//
// Путь к БД — флаг -db, иначе SQLITE_PATH (в том числе из backend/.env), иначе
// data/marketing.db: та же логика, что у сервера (config.SQLitePath).
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"strconv"

	"github.com/joho/godotenv"

	"github.com/977ADAM/marketing-agents/internal/config"
	"github.com/977ADAM/marketing-agents/internal/migrate"
	"github.com/977ADAM/marketing-agents/internal/store"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("migrate: ")
	// .env опционален: в compose переменные приходят из окружения сервиса.
	_ = godotenv.Load()

	dbFlag := flag.String("db", "", "путь к файлу SQLite (по умолчанию SQLITE_PATH или data/marketing.db)")
	flag.Usage = usage
	flag.Parse()

	path, err := dbPath(*dbFlag)
	if err != nil {
		log.Fatal(err)
	}

	command := "up"
	if args := flag.Args(); len(args) > 0 {
		command = args[0]
	}

	ctx := context.Background()
	db, err := store.OpenDB(ctx, path)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	if err := run(ctx, db, command); err != nil {
		log.Fatal(err)
	}

	version, dirty, err := migrate.Version(ctx, db)
	if err != nil {
		log.Fatal(err)
	}
	latest, err := migrate.Latest()
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("схема %s: версия %d из %d (dirty=%v)", path, version, latest, dirty)
}

// run выполняет команду; перенос учёта из таблицы прежнего раннера делается и
// здесь (для понятного лога), и внутри Up — вызов идемпотентен.
func run(ctx context.Context, db *sql.DB, command string) error {
	switch command {
	case "up":
		if version, adopted, err := migrate.AdoptLegacy(ctx, db); err != nil {
			return err
		} else if adopted {
			log.Printf("прежняя таблица учёта перенесена в формат golang-migrate: версия %d, миграции повторно не выполнялись", version)
		}
		return migrate.Up(ctx, db)
	case "down":
		return migrate.Down(ctx, db)
	case "force":
		args := flag.Args()
		if len(args) < 2 {
			return fmt.Errorf("force требует версию: migrate force <N>")
		}
		version, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("версия %q не число", args[1])
		}
		return migrate.Force(ctx, db, version)
	case "version":
		return nil
	default:
		return fmt.Errorf("неизвестная команда %q (up | down | version | force <N>)", command)
	}
}

func dbPath(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	return config.SQLitePath()
}

func usage() {
	fmt.Fprintf(flag.CommandLine.Output(), `migrate — миграции схемы marketing-agents (golang-migrate).

Использование:
  migrate [флаги] [up | down | version | force <N>]

Команды:
  up            применить все неприменённые миграции (по умолчанию)
  down          откатить последнюю миграцию
  version       показать текущую версию схемы
  force <N>     пометить версию применённой, ничего не выполняя
                (выводит БД из «грязного» состояния)

Флаги:
`)
	flag.PrintDefaults()
}
