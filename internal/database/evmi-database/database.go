package evmi_database

import (
	"errors"
	"os"

	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
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

	err = db.AutoMigrate(&EvmiExporter{})
	if err != nil {
		return nil, err
	}

	err = db.AutoMigrate(&Plugin{})
	if err != nil {
		return nil, err
	}

	err = db.AutoMigrate(&User{}, &AccessToken{}, &OAuthProvider{})
	if err != nil {
		return nil, err
	}

	if err := seedDefaultAdmin(db, logger); err != nil {
		return nil, err
	}

	return &EvmiDatabase{Conn: db}, nil
}

// seedDefaultAdmin creates an initial admin user when no users exist yet. The
// password comes from EVMI_ADMIN_PASSWORD when set; the admin/admin fallback
// is meant for local setups only.
func seedDefaultAdmin(db *gorm.DB, logger zerolog.Logger) error {
	var count int64
	if err := db.Model(&User{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	password := os.Getenv("EVMI_ADMIN_PASSWORD")
	fromEnv := password != ""
	if !fromEnv {
		password = "admin"
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	admin := User{
		Username:     "admin",
		PasswordHash: string(hash),
		Role:         string(RoleAdmin),
	}
	if err := db.Create(&admin).Error; err != nil {
		return err
	}

	if fromEnv {
		logger.Info().Msg("created default admin user with the password from EVMI_ADMIN_PASSWORD")
	} else {
		logger.Warn().Msg("created default admin user (admin/admin) — set EVMI_ADMIN_PASSWORD or change the password immediately")
	}
	return nil
}
