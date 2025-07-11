package evmi_database

import (
	"errors"

	"github.com/rs/zerolog"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type DatabaseType string

const (
	SqliteDatabaseType   DatabaseType = "SQLITE"
	PostgresDatabaseType DatabaseType = "POSTGRES"
	MysqlDatabaseType    DatabaseType = "MYSQL"
)

type EvmiDatabase struct {
	Conn *gorm.DB
}

func LoadDatabase(dbType DatabaseType, config map[string]string, logger zerolog.Logger) (*EvmiDatabase, error) {

	var db *gorm.DB
	var err error

	if dbType == SqliteDatabaseType {
		logger.Info().Msg("Initialize SQLite database")
		filename, ok := config["filename"]
		if !ok {
			return nil, errors.New("filename of sqlite database not provided in config")
		}

		db, err = gorm.Open(sqlite.Open(filename), &gorm.Config{})
		if err != nil {
			return nil, err
		}
	}

	if dbType == PostgresDatabaseType {
		logger.Log().Msg("Initialize Postgres database")
		dsn, ok := config["dsn"]
		if !ok {
			return nil, errors.New("dsn of sqlite database not provided in config")
		}

		db, err = gorm.Open(postgres.New(postgres.Config{
			DSN: dsn,
		}), &gorm.Config{})
		if err != nil {
			return nil, err
		}
	}

	if dbType == MysqlDatabaseType {
		logger.Log().Msg("Initialize MySQL database")
		dsn, ok := config["dsn"]
		if !ok {
			return nil, errors.New("dsn of sqlite database not provided in config")
		}

		db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
		if err != nil {
			return nil, err
		}
	}

	if db == nil {
		return nil, errors.New("unknown database type")
	}

	// Migrate the schema
	err = db.AutoMigrate(&EvmiInstance{})
	if err != nil {
		return nil, err
	}

	err = db.AutoMigrate(&EvmBlockchain{})
	if err != nil {
		return nil, err
	}

	err = db.AutoMigrate(&EvmJsonAbi{})
	if err != nil {
		return nil, err
	}

	err = db.AutoMigrate(&EvmLogStore{})
	if err != nil {
		return nil, err
	}

	err = db.AutoMigrate(&EvmLogPipeline{})
	if err != nil {
		return nil, err
	}

	err = db.AutoMigrate(&EvmLogSource{})
	if err != nil {
		return nil, err
	}

	return &EvmiDatabase{Conn: db}, nil
}
