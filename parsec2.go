package main

import (
	"os"
	"fmt"
	"log"
	"runtime"
	"time"
	"sync"
	"compress/gzip"
	"encoding/json"
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
	RaidUserId            int32
	RaidGroupId           uint32 // Server provided
	LastConnectDate       string // Server provided
	CharacterName         string
	DamageOut             int32
	DamageIn              int32
	HealOut               int32
	EffectiveHealOut      int32
	HealIn                int32
	Threat                int32
	RaidEncounterId       int32
	RaidEncounterMode     int32
	RaidEncounterPlayers  int32
	CombatTicks           int64
	CombatStart           string
	CombatEnd             string
	LastCombatUpdate      string // Server provided
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

	// GC Configs
	gcCheckFrequency = 1*time.Minute
	inactiveTimeoutDuration = 5*time.Minute
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

	// Start up GC for inactive users and groups
	go garbageCollectInactive()

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
	http.HandleFunc("/api/v2/stats", statsHandler)
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

func statsHandler(w http.ResponseWriter, r *http.Request) {
	// Look up user by token
	token := r.URL.Query().Get("t")
	allUsers.RLock()
	user := allUsers.users[token]
	allUsers.RUnlock()
	if user == nil {
		http.Error(w, "Invalid connection token", 400)
		return
	}

	// Update activity timestamp
	user.lastActivity = time.Now()

	// Update user stats if POST
	if r.Method == "POST" {
		// Parse JSON
		var userStats UserStats
		err := json.NewDecoder(r.Body).Decode(&userStats)
		if err != nil {
			http.Error(w, "Invalid JSON", 400)
			return
		}

		// Update user
		user.stats = userStats
	}

	// Build response
	raidGroupStats := calculateRaidStats(user.raidGroup)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Encoding", "gzip")
	gz := gzip.NewWriter(w)
	json.NewEncoder(gz).Encode(&raidGroupStats)
	gz.Close()
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

func calculateRaidStats(raidGroup *RaidGroup) []UserStats {
	// Pull out all active user stats
	raidGroup.RLock()
	userCount := len(raidGroup.users)
	userStats := make([]UserStats, 0, userCount)
	for i := 0; i < userCount; i++ {
		if raidGroup.users[i] != nil {
			userStats = append(userStats, raidGroup.users[i].stats)
		}
	}
	raidGroup.RUnlock()

	// Post-process...

	return userStats
}

func garbageCollectInactive() {
	tick := time.Tick(gcCheckFrequency)
	for {
		<-tick

		now := time.Now()
		inactiveUsers := make([]*User, 0, 32)

		// Build list of inactive users
		allUsers.RLock()
		for k := range allUsers.users {
			user := allUsers.users[k]
			if now.Sub(user.lastActivity) > inactiveTimeoutDuration {
				inactiveUsers = append(inactiveUsers, user)
			}
		}
		allUsers.RUnlock()

		// Continue if no inactive users
		if len(inactiveUsers) == 0 {
			continue;
		}
		log.Printf("Deleting %d inactive users", len(inactiveUsers))

		allUsers.Lock()
		for i := 0; i < len(inactiveUsers); i++ {
			user := inactiveUsers[i]

			// Remove from raid group
			user.raidGroup.Lock()
			groupUsers := user.raidGroup.users
			userCount := len(groupUsers)
			for i := 0; i < userCount; i++ {
				if groupUsers[i] == user {
					groupUsers[i] = nil
					break
				}
			}
			user.raidGroup.Unlock()
			user.raidGroup = nil

			// Remove from users store
			delete(allUsers.users, user.token)
		}
		allUsers.Unlock()

		log.Printf("GC run completed in %d ms", int64(time.Since(now) / time.Millisecond))
	}
}