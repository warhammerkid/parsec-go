package main

import (
	"os"
	"fmt"
	"log"
	"runtime"
	"time"
	"sync"
	"net/http"
	"database/sql"
	"github.com/satori/go.uuid"
	_ "github.com/mattn/go-sqlite3"
)

type UserStore struct {
	sync.RWMutex
	users map[string]*User
}

type User struct {
    token string
    lastActivity time.Time
    raidGroup *RaidGroup
    stats UserStats
}

type UserStats struct {
	raidUserId            int32 `json:"RaidUserId" sync_type:"client-static"`
	raidGroupId           uint32 `json:"RaidGroupId" sync_type:"server-static"`
	lastConnectDate       string `json:"LastConnectDate" sync_type:"server"`
	characterName         string `json:"CharacterName" sync_type:"client-static"`
	damageOut             int32 `json:"DamageOut" sync_type:"client"`
	damageIn              int32 `json:"DamageIn" sync_type:"client"`
	healOut               int32 `json:"HealOut" sync_type:"client"`
	effectiveHealOut      int32 `json:"EffectiveHealOut" sync_type:"client"`
	healIn                int32 `json:"HealIn" sync_type:"client"`
	threat                int32 `json:"Threat" sync_type:"client"`
	raidEncounterId       int32 `json:"RaidEncounterId" sync_type:"client"`
	raidEncounterMode     int32 `json:"RaidEncounterMode" sync_type:"client"`
	raidEncounterPlayers  int32 `json:"RaidEncounterPlayers" sync_type:"client"`
	combatTicks           int64 `json:"CombatTicks" sync_type:"client"`
	combatStart           string `json:"CombatStart" sync_type:"client"`
	combatEnd             string `json:"CombatEnd" sync_type:"client"`
	lastCombatUpdate      string `json:"LastCombatUpdate" sync_type:"server"`
}

type RaidGroupStore struct {
	sync.RWMutex
	raidGroups map[uint32]*RaidGroup
}

type RaidGroup struct {
    sync.RWMutex
    id uint32
    name string
    users []*User
}

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

	// In-memory collections
	allUsers            *UserStore
	allRaidGroups       *RaidGroupStore
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

	// Initialize in-memory stores
	allUsers = &UserStore{users:map[string]*User{}}
	allRaidGroups = &RaidGroupStore{raidGroups:map[uint32]*RaidGroup{}}

	// What port are we running on?
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	httpPort := fmt.Sprintf(":%s", port)

	// Start up web server
	log.Printf("Starting up Parsec Server on port %s", port)
	http.HandleFunc("/api/v2/raid_group", raidGroupHandler)
	http.HandleFunc("/api/v2/connect", connectHandler)
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
		if groupId == 0 {
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

func connectHandler(w http.ResponseWriter, r *http.Request) {
	// Only accept posts
	if r.Method != "POST" {
		http.Error(w, "Unsupported method", 404)
		return
	}

	// Check login
	params := r.URL.Query()
	name := params.Get("name")
	groupId := loginRaid(name, params.Get("password"))
	if groupId == 0 {
		http.Error(w, "Invalid group name or password", 401)
		return
	}

	// Create user
	token := uuid.NewV4().String()
	user := &User{token:token, lastActivity:time.Now()}
	log.Printf("User connected: %s", token)

	// Add user to user store
	allUsers.Lock()
	allUsers.users[token] = user
	allUsers.Unlock()

	// Add them to their raid group
	allRaidGroups.Lock()
	raidGroup := allRaidGroups.raidGroups[groupId]
	if raidGroup != nil {
		// Add the user to the existing raid group
		raidGroup.Lock()
		raidGroup.users = append(raidGroup.users, user)
		raidGroup.Unlock()
	} else {
		// Create a new raid group that contains the user
		users := make([]*User, 0, 16)
		users = append(users, user)
		raidGroup = &RaidGroup{id:groupId, name:name, users:users}
		allRaidGroups.raidGroups[groupId] = raidGroup
	}
	allRaidGroups.Unlock()

	// Set user's raidGroup property so it knows what group it belongs to
	user.raidGroup = raidGroup

	// Write out token
	w.Write([]byte(token))
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