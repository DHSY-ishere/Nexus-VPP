package db

import (
	"fmt"
	"log"

	"github.com/DHSY-ishere/Nexus-VPP/backend/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var DB *gorm.DB

func InitDB() {
	dsn := "host=127.0.0.1 user=nexus_admin password=supersecretpassword dbname=nexus_ledger port=5433 sslmode=disable TimeZone=UTC"
	var err error
	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("Failed to connect to PostgreSQL:", err)
	}

	// Auto-migrate the schema.
	DB.AutoMigrate(&models.P2PTrade{}, &models.MarketTrade{}, &models.IntervalRoot{})
	fmt.Println("PostgreSQL Ledger initialized.")
}
