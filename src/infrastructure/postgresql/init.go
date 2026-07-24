package postgresql

import (
	"os"
	"sync"

	e "github.com/ChatDetectiveORG/shared/errors"

	"github.com/go-pg/pg/v10"
	"github.com/go-pg/pg/v10/orm"

	models "github.com/ChatDetectiveORG/shared/postgresModels"
)

var (
	dbMu sync.RWMutex
	db   *pg.DB
)

func GetDB() *pg.DB {
	dbMu.RLock()
	if db != nil {
		defer dbMu.RUnlock()
		return db
	}
	dbMu.RUnlock()

	dbMu.Lock()
	defer dbMu.Unlock()
	if db == nil {
		db = pg.Connect(&pg.Options{
			Addr:     os.Getenv("DB_HOST") + ":" + os.Getenv("DB_PORT"),
			User:     os.Getenv("POSTGRES_USER"),
			Password: os.Getenv("POSTGRES_PASSWORD"),
			Database: os.Getenv("POSTGRES_DB"),
			PoolSize: 20,
		})
	}
	return db
}

// SetDBForTest injects a database handle for chain tests. Call ResetDBForTest in t.Cleanup.
func SetDBForTest(testDB *pg.DB) {
	dbMu.Lock()
	defer dbMu.Unlock()
	db = testDB
}

// ResetDBForTest clears the injected handle so the next GetDB reconnects from env.
func ResetDBForTest() {
	dbMu.Lock()
	defer dbMu.Unlock()
	db = nil
}

func InitPostgresql() *e.ErrorInfo {
	db := GetDB()

	models := []interface{}{
		(*models.Message)(nil),
		(*models.Telegramuser)(nil),
		(*models.UserSettings)(nil),
		(*models.Admin)(nil),
		(*models.MessageVersion)(nil),
		(*models.UserLevels)(nil),
	}

	for _, model := range models {
		err := db.Model(model).CreateTable(&orm.CreateTableOptions{
			IfNotExists: true,
			Temp:        false,
		})
		if err != nil {
			return e.FromError(err, "error creating table")
		}
	}

	return nil
}
