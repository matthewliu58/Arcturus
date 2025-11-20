package middleware

import (
	"database/sql"
	"fmt"
	log "github.com/sirupsen/logrus"
	"path/filepath"
	"scheduling/structs"
	"time"

	"github.com/BurntSushi/toml"
	_ "github.com/go-sql-driver/mysql"
)

// db
var db *sql.DB

// ConnectToDB
func ConnectToDB(dbConfig structs.DatabaseConfig) *sql.DB {

	if db != nil {
		return db
	}

	// DSN: [username[:password]@][protocol[(address)]]/dbname[?param1=value1&...¶mN=valueN]
	dsn := fmt.Sprintf("%s:%s@tcp(127.0.0.1:3306)/%s?charset=utf8&parseTime=True&loc=Local",
		dbConfig.Username,
		dbConfig.Password,
		dbConfig.DBName,
	)
	var err error

	db, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Errorf("Error ConnectToDB:", err)
		return nil
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.Ping(); err != nil {
		log.Errorf("Error pinging the database: %v", err)
		return nil
	}

	log.Infof("Database connection goroutine_pool initialized successfully.")
	return db
}

func CloseDB() {
	if db != nil {
		err := db.Close()
		if err != nil {
			log.Errorf("Error closing the database connection goroutine_pool:", err)
		} else {
			log.Infof("Database connection goroutine_pool closed.")
		}
	}
}

// LoadConfig reads the TOML configuration file
func LoadConfig(path string) (*structs.Config, error) {
	var cfg structs.Config
	// Get absolute path for clearer error messages if file not found
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("error getting absolute path for %s: %w", path, err)
	}

	log.Infof("Attempting to load configuration from: %s", absPath)

	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("error decoding TOML file %s: %w", path, err)
	}
	return &cfg, nil
}
