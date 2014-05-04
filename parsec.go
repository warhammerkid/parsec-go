package main

import (
	"log"
	"runtime"
	"time"
	"encoding/json"
	"net/http"
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
)

type ActionResponse struct {
	Success bool `json:"Success"`
	Message string `json:"Message"`
}

const (
	// Database
	tableCreate = "CREATE TABLE IF NOT EXISTS raid_groups (id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL UNIQUE, name TEXT NOT NULL UNIQUE, password TEXT, admin_password TEXT, datetime TEXT);"
	createRaidGroup = "INSERT INTO raid_groups VALUES (NULL, ?, ?, ?, ?)"
	deleteRaidGroup = "DELETE FROM raid_groups WHERE name=? AND admin_password=?"
)

var (
	// Database
	createRaidGroupStmt *sql.Stmt
	deleteRaidGroupStmt *sql.Stmt
)

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	db, err := sql.Open("sqlite3", "./raid_groups.db")
	if err != nil {
		log.Fatalf("Error opening database: %v", err)
	}
	defer db.Close()

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

	http.HandleFunc("/RequestRaidGroup", requestRaidGroupHandler)
	http.HandleFunc("/DeleteRaidGroup", deleteRaidGroupHandler)
	http.ListenAndServe(":8080", nil)
}

func requestRaidGroupHandler(w http.ResponseWriter, r *http.Request) {
	// Set up http response and defer writing output
	res := ActionResponse{false, "An unknown error was encountered"}
	w.Header().Set("Content-Type", "application/json")
	defer json.NewEncoder(w).Encode(&res)

	// Parse and validate request
	type CreateRequest struct {
		RequestedName string `json:"requestedName"`
		RequestedPassword string `json:"requestedPassword"`
		AdminPassword string `json:"adminPassword"`
	}
	var req CreateRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		res.Message = "Invalid JSON"
		return
	}
	if req.RequestedName == "" || req.RequestedPassword == "" || req.AdminPassword == "" {
		res.Message = "Empty paramaters - all fields required"
		return
	}

	// Insert into the database
	qres, _ := createRaidGroupStmt.Exec(req.RequestedName, req.RequestedPassword, req.AdminPassword, time.Now().Format(time.RFC3339))
	if qres != nil {
		affected, _ := qres.RowsAffected()
		if affected == 1 {
			log.Printf("Created raid group: '%s'", req.RequestedName)
			res.Success = true
			res.Message = "Raid group created successfully"
		}
	} else {
		res.Message = "A group with the given name already exists"
	}
}

func deleteRaidGroupHandler(w http.ResponseWriter, r *http.Request) {
	// Set up http response and defer writing output
	res := ActionResponse{false, "An unknown error was encountered"}
	w.Header().Set("Content-Type", "application/json")
	defer json.NewEncoder(w).Encode(&res)

	// Parse request
	type DeleteRequest struct {
		GroupName string `json:"groupName"`
		AdminPassword string `json:"adminPassword"`
	}
	var req DeleteRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		res.Message = "Invalid JSON"
		return
	}

	// Perform delete
	qres, _ := deleteRaidGroupStmt.Exec(req.GroupName, req.AdminPassword)
	if qres != nil {
		affected, _ := qres.RowsAffected()
		if affected == 1 {
			log.Printf("Deleted raid group: '%s'", req.GroupName)
			res.Success = true
			res.Message = "Raid group deleted successfully"
		}
	}
}