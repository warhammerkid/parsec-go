package main

import (
	"os"
	"fmt"
	"log"
	"runtime"
	"time"
	"net/http"
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
)

const (
	// Database
	tableCreate = "CREATE TABLE IF NOT EXISTS raid_groups (id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL UNIQUE, name TEXT NOT NULL UNIQUE, password TEXT, admin_password TEXT, datetime TEXT);"
	createRaidGroup = "INSERT INTO raid_groups VALUES (NULL, ?, ?, ?, ?)"
	deleteRaidGroup = "DELETE FROM raid_groups WHERE name=? AND admin_password=?"
	selectRaidGroup = "SELECT id, password FROM raid_groups WHERE name=?"
)

var (
	// Database
	createRaidGroupStmt *sql.Stmt
	deleteRaidGroupStmt *sql.Stmt
	selectRaidGroupStmt *sql.Stmt
)

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	// Open database
	db, err := sql.Open("sqlite3", "./raid_groups.db")
	if err != nil {
		log.Fatalf("Error opening database: %v", err)
	}
	defer db.Close()

	// Prepare SQL queries
	_, err = db.Exec(tableCreate)
	if err != nil {
		log.Fatal(err)
	}
	createRaidGroupStmt, err = db.Prepare(createRaidGroup)
	if err != nil {
		log.Fatal(err)
	}
	deleteRaidGroupStmt, err = db.Prepare(deleteRaidGroup)
	if err != nil {
		log.Fatal(err)
	}
	selectRaidGroupStmt, err = db.Prepare(selectRaidGroup)
	if err != nil {
		log.Fatal(err)
	}

	// What port are we running on?
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	httpPort := fmt.Sprintf(":%s", port)

	// Start up web server
	log.Printf("Starting up Parsec Server on port %s", port)
	http.HandleFunc("/api/v2/raid_group", raidGroupHandler)
	http.ListenAndServe(httpPort, nil)
}

func raidGroupHandler(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	name := params.Get("name")
	password := params.Get("password")
	adminPassword := params.Get("adminPassword")

	if r.Method == "GET" {
		// Check if the credentials are valid
		groupId := loginRaid(name, password)
		if groupId <= 0 {
			http.Error(w, "Invalid group name or password", 401)
		}
	} else if r.Method == "POST" {
		// Validate params
		if name == "" || password == "" || adminPassword == "" {
			http.Error(w, "All three arguments required to create a raid group", 400)
			return
		}

		// Attempt to create it
		result, _ := createRaidGroupStmt.Exec(name, password, adminPassword, time.Now().Format(time.RFC3339))
		if result != nil {
			log.Printf("Created raid group: '%s'", name)
			w.Write([]byte("Raid group created successfully"))
		} else {
			http.Error(w, "A group with the given name already exists", 400)
		}
	} else if r.Method == "DELETE" {
		qres, _ := deleteRaidGroupStmt.Exec(name, adminPassword)
		if qres == nil {
			http.Error(w, "Delete failed", 500)
			return
		}

		affected, _ := qres.RowsAffected()
		if affected == 1 {
			log.Printf("Deleted raid group: '%s'", name)
			w.Write([]byte("Raid group deleted successfully"))
		} else {
			http.Error(w, "Invalid group name or admin password", 400)
		}
	} else {
		http.Error(w, "Unsupported method", 404)
	}
}

func loginRaid(group string, password string) uint32 {
	var id uint32
	var groupPassword string
	selectRaidGroupStmt.QueryRow(group).Scan(&id, &groupPassword)
	if password == groupPassword {
		return id
	} else {
		return 0
	}
}