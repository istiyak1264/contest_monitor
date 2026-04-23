package main

import (
	"database/sql"
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"log"
	"net"
	"os"
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

// ── Models ───────────────────────────────────────────────────────────────────

type User struct {
	ID       uint   `json:"id"    gorm:"primaryKey"`
	Name     string `json:"name"`
	Email    string `json:"email" gorm:"unique"`
	Password string `json:"-"`
}

type Contest struct {
	ID               uint      `json:"id"                gorm:"primaryKey"`
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

// ── Globals ───────────────────────────────────────────────────────────────────

var (
	db        *gorm.DB
	sqlDB     *sql.DB
	jwtSecret = []byte(getEnv("JWT_SECRET", "kali-linux-super-secret-key"))
	aiDomains = []string{
		"chatgpt", "openai", "gemini", "grok", "claude", "anthropic",
		"perplexity", "deepseek", "manus", "stackoverflow", "geeksforgeeks",
	}
)

// ── Helpers ───────────────────────────────────────────────────────────────────

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func normalizeIP(addr string) string {
	ip := addr
	if host, _, err := net.SplitHostPort(addr); err == nil {
		ip = host
	}
	if parsed := net.ParseIP(ip); parsed != nil {
		if v4 := parsed.To4(); v4 != nil {
			return v4.String()
		}
	}
	return ip
}

func containsAIDomain(s string) string {
	lower := strings.ToLower(s)
	for _, d := range aiDomains {
		if strings.Contains(lower, d) {
			return d
		}
	}
	return ""
}

// ── main ─────────────────────────────────────────────────────────────────────

func main() {
	probeInterfaces()

	dsn := fmt.Sprintf("%s:%s@tcp(%s:3306)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		getEnv("DB_USER", "root"),
		getEnv("DB_PASS", ""),
		getEnv("DB_HOST", "127.0.0.1"),
		getEnv("DB_NAME", "auth_db"),
	)

	var err error
	db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("[DB] connect error: %v", err)
	}

	sqlDB, err = db.DB()
	if err != nil {
		log.Fatalf("[DB] get underlying sql.DB error: %v", err)
	}

	if err = db.AutoMigrate(&User{}, &Contest{}); err != nil {
		log.Fatalf("[DB] auto-migrate error: %v", err)
	}

	gin.SetMode(getEnv("GIN_MODE", "release"))
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

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

	if err := r.Run(":8080"); err != nil {
		log.Fatalf("[GIN] server error: %v", err)
	}
}

// ── Interface probing ─────────────────────────────────────────────────────────

type probeResult struct {
	name   string
	ips    []string
	usable bool
	reason string // populated when usable == false
}
func probeInterfaces() []string {
	log.Println("[IFACE] ── Probing network interfaces ──────────────────────")

	devs, err := pcap.FindAllDevs()
	if err != nil {
		log.Printf("[IFACE] FindAllDevs error: %v", err)
		return nil
	}

	var usable []string
	for _, dev := range devs {
		r := evaluateInterface(dev)
		if r.usable {
			usable = append(usable, r.name)
			log.Printf("[IFACE] ✓  %-15s  IPs: %s", r.name, strings.Join(r.ips, ", "))
		} else {
			log.Printf("[IFACE] ✗  %-15s  skip: %s", r.name, r.reason)
		}
	}

	if len(usable) == 0 {
		log.Println("[IFACE] WARNING: no usable interfaces found — sniffing will not work")
	} else {
		log.Printf("[IFACE] Ready to sniff %d interface(s): %s",
			len(usable), strings.Join(usable, ", "))
	}
	log.Println("[IFACE] ────────────────────────────────────────────────────")
	return usable
}

func evaluateInterface(dev pcap.Interface) probeResult {
	r := probeResult{name: dev.Name}

	// 1. Skip the "any" pseudo-device.
	if dev.Name == "any" {
		r.reason = "pseudo-device (skipped to ensure real LAN traffic is captured)"
		return r
	}

	// 2. Skip loopback — checked by name prefix and pcap flag (FlagLoopback = 1).
	if dev.Name == "lo" || strings.HasPrefix(dev.Name, "lo:") || dev.Flags&0x1 != 0 {
		r.reason = "loopback"
		return r
	}

	// 3. Must have at least one assigned IP address.
	for _, addr := range dev.Addresses {
		if ip := addr.IP.String(); ip != "" && ip != "<nil>" {
			r.ips = append(r.ips, ip)
		}
	}
	if len(r.ips) == 0 {
		r.reason = "no IP address assigned (interface may be down)"
		return r
	}

	// 4. Try opening with pcap to confirm permissions and driver support.
	handle, err := pcap.OpenLive(dev.Name, 65535, true, pcap.BlockForever)
	if err != nil {
		r.reason = fmt.Sprintf("pcap open failed: %v", err)
		return r
	}
	handle.Close()

	r.usable = true
	return r
}

func getSniffInterfaces() []string {
	if override := getEnv("SNIFF_IFACE", ""); override != "" {
		var result []string
		for _, name := range strings.Split(override, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			h, err := pcap.OpenLive(name, 65535, true, pcap.BlockForever)
			if err != nil {
				log.Printf("[IFACE] SNIFF_IFACE=%q cannot open: %v — skipping", name, err)
				continue
			}
			h.Close()
			log.Printf("[IFACE] SNIFF_IFACE override accepted: %s", name)
			result = append(result, name)
		}
		if len(result) > 0 {
			return result
		}
		log.Println("[IFACE] SNIFF_IFACE produced no usable interfaces — falling back to auto-detect")
	}

	// Auto-detect all usable interfaces.
	devs, err := pcap.FindAllDevs()
	if err != nil {
		log.Printf("[IFACE] FindAllDevs error: %v", err)
		return nil
	}
	var result []string
	for _, dev := range devs {
		if r := evaluateInterface(dev); r.usable {
			result = append(result, r.name)
		}
	}
	return result
}

// ── Table helpers ─────────────────────────────────────────────────────────────

func createParticipantsTable(name string) error {
	q := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS `+"`%s`"+` (
			id           INT AUTO_INCREMENT PRIMARY KEY,
			team_name    TEXT,
			ip           TEXT,
			members      TEXT,
			ai_violation TINYINT(1) DEFAULT 0,
			last_seen    DATETIME
		)`, name)
	_, err := sqlDB.Exec(q)
	return err
}

func createTrafficLogsTable(name string) error {
	q := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS `+"`%s`"+` (
			id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
			ip         TEXT,
			ai_service TEXT,
			timestamp  DATETIME(3)
		)`, name)
	_, err := sqlDB.Exec(q)
	return err
}

func createAIHitsTable(name string) error {
	q := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS `+"`%s`"+` (
			id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
			contest_id BIGINT UNSIGNED,
			ip         TEXT,
			domain     TEXT,
			created_at DATETIME(3)
		)`, name)
	_, err := sqlDB.Exec(q)
	return err
}

// ── SNI extraction ────────────────────────────────────────────────────────────

func extractSNI(payload []byte) string {
	if len(payload) < 6 || payload[0] != 0x16 || payload[5] != 0x01 {
		return ""
	}

	pos := 43
	if pos >= len(payload) {
		return ""
	}

	// Session ID
	sessionIDLen := int(payload[pos])
	pos += 1 + sessionIDLen
	if pos+2 > len(payload) {
		return ""
	}

	// Cipher suites
	cipherSuitesLen := int(binary.BigEndian.Uint16(payload[pos : pos+2]))
	pos += 2 + cipherSuitesLen
	if pos+1 > len(payload) {
		return ""
	}

	// Compression methods
	compressionLen := int(payload[pos])
	pos += 1 + compressionLen
	if pos+2 > len(payload) {
		return ""
	}

	// Extensions block
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

func extractDNSHostnames(payload []byte) []string {
	pkt := gopacket.NewPacket(payload, layers.LayerTypeDNS, gopacket.Default)
	dnsLayer := pkt.Layer(layers.LayerTypeDNS)
	if dnsLayer == nil {
		return nil
	}
	dns, _ := dnsLayer.(*layers.DNS)
	if dns.QR { // QR=1 → response
		return nil
	}
	var names []string
	for _, q := range dns.Questions {
		if name := strings.TrimSpace(string(q.Name)); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// ── Sniffer ───────────────────────────────────────────────────────────────────
func startSniffer(contest Contest) {
	ifaces := getSniffInterfaces()
	if len(ifaces) == 0 {
		log.Printf("[SNIFFER] contest %d: no usable interfaces found — sniffing disabled", contest.ID)
		return
	}
	log.Printf("[SNIFFER] contest %d: starting on interface(s): %s",
		contest.ID, strings.Join(ifaces, ", "))
	for _, iface := range ifaces {
		go sniffInterface(iface, contest)
	}
}

func sniffInterface(iface string, contest Contest) {
	handle, err := pcap.OpenLive(iface, 65535, true, pcap.BlockForever)
	if err != nil {
		log.Printf("[SNIFFER] [%s] open error: %v", iface, err)
		return
	}
	defer handle.Close()

	const bpf = "tcp port 443 or tcp port 80 or udp port 53"
	if err := handle.SetBPFFilter(bpf); err != nil {
		log.Printf("[SNIFFER] [%s] BPF filter error: %v", iface, err)
		return
	}

	log.Printf("[SNIFFER] [%s] listening — contest %d  (%s → %s)",
		iface, contest.ID,
		contest.StartTime.Format("15:04:05"),
		contest.EndTime.Format("15:04:05"),
	)

	src := gopacket.NewPacketSource(handle, handle.LinkType())
	src.DecodeOptions.Lazy = true
	src.DecodeOptions.NoCopy = true

	for pkt := range src.Packets() {
		now := time.Now()
		if now.After(contest.EndTime) {
			log.Printf("[SNIFFER] [%s] contest %d ended — stopping", iface, contest.ID)
			return
		}
		if now.Before(contest.StartTime) {
			continue
		}

		netLayer := pkt.NetworkLayer()
		if netLayer == nil {
			continue
		}
		srcIP := normalizeIP(netLayer.NetworkFlow().Src().String())

		detected := ""

		// Layer 1 — DNS query (UDP 53).
		if detected == "" {
			if udpLayer := pkt.Layer(layers.LayerTypeUDP); udpLayer != nil {
				udp, _ := udpLayer.(*layers.UDP)
				for _, name := range extractDNSHostnames(udp.Payload) {
					if d := containsAIDomain(name); d != "" {
						detected = d
						break
					}
				}
			}
		}

		// Layer 2 — TLS SNI (TCP 443).
		if detected == "" {
			if tcpLayer := pkt.Layer(layers.LayerTypeTCP); tcpLayer != nil {
				tcp, _ := tcpLayer.(*layers.TCP)
				if len(tcp.Payload) > 0 {
					if sni := extractSNI(tcp.Payload); sni != "" {
						detected = containsAIDomain(sni)
					}
				}
			}
		}

		// Layer 3 — Plain HTTP Host header (TCP 80).
		if detected == "" {
			if app := pkt.ApplicationLayer(); app != nil {
				detected = containsAIDomain(string(app.Payload()))
			}
		}

		if detected != "" {
			recordHit(contest, srcIP, detected)
		}
	}
}

// ── Hit recording ─────────────────────────────────────────────────────────────

// recordHit writes a detection event to all three contest tables:
//  1. traffic_logs  — full unbounded history (every single hit).
//  2. ai_hits       — deduplicated per IP+domain within a 10-minute window
//     (prevents log flooding while still recording repeat visits).
//  3. participants  — sets ai_violation=1 and updates last_seen for the team.
func recordHit(contest Contest, srcIP, domain string) {
	// 1. Full traffic history.
	tlQ := fmt.Sprintf(
		"INSERT INTO `%s` (ip, ai_service, timestamp) VALUES (?, ?, NOW(3))",
		contest.TrafficLogsTable,
	)
	if _, err := sqlDB.Exec(tlQ, srcIP, domain); err != nil {
		log.Printf("[SNIFFER] traffic_logs insert error: %v", err)
	}

	// 2. Deduped ai_hits — only insert if no matching row in the last 10 minutes.
	ahQ := fmt.Sprintf(`
		INSERT INTO `+"`%s`"+` (contest_id, ip, domain, created_at)
		SELECT ?, ?, ?, NOW(3)
		WHERE NOT EXISTS (
			SELECT 1 FROM `+"`%s`"+`
			WHERE ip = ? AND domain = ?
			  AND created_at >= NOW(3) - INTERVAL 10 MINUTE
		)`,
		contest.AIHitsTable, contest.AIHitsTable,
	)
	if _, err := sqlDB.Exec(ahQ, contest.ID, srcIP, domain, srcIP, domain); err != nil {
		log.Printf("[SNIFFER] ai_hits insert error: %v", err)
	}

	// 3. Flag the participant and record when the violation last occurred.
	updQ := fmt.Sprintf(
		"UPDATE `%s` SET ai_violation = 1, last_seen = NOW() WHERE ip = ?",
		contest.TableName,
	)
	res, err := sqlDB.Exec(updQ, srcIP)
	if err != nil {
		log.Printf("[SNIFFER] participant update error: %v", err)
		return
	}
	affected, _ := res.RowsAffected()
	log.Printf("[SNIFFER] domain=%-20s  src=%-18s  rows_updated=%d", domain, srcIP, affected)
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func hostContest(c *gin.Context) {
	name := strings.TrimSpace(c.PostForm("contestName"))
	if name == "" {
		c.JSON(400, gin.H{"error": "contestName is required"})
		return
	}

	durationStr := c.PostForm("duration")
	duration, err := time.ParseDuration(durationStr + "m")
	if err != nil || duration <= 0 {
		c.JSON(400, gin.H{"error": "invalid duration (positive integer minutes expected)"})
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "csv file required"})
		return
	}

	// Nanosecond suffix guarantees unique table names even under rapid requests.
	ts := time.Now().UnixNano()
	participantTable := fmt.Sprintf("contest_%d", ts)
	trafficTable := fmt.Sprintf("traffic_logs_%d", ts)
	aiHitsTable := fmt.Sprintf("ai_hits_%d", ts)

	if err := createParticipantsTable(participantTable); err != nil {
		log.Printf("[HOST] create participants table error: %v", err)
		c.JSON(500, gin.H{"error": "failed to create participant table"})
		return
	}
	if err := createTrafficLogsTable(trafficTable); err != nil {
		log.Printf("[HOST] create traffic table error: %v", err)
		c.JSON(500, gin.H{"error": "failed to create traffic table"})
		return
	}
	if err := createAIHitsTable(aiHitsTable); err != nil {
		log.Printf("[HOST] create ai_hits table error: %v", err)
		c.JSON(500, gin.H{"error": "failed to create ai_hits table"})
		return
	}

	f, err := file.Open()
	if err != nil {
		c.JSON(400, gin.H{"error": "failed to open uploaded file"})
		return
	}
	defer f.Close()

	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		c.JSON(400, gin.H{"error": "failed to parse CSV"})
		return
	}

	insertQ := fmt.Sprintf("INSERT INTO `%s` (team_name, ip, members) VALUES (?, ?, ?)", participantTable)
	for i := 1; i < len(records); i++ {
		row := records[i]
		if len(row) < 2 {
			continue
		}
		teamName := strings.TrimSpace(row[0])
		ip := normalizeIP(strings.TrimSpace(row[1]))
		members := ""
		if len(row) > 2 {
			members = strings.Join(row[2:], ", ")
		}
		if _, err := sqlDB.Exec(insertQ, teamName, ip, members); err != nil {
			log.Printf("[HOST] insert participant error (row %d): %v", i, err)
		}
	}

	now := time.Now()
	contest := Contest{
		Name:             name,
		StartTime:        now,
		EndTime:          now.Add(duration),
		TableName:        participantTable,
		TrafficLogsTable: trafficTable,
		AIHitsTable:      aiHitsTable,
	}
	if err := db.Create(&contest).Error; err != nil {
		log.Printf("[HOST] create contest record error: %v", err)
		c.JSON(500, gin.H{"error": "failed to create contest"})
		return
	}

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
		if tbl == "" {
			continue
		}
		if _, err := sqlDB.Exec(fmt.Sprintf("DROP TABLE IF EXISTS `%s`", tbl)); err != nil {
			log.Printf("[DELETE] drop table %q error: %v", tbl, err)
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

	statuses := []TeamStatus{}
	for rows.Next() {
		var s TeamStatus
		var v int
		var ls sql.NullTime
		if err := rows.Scan(&s.Name, &s.Members, &s.IP, &v, &ls); err != nil {
			log.Printf("[MONITOR] scan error: %v", err)
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
	if err := rows.Err(); err != nil {
		log.Printf("[MONITOR] rows error: %v", err)
	}
	c.JSON(200, statuses)
}

func getViolations(c *gin.Context) {
	var contest Contest
	if err := db.First(&contest, c.Param("id")).Error; err != nil {
		c.JSON(404, gin.H{"error": "contest not found"})
		return
	}

	q := fmt.Sprintf(`
		SELECT p.team_name, p.members, p.ip, p.last_seen,
		       COALESCE((
		           SELECT ah.domain FROM `+"`%s`"+` ah
		           WHERE ah.ip = p.ip
		           ORDER BY ah.created_at DESC LIMIT 1
		       ), '') AS domain
		FROM `+"`%s`"+` p
		WHERE p.ai_violation = 1`,
		contest.AIHitsTable,
		contest.TableName,
	)
	rows, err := sqlDB.Query(q)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	violations := []ViolationTeam{}
	for rows.Next() {
		var teamName, membersRaw, ip, domain string
		var ls sql.NullTime
		if err := rows.Scan(&teamName, &membersRaw, &ip, &ls, &domain); err != nil {
			log.Printf("[VIOLATIONS] scan error: %v", err)
			continue
		}
		detectedAt := "Unknown"
		if ls.Valid {
			detectedAt = ls.Time.Format("15:04:05")
		}
		violations = append(violations, ViolationTeam{
			TeamName:   teamName,
			Members:    splitMembers(membersRaw),
			IP:         ip,
			DetectedAt: detectedAt,
			Domain:     domain,
		})
	}
	if err := rows.Err(); err != nil {
		log.Printf("[VIOLATIONS] rows error: %v", err)
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

	hits := []AIHitDetail{}
	for rows.Next() {
		var ip, domain string
		var teamName, membersRaw sql.NullString
		var createdAt sql.NullTime
		if err := rows.Scan(&ip, &teamName, &membersRaw, &domain, &createdAt); err != nil {
			log.Printf("[AI-HITS] scan error: %v", err)
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
	if err := rows.Err(); err != nil {
		log.Printf("[AI-HITS] rows error: %v", err)
	}
	c.JSON(200, hits)
}

func getContests(c *gin.Context) {
	var list []Contest
	db.Find(&list)
	c.JSON(200, list)
}

func register(c *gin.Context) {
	var in struct {
		FirstName string `json:"FirstName"`
		LastName  string `json:"LastName"`
		Email     string `json:"Email"`
		Password  string `json:"Password"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}
	pw, err := bcrypt.GenerateFromPassword([]byte(in.Password), 10)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to hash password"})
		return
	}
	if err := db.Create(&User{
		Name:     strings.TrimSpace(in.FirstName + " " + in.LastName),
		Email:    in.Email,
		Password: string(pw),
	}).Error; err != nil {
		c.JSON(500, gin.H{"error": "failed to create user"})
		return
	}
	c.JSON(200, gin.H{"message": "success"})
}

func login(c *gin.Context) {
	var in struct {
		Email    string `json:"Email"`
		Password string `json:"Password"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}
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

// ── Utilities ─────────────────────────────────────────────────────────────────

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