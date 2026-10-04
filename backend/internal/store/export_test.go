package store

import "database/sql"

// DB открывает тестам пакета store_test соединение с БД. Файл с суффиксом
// _test.go компилируется только в тест-бинарь пакета, поэтому в прод-сборку
// этот доступ не попадает.
func (s *Store) DB() *sql.DB { return s.db }
