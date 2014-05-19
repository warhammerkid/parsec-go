package main

import (
	"os"
	"fmt"
	"log"
	"runtime"
	"time"
	"sync"
	"encoding/json"
	"net/http"
	"database/sql"
	"github.com/youtube/vitess/go/cgzip"
	_ "github.com/mattn/go-sqlite3"
)

type RaidUser struct {
	RaidUserId            int32 `json:"RaidUserId" sync_type:"client-static"`
	RaidGroupId           uint32 `json:"RaidGroupId" sync_type:"server-static"`
	LastConnectDate       string `json:"LastConnectDate" sync_type:"server"`
	IsConnected           bool `json:"IsConnected" sync_type:"server"`
	CharacterName         string `json:"CharacterName" sync_type:"client-static"`
	DamageOut             int32 `json:"DamageOut" sync_type:"client"`
	DamageIn              int32 `json:"DamageIn" sync_type:"client"`
	HealOut               int32 `json:"HealOut" sync_type:"client"`
	EffectiveHealOut      int32 `json:"EffectiveHealOut" sync_type:"client"`
	HealIn                int32 `json:"HealIn" sync_type:"client"`
	Threat                int32 `json:"Threat" sync_type:"client"`
	RaidEncounterId       int32 `json:"RaidEncounterId" sync_type:"client"`
	RaidEncounterMode     int32 `json:"RaidEncounterMode" sync_type:"client"`
	RaidEncounterPlayers  int32 `json:"RaidEncounterPlayers" sync_type:"client"`
	CombatTicks           int64 `json:"CombatTicks" sync_type:"client"`
	CombatStart           string `json:"CombatStart" sync_type:"client"`
	CombatEnd             string `json:"CombatEnd" sync_type:"client"`
	LastCombatUpdate      string `json:"LastCombatUpdate" sync_type:"server"`
}

type RaidStats struct {
	GroupId               uint32
	GroupName             string
	Users                 []*RaidUser
	LastActivity          time.Time
}

type RaidStatsCache struct {
	sync.RWMutex
	Raids                 map[uint32]*RaidStats
}

type ActionResponse struct {
	Success               bool
	Message               string
}

type CreateRequest struct {
	RequestedName         string `json:"requestedName"`
	RequestedPassword     string `json:"requestedPassword"`
	AdminPassword         string `json:"adminPassword"`
}

type DeleteRequest struct {
	GroupName             string `json:"groupName"`
	AdminPassword         string `json:"adminPassword"`
}

type SyncOrGetRequest struct {
	RaidGroup             string
	RaidPassword          string
	Statistics            RaidUser
}

type SyncOrGetResponse struct {
	ErrorMessage          string
	Users                 []*RaidUser
	MinimumPollingRate    uint32
}

const (
	// Database
	tableCreate = "CREATE TABLE IF NOT EXISTS raid_groups (id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL UNIQUE, name TEXT NOT NULL UNIQUE, password TEXT, admin_password TEXT, datetime TEXT);"
	createRaidGroup = "INSERT INTO raid_groups VALUES (NULL, ?, ?, ?, ?)"
	deleteRaidGroup = "DELETE FROM raid_groups WHERE name=? AND admin_password=?"
	loginSelect     = "SELECT id, password FROM raid_groups WHERE name=?"

	// Paths
	requestRaidGroupPath = "/api/RequestRaidGroup"
	deleteRaidGroupPath = "/api/DeleteRaidGroup"
	testConnectionPath = "/api/TestConnection"
	syncRaidStatsPath = "/api/SyncRaidStats"
	getRaidStatsPath = "/api/GetRaidStats"
)

var (
	// Database
	createRaidGroupStmt *sql.Stmt
	deleteRaidGroupStmt *sql.Stmt
	loginStmt           *sql.Stmt

	// Stats
	allRaidStats        *RaidStatsCache
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
	loginStmt, err = db.Prepare(loginSelect)
	if err != nil {
		log.Fatal(err)
	}

	// Initialize in-memory stores
	allRaidStats = &RaidStatsCache{Raids:map[uint32]*RaidStats{}}

	// Start up raid GC
	go garbageCollectRaidStats()

	// What port are we running on?
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	httpPort := fmt.Sprintf(":%s", port)

	// Start up web server
	log.Printf("Starting up Parsec Server on port %s", port)
	http.HandleFunc("/", homepageHandler)
	http.HandleFunc(requestRaidGroupPath, requestRaidGroupHandler)
	http.HandleFunc(deleteRaidGroupPath, deleteRaidGroupHandler)
	http.HandleFunc(testConnectionPath, testConnectionHandler)
	http.HandleFunc(syncRaidStatsPath, syncOrGetStatsHandler)
	http.HandleFunc(getRaidStatsPath, syncOrGetStatsHandler)
	http.ListenAndServe(httpPort, nil)
}

func homepageHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

func requestRaidGroupHandler(w http.ResponseWriter, r *http.Request) {
	// Set up http response and defer writing output
	res := ActionResponse{false, "An unknown error was encountered"}
	defer sendSerializedJSON(w, &res)

	// Parse and validate request
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
	defer sendSerializedJSON(w, &res)

	// Parse request
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

func testConnectionHandler(w http.ResponseWriter, r *http.Request) {
	// Set up http response and defer writing output
	res := SyncOrGetResponse{ErrorMessage:"Connection failed"}
	defer sendSerializedJSON(w, &res)

	// Parse request
	var req SyncOrGetRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		res.ErrorMessage = "Invalid JSON"
		return
	}

	// Attempt to login
	groupId := loginRaid(req.RaidGroup, req.RaidPassword)
	if groupId > 0 {
		res.ErrorMessage = ""
	}
}

func syncOrGetStatsHandler(w http.ResponseWriter, r *http.Request) {
	// Set up http response and defer writing output
	res := SyncOrGetResponse{ErrorMessage:"An unknown error was encountered"}
	defer sendSerializedJSON(w, &res)

	// Parse request
	var req SyncOrGetRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		res.ErrorMessage = "Invalid JSON"
		return
	}

	// Attempt to login
	raidStats := loginRaidStats(req.RaidGroup, req.RaidPassword)
	if raidStats == nil {
		res.ErrorMessage = "Invalid RaidGroup or RaidPassword"
		return
	}

	// Save stats
	if r.URL.Path == syncRaidStatsPath {
		updateRaidStats(raidStats, req.Statistics)
	}

	// Prepare response
	res.ErrorMessage = ""
	res.Users = raidStats.Users
	res.MinimumPollingRate = 1
}

func loginRaid(group string, password string) uint32 {
	var id uint32
	var groupPassword string
	loginStmt.QueryRow(group).Scan(&id, &groupPassword)
	if password == groupPassword {
		return id
	} else {
		return 0
	}
}

func loginRaidStats(group string, password string) *RaidStats {
	// Login to get group id
	groupId := loginRaid(group, password)
	if groupId <= 0 {
		return nil
	}

	// Get or create RaidStats and update access time
	allRaidStats.RLock()
	raidStats, ok := allRaidStats.Raids[groupId]
	allRaidStats.RUnlock()
	if ok {
		raidStats.LastActivity = time.Now()
	} else {
		log.Printf("Creating raid stats collection for: %s (%d)", group, groupId)
		users := make([]*RaidUser, 0, 8)
		raidStats = &RaidStats{groupId, group, users, time.Now()}
		allRaidStats.Lock()
		allRaidStats.Raids[groupId] = raidStats
		allRaidStats.Unlock()
	}

	return raidStats
}

func updateRaidStats(raidStats *RaidStats, parsedUser RaidUser) {
	nowString := time.Now().UTC().Format(time.RFC3339)

	// Update existing user or create new one
	var user *RaidUser
	for i := 0; i < len(raidStats.Users); i++ {
		if raidStats.Users[i].RaidUserId == parsedUser.RaidUserId {
			user = raidStats.Users[i]
			user.DamageOut            = parsedUser.DamageOut
			user.DamageIn             = parsedUser.DamageIn
			user.HealOut              = parsedUser.HealOut
			user.EffectiveHealOut     = parsedUser.EffectiveHealOut
			user.HealIn               = parsedUser.HealIn
			user.Threat               = parsedUser.Threat
			user.RaidEncounterId      = parsedUser.RaidEncounterId
			user.RaidEncounterMode    = parsedUser.RaidEncounterMode
			user.RaidEncounterPlayers = parsedUser.RaidEncounterPlayers
			user.CombatTicks          = parsedUser.CombatTicks
			user.CombatStart          = parsedUser.CombatStart
			user.CombatEnd            = parsedUser.CombatEnd
			break
		}
	}
	if user == nil {
		user = &parsedUser
		user.RaidGroupId = raidStats.GroupId
		raidStats.Users = append(raidStats.Users, user)
	}

	// Update user server-managed properties
	user.LastConnectDate  = nowString
	user.IsConnected      = true
	user.LastCombatUpdate = nowString
}

func sendSerializedJSON(w http.ResponseWriter, res interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Encoding", "gzip")
	gz, _ := cgzip.NewWriterLevel(w, 1)
	json.NewEncoder(gz).Encode(&res)
	gz.Close()
}

// Go through all raid groups and remove those that are inactive
func garbageCollectRaidStats() {
	tick := time.Tick(5*time.Minute)
	for {
		<-tick

		now := time.Now()
		inactiveGroupIds := make([]uint32, 0, 10)

		// Build list of inactive group ids
		allRaidStats.RLock()
		for k := range allRaidStats.Raids {
			raidStats := allRaidStats.Raids[k]
			inactiveDuration := now.Sub(raidStats.LastActivity)
			if inactiveDuration > 60*time.Minute {
				inactiveGroupIds = append(inactiveGroupIds, k)
			}
		}
		allRaidStats.RUnlock()

		// Delete inactive groups
		if len(inactiveGroupIds) > 0 {
			log.Printf("Deleting inactive group ids: %v", inactiveGroupIds)
			allRaidStats.Lock()
			for i := 0; i < len(inactiveGroupIds); i++ {
				delete(allRaidStats.Raids, inactiveGroupIds[i])
			}
			allRaidStats.Unlock()
		}
	}
}