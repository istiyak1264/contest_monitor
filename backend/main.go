package main

import (
	"database/sql"
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// ── Models ──────────────────────────────────────────────────────────────────

type User struct {
	ID       uint   `json:"id"    gorm:"primaryKey"`
	Name     string `json:"name"`
	Email    string `json:"email" gorm:"unique"`
	Password string `json:"-"`
}

type Contest struct {
	ID               uint      `json:"id"               gorm:"primaryKey"`
	Name             string    `json:"name"`
	StartTime        time.Time `json:"start_time"`
	EndTime          time.Time `json:"end_time"`
	TableName        string    `json:"table_name"`
	TrafficLogsTable string    `json:"traffic_logs_table"`
	AIHitsTable      string    `json:"ai_hits_table"`
}

// TeamStatus is returned by the monitor endpoint.
type TeamStatus struct {
	Name      string `json:"name"`
	Members   string `json:"members"`
	IP        string `json:"ip"`
	AIStatus  string `json:"ai_status"`
	IsWarning bool   `json:"is_warning"`
	LastSeen  string `json:"last_seen"`
}

// ViolationTeam is returned by the violations endpoint.
type ViolationTeam struct {
	TeamName   string   `json:"team_name"`
	Members    []string `json:"members"`
	IP         string   `json:"ip"`
	DetectedAt string   `json:"detected_at"`
	Domain     string   `json:"domain"`
}

// AIHitDetail is returned by the ai-hits endpoint.
type AIHitDetail struct {
	IP       string   `json:"ip"`
	TeamName string   `json:"team_name"`
	Members  []string `json:"members"`
	Domain   string   `json:"domain"`
	HitTime  string   `json:"hit_time"`
}

// ── Globals ──────────────────────────────────────────────────────────────────

var (
	db        *gorm.DB
	sqlDB     *sql.DB
	jwtSecret = []byte("kali-linux-super-secret-key")
	aiDomains = []string{
		"chatgpt", "openai", "gemini", "grok", "claude", "anthropic",
		"perplexity", "deepseek", "manus", "stackoverflow", "geeksforgeeks",
	}
)

// ── main ─────────────────────────────────────────────────────────────────────

func main() {
	dsn := "root:1712750452@tcp(127.0.0.1:3306)/auth_db?charset=utf8mb4&parseTime=True&loc=Local"
	var err error
	db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal(err)
	}
	sqlDB, _ = db.DB()
	db.AutoMigrate(&User{}, &Contest{})

	r := gin.Default()

	// CORS
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, DELETE")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// Auth
	r.POST("/login", login)
	r.POST("/register", register)

	// Contests
	r.POST("/host-contest", hostContest)
	r.GET("/contests", getContests)
	r.DELETE("/contests/:id", deleteContest)

	// Monitor
	r.GET("/contests/:id/monitor", monitorTelemetry)
	r.GET("/contests/:id/violations", getViolations)
	r.GET("/contests/:id/ai-hits", getAIHits)

	r.Run(":8080")
}

func createTrafficLogsTable(name string) {
	q := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS `+"`%s`"+` (
			id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
			ip         TEXT,
			ai_service TEXT,
			timestamp  DATETIME(3)
		)`, name)
	if _, err := sqlDB.Exec(q); err != nil {
		log.Printf("[DB] create traffic table error: %v", err)
	}
}

func createAIHitsTable(name string) {
	q := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS `+"`%s`"+` (
			id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
			contest_id BIGINT UNSIGNED,
			ip         TEXT,
			domain     TEXT,
			created_at DATETIME(3)
		)`, name)
	if _, err := sqlDB.Exec(q); err != nil {
		log.Printf("[DB] create ai_hits table error: %v", err)
	}
}

func extractSNI(payload []byte) string {
	if len(payload) < 6 || payload[0] != 0x16 || payload[5] != 0x01 {
		return ""
	}
	pos := 43
	if pos >= len(payload) {
		return ""
	}
	sessionIDLen := int(payload[pos])
	pos += 1 + sessionIDLen
	if pos+2 > len(payload) {
		return ""
	}
	cipherSuitesLen := int(binary.BigEndian.Uint16(payload[pos : pos+2]))
	pos += 2 + cipherSuitesLen
	if pos+1 > len(payload) {
		return ""
	}
	compressionLen := int(payload[pos])
	pos += 1 + compressionLen
	if pos+2 > len(payload) {
		return ""
	}
	extensionsLen := int(binary.BigEndian.Uint16(payload[pos : pos+2]))
	pos += 2
	end := pos + extensionsLen
	if end > len(payload) {
		end = len(payload)
	}
	for pos+4 <= end {
		extType := binary.BigEndian.Uint16(payload[pos : pos+2])
		extLen := int(binary.BigEndian.Uint16(payload[pos+2 : pos+4]))
		pos += 4
		if pos+extLen > end {
			break
		}
		if extType == 0x0000 && extLen > 5 {
			nameLen := int(binary.BigEndian.Uint16(payload[pos+3 : pos+5]))
			if pos+5+nameLen <= end {
				return string(payload[pos+5 : pos+5+nameLen])
			}
		}
		pos += extLen
	}
	return ""
}

func startSniffer(contest Contest) {
	handle, err := pcap.OpenLive("any", 65535, true, pcap.BlockForever)
	if err != nil {
		log.Printf("[SNIFFER] open error: %v", err)
		return
	}
	defer handle.Close()

	if err := handle.SetBPFFilter("tcp port 443 or tcp port 80"); err != nil {
		log.Printf("[SNIFFER] BPF error: %v", err)
		return
	}

	src := gopacket.NewPacketSource(handle, handle.LinkType())

	for pkt := range src.Packets() {
		now := time.Now()
		if now.Before(contest.StartTime) || now.After(contest.EndTime) {
			if now.After(contest.EndTime) {
				log.Printf("[SNIFFER] contest %d ended, stopping", contest.ID)
				break
			}
			continue
		}

		netLayer := pkt.NetworkLayer()
		if netLayer == nil {
			continue
		}
		srcIP := netLayer.NetworkFlow().Src().String()

		detected := ""

		tcpLayer := pkt.Layer(layers.LayerTypeTCP)
		if tcpLayer != nil {
			tcp, _ := tcpLayer.(*layers.TCP)
			if len(tcp.Payload) > 0 {
				sni := extractSNI(tcp.Payload)
				if sni != "" {
					lower := strings.ToLower(sni)
					for _, d := range aiDomains {
						if strings.Contains(lower, d) {
							detected = d
							break
						}
					}
				}
			}
		}

		if detected == "" {
			appLayer := pkt.ApplicationLayer()
			if appLayer != nil {
				lower := strings.ToLower(string(appLayer.Payload()))
				for _, d := range aiDomains {
					if strings.Contains(lower, d) {
						detected = d
						break
					}
				}
			}
		}

		if detected != "" {
			recordHit(contest, srcIP, detected)
		}
	}
}

func recordHit(contest Contest, srcIP, domain string) {
	tlQ := fmt.Sprintf(
		"INSERT INTO `%s` (ip, ai_service, timestamp) VALUES (?, ?, NOW(3))",
		contest.TrafficLogsTable,
	)
	if _, err := sqlDB.Exec(tlQ, srcIP, domain); err != nil {
		log.Printf("[SNIFFER] traffic_logs insert error: %v", err)
	}

	var exists int
	checkQ := fmt.Sprintf(
		"SELECT COUNT(*) FROM `%s` WHERE ip = ? AND domain = ?",
		contest.AIHitsTable,
	)
	if err := sqlDB.QueryRow(checkQ, srcIP, domain).Scan(&exists); err != nil {
		log.Printf("[SNIFFER] ai_hits check error: %v", err)
		exists = 0
	}
	if exists == 0 {
		ahQ := fmt.Sprintf(
			"INSERT INTO `%s` (contest_id, ip, domain, created_at) VALUES (?, ?, ?, NOW(3))",
			contest.AIHitsTable,
		)
		if _, err := sqlDB.Exec(ahQ, contest.ID, srcIP, domain); err != nil {
			log.Printf("[SNIFFER] ai_hits insert error: %v", err)
		}
	}

	updQ := fmt.Sprintf(
		"UPDATE `%s` SET ai_violation = 1, last_seen = NOW() WHERE ip = ?",
		contest.TableName,
	)
	if res, err := sqlDB.Exec(updQ, srcIP); err != nil {
		log.Printf("[SNIFFER] participant update error: %v", err)
	} else {
		rows, _ := res.RowsAffected()
		log.Printf("[SNIFFER] domain=%s ip=%s rows_updated=%d", domain, srcIP, rows)
	}
}

func hostContest(c *gin.Context) {
	name := c.PostForm("contestName")
	durationStr := c.PostForm("duration")
	duration, err := time.ParseDuration(durationStr + "m")
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid duration"})
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "csv file required"})
		return
	}

	ts := time.Now().Unix()
	participantTable := fmt.Sprintf("contest_%d", ts)
	trafficTable := fmt.Sprintf("traffic_logs_%d", ts)
	aiHitsTable := fmt.Sprintf("ai_hits_%d", ts)

	sqlDB.Exec(fmt.Sprintf(`
		CREATE TABLE `+"`%s`"+` (
			id           INT AUTO_INCREMENT PRIMARY KEY,
			team_name    TEXT,
			ip           TEXT,
			members      TEXT,
			ai_violation TINYINT(1) DEFAULT 0,
			last_seen    DATETIME
		)`, participantTable))

	createTrafficLogsTable(trafficTable)
	createAIHitsTable(aiHitsTable)

	f, _ := file.Open()
	records, _ := csv.NewReader(f).ReadAll()
	for i := 1; i < len(records); i++ {
		row := records[i]
		if len(row) < 2 {
			continue
		}
		teamName := row[0]
		ip := row[1]
		members := ""
		if len(row) > 2 {
			members = strings.Join(row[2:], ", ")
		}
		sqlDB.Exec(
			fmt.Sprintf("INSERT INTO `%s` (team_name, ip, members) VALUES (?, ?, ?)", participantTable),
			teamName, ip, members,
		)
	}

	contest := Contest{
		Name:             name,
		StartTime:        time.Now(),
		EndTime:          time.Now().Add(duration),
		TableName:        participantTable,
		TrafficLogsTable: trafficTable,
		AIHitsTable:      aiHitsTable,
	}
	db.Create(&contest)

	go startSniffer(contest)

	c.JSON(200, contest)
}

func deleteContest(c *gin.Context) {
	var contest Contest
	if err := db.First(&contest, c.Param("id")).Error; err != nil {
		c.JSON(404, gin.H{"error": "contest not found"})
		return
	}

	for _, tbl := range []string{contest.TableName, contest.TrafficLogsTable, contest.AIHitsTable} {
		if tbl != "" {
			sqlDB.Exec(fmt.Sprintf("DROP TABLE IF EXISTS `%s`", tbl))
		}
	}

	db.Delete(&contest)
	c.JSON(200, gin.H{"status": "deleted"})
}

func monitorTelemetry(c *gin.Context) {
	var contest Contest
	if err := db.First(&contest, c.Param("id")).Error; err != nil {
		c.JSON(404, gin.H{"error": "contest not found"})
		return
	}

	q := fmt.Sprintf(
		"SELECT team_name, members, ip, CAST(ai_violation AS UNSIGNED), last_seen FROM `%s`",
		contest.TableName,
	)
	rows, err := sqlDB.Query(q)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var statuses []TeamStatus
	for rows.Next() {
		var s TeamStatus
		var v int
		var ls sql.NullTime
		if err := rows.Scan(&s.Name, &s.Members, &s.IP, &v, &ls); err != nil {
			continue
		}
		s.IsWarning = v != 0
		s.AIStatus = "CLEAN"
		if s.IsWarning {
			s.AIStatus = "AI SITE DETECTED"
		}
		s.LastSeen = "Never"
		if ls.Valid {
			s.LastSeen = ls.Time.Format("15:04:05")
		}
		statuses = append(statuses, s)
	}
	if statuses == nil {
		statuses = []TeamStatus{}
	}
	c.JSON(200, statuses)
}

func getViolations(c *gin.Context) {
	var contest Contest
	if err := db.First(&contest, c.Param("id")).Error; err != nil {
		c.JSON(404, gin.H{"error": "contest not found"})
		return
	}

	q := fmt.Sprintf(
		"SELECT team_name, members, ip, last_seen FROM `%s` WHERE ai_violation = 1",
		contest.TableName,
	)
	rows, err := sqlDB.Query(q)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var violations []ViolationTeam
	for rows.Next() {
		var teamName, membersRaw, ip string
		var ls sql.NullTime
		if err := rows.Scan(&teamName, &membersRaw, &ip, &ls); err != nil {
			continue
		}
		members := splitMembers(membersRaw)
		detectedAt := "Unknown"
		if ls.Valid {
			detectedAt = ls.Time.Format("15:04:05")
		}
		violations = append(violations, ViolationTeam{
			TeamName:   teamName,
			Members:    members,
			IP:         ip,
			DetectedAt: detectedAt,
		})
	}
	if violations == nil {
		violations = []ViolationTeam{}
	}
	c.JSON(200, violations)
}

func getAIHits(c *gin.Context) {
	var contest Contest
	if err := db.First(&contest, c.Param("id")).Error; err != nil {
		c.JSON(404, gin.H{"error": "contest not found"})
		return
	}

	q := fmt.Sprintf(`
		SELECT ah.ip, p.team_name, p.members, ah.domain, ah.created_at
		FROM `+"`%s`"+` ah
		LEFT JOIN `+"`%s`"+` p ON p.ip = ah.ip
		ORDER BY ah.created_at DESC`,
		contest.AIHitsTable,
		contest.TableName,
	)
	rows, err := sqlDB.Query(q)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var hits []AIHitDetail
	for rows.Next() {
		var ip, domain string
		var teamName, membersRaw sql.NullString
		var createdAt sql.NullTime
		if err := rows.Scan(&ip, &teamName, &membersRaw, &domain, &createdAt); err != nil {
			continue
		}
		hitTime := "Unknown"
		if createdAt.Valid {
			hitTime = createdAt.Time.Format("15:04:05")
		}
		team := "Unknown"
		if teamName.Valid {
			team = teamName.String
		}
		members := []string{}
		if membersRaw.Valid {
			members = splitMembers(membersRaw.String)
		}
		hits = append(hits, AIHitDetail{
			IP:       ip,
			TeamName: team,
			Members:  members,
			Domain:   domain,
			HitTime:  hitTime,
		})
	}
	if hits == nil {
		hits = []AIHitDetail{}
	}
	c.JSON(200, hits)
}

func getContests(c *gin.Context) {
	var list []Contest
	db.Find(&list)
	c.JSON(200, list)
}

func register(c *gin.Context) {
	var in struct{ FirstName, LastName, Email, Password string }
	c.ShouldBindJSON(&in)
	pw, _ := bcrypt.GenerateFromPassword([]byte(in.Password), 10)
	db.Create(&User{Name: in.FirstName + " " + in.LastName, Email: in.Email, Password: string(pw)})
	c.JSON(200, gin.H{"message": "success"})
}

func login(c *gin.Context) {
	var in struct{ Email, Password string }
	c.ShouldBindJSON(&in)
	var u User
	if err := db.Where("email = ?", in.Email).First(&u).Error; err == nil {
		if err := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(in.Password)); err == nil {
			token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"user_id": u.ID})
			t, _ := token.SignedString(jwtSecret)
			c.JSON(200, gin.H{"token": t, "user": u})
			return
		}
	}
	c.JSON(401, gin.H{"error": "unauthorized"})
}

func splitMembers(raw string) []string {
	parts := strings.Split(raw, ",")
	var out []string
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
