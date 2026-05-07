package main

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
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

// ── Timezone ──────────────────────────────────────────────────────────────────

// BST is Bangladesh Standard Time (UTC+6). All user-facing timestamps are
// stored and displayed in this zone.
var bst = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Dhaka")
	if err != nil {
		// Fallback: fixed offset UTC+6 (same as Asia/Dhaka, no DST)
		loc = time.FixedZone("BST", 6*3600)
	}
	return loc
}()

// nowBST returns the current time in BST.
func nowBST() time.Time { return time.Now().In(bst) }

// fmtBST formats a time as HH:MM:SS in BST.
func fmtBST(t time.Time) string { return t.In(bst).Format("15:04:05") }

// ── Models ────────────────────────────────────────────────────────────────────

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

type TeamStatus struct {
	Name      string `json:"name"`
	Members   string `json:"members"`
	IP        string `json:"ip"`
	AIStatus  string `json:"ai_status"`
	IsWarning bool   `json:"is_warning"`
	LastSeen  string `json:"last_seen"`
}

type ViolationTeam struct {
	TeamName   string   `json:"team_name"`
	Members    []string `json:"members"`
	IP         string   `json:"ip"`
	DetectedAt string   `json:"detected_at"`
	Domain     string   `json:"domain"`
}

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
	jwtSecret = []byte(getEnv("JWT_SECRET", "change-this-to-a-long-random-secret-key"))
	aiDomains = []string{
		"chatgpt", "openai", "gemini", "grok", "claude", "anthropic",
		"perplexity", "deepseek", "manus", "stackoverflow", "geeksforgeeks",
	}

	snifferCancels   = make(map[uint]context.CancelFunc)
	snifferCancelsMu sync.Mutex
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

// splitMembers splits a comma-separated member string into a trimmed slice.
func splitMembers(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// ── JWT Middleware ────────────────────────────────────────────────────────────

func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authorization header required"})
			return
		}
		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return jwtSecret, nil
		})
		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			return
		}
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token claims"})
			return
		}
		c.Set("user_id", claims["user_id"])
		c.Next()
	}
}

// ── main ─────────────────────────────────────────────────────────────────────

func main() {
	probeInterfaces()

	// Use UTC for MySQL storage; all BST conversion happens in Go.
	dsn := fmt.Sprintf("%s:%s@tcp(%s:3306)/%s?charset=utf8mb4&parseTime=True&loc=UTC",
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
	// Connection pool tuning.
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	if err = db.AutoMigrate(&User{}, &Contest{}); err != nil {
		log.Fatalf("[DB] auto-migrate error: %v", err)
	}

	resumeActiveSniffers()

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

	r.POST("/login", login)
	r.POST("/register", register)

	auth := r.Group("/", authMiddleware())
	{
		auth.POST("/host-contest", hostContest)
		auth.GET("/contests", getContests)
		auth.DELETE("/contests/:id", deleteContest)
		auth.GET("/contests/:id/monitor", monitorTelemetry)
		auth.GET("/contests/:id/violations", getViolations)
		auth.GET("/contests/:id/ai-hits", getAIHits)
	}

	if err := r.Run(":8080"); err != nil {
		log.Fatalf("[GIN] server error: %v", err)
	}
}

// resumeActiveSniffers restarts packet capture for contests still active after restart.
func resumeActiveSniffers() {
	var contests []Contest
	if err := db.Find(&contests).Error; err != nil {
		log.Printf("[RESUME] could not load contests: %v", err)
		return
	}
	now := time.Now().UTC()
	for _, c := range contests {
		if now.Before(c.EndTime) {
			log.Printf("[RESUME] restarting sniffer for contest %d (%s)", c.ID, c.Name)
			startSniffer(c)
		}
	}
}

// ── Interface probing ─────────────────────────────────────────────────────────

func probeInterfaces() {
	log.Println("[IFACE] ── Probing network interfaces ──────────────────────")
	devs, err := pcap.FindAllDevs()
	if err != nil {
		log.Printf("[IFACE] FindAllDevs error: %v", err)
		return
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
		log.Printf("[IFACE] Ready to sniff %d interface(s): %s", len(usable), strings.Join(usable, ", "))
	}
	log.Println("[IFACE] ────────────────────────────────────────────────────")
}

type probeResult struct {
	name   string
	ips    []string
	usable bool
	reason string
}

func evaluateInterface(dev pcap.Interface) probeResult {
	r := probeResult{name: dev.Name}
	if dev.Name == "any" {
		r.reason = "pseudo-device"
		return r
	}
	if dev.Name == "lo" || strings.HasPrefix(dev.Name, "lo:") || dev.Flags&0x1 != 0 {
		r.reason = "loopback"
		return r
	}
	for _, addr := range dev.Addresses {
		if ip := addr.IP.String(); ip != "" && ip != "<nil>" {
			r.ips = append(r.ips, ip)
		}
	}
	if len(r.ips) == 0 {
		r.reason = "no IP address assigned"
		return r
	}
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
			result = append(result, name)
		}
		if len(result) > 0 {
			return result
		}
	}
	devs, err := pcap.FindAllDevs()
	if err != nil {
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
	q := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS `+"`%s`"+` (
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
	q := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS `+"`%s`"+` (
		id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		ip         TEXT,
		ai_service TEXT,
		timestamp  DATETIME(3)
	)`, name)
	_, err := sqlDB.Exec(q)
	return err
}

func createAIHitsTable(name string) error {
	q := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS `+"`%s`"+` (
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

func extractDNSHostnames(payload []byte) []string {
	pkt := gopacket.NewPacket(payload, layers.LayerTypeDNS, gopacket.Default)
	dnsLayer := pkt.Layer(layers.LayerTypeDNS)
	if dnsLayer == nil {
		return nil
	}
	dns, _ := dnsLayer.(*layers.DNS)
	if dns.QR {
		return nil
	}
	names := make([]string, 0, len(dns.Questions))
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
		log.Printf("[SNIFFER] contest %d: no usable interfaces — sniffing disabled", contest.ID)
		return
	}

	ctx, cancel := context.WithDeadline(context.Background(), contest.EndTime)

	snifferCancelsMu.Lock()
	if existing, ok := snifferCancels[contest.ID]; ok {
		existing()
	}
	snifferCancels[contest.ID] = cancel
	snifferCancelsMu.Unlock()

	log.Printf("[SNIFFER] contest %d: starting on %s", contest.ID, strings.Join(ifaces, ", "))
	for _, iface := range ifaces {
		go sniffInterface(ctx, iface, contest)
	}

	go func() {
		<-ctx.Done()
		snifferCancelsMu.Lock()
		delete(snifferCancels, contest.ID)
		snifferCancelsMu.Unlock()
		log.Printf("[SNIFFER] contest %d: all sniffers stopped", contest.ID)
	}()
}

func stopSniffer(contestID uint) {
	snifferCancelsMu.Lock()
	defer snifferCancelsMu.Unlock()
	if cancel, ok := snifferCancels[contestID]; ok {
		cancel()
		delete(snifferCancels, contestID)
	}
}

func sniffInterface(ctx context.Context, iface string, contest Contest) {
	handle, err := pcap.OpenLive(iface, 65535, true, 500*time.Millisecond)
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

	log.Printf("[SNIFFER] [%s] listening — contest %d (%s → %s BST)",
		iface, contest.ID,
		fmtBST(contest.StartTime),
		fmtBST(contest.EndTime),
	)

	src := gopacket.NewPacketSource(handle, handle.LinkType())
	src.DecodeOptions.Lazy = true
	src.DecodeOptions.NoCopy = true

	for {
		select {
		case <-ctx.Done():
			log.Printf("[SNIFFER] [%s] contest %d: context cancelled — stopping", iface, contest.ID)
			return
		case pkt, ok := <-src.Packets():
			if !ok {
				return
			}
			if time.Now().UTC().Before(contest.StartTime) {
				continue
			}

			netLayer := pkt.NetworkLayer()
			if netLayer == nil {
				continue
			}
			srcIP := normalizeIP(netLayer.NetworkFlow().Src().String())
			detected := ""

			// DNS query
			if udpLayer := pkt.Layer(layers.LayerTypeUDP); udpLayer != nil {
				udp, _ := udpLayer.(*layers.UDP)
				for _, name := range extractDNSHostnames(udp.Payload) {
					if d := containsAIDomain(name); d != "" {
						detected = d
						break
					}
				}
			}

			// TLS SNI
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

			// Plain HTTP Host header
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
}

// ── Hit recording ─────────────────────────────────────────────────────────────

func recordHit(contest Contest, srcIP, domain string) {
	// Store UTC timestamps in DB (consistent with loc=UTC DSN).
	tlQ := fmt.Sprintf(
		"INSERT INTO `%s` (ip, ai_service, timestamp) VALUES (?, ?, UTC_TIMESTAMP(3))",
		contest.TrafficLogsTable,
	)
	if _, err := sqlDB.Exec(tlQ, srcIP, domain); err != nil {
		log.Printf("[SNIFFER] traffic_logs insert error: %v", err)
	}

	// Deduped ai_hits — skip if same IP+domain hit within last 10 minutes.
	ahQ := fmt.Sprintf(`
		INSERT INTO `+"`%s`"+` (contest_id, ip, domain, created_at)
		SELECT ?, ?, ?, UTC_TIMESTAMP(3)
		WHERE NOT EXISTS (
			SELECT 1 FROM `+"`%s`"+`
			WHERE ip = ? AND domain = ?
			  AND created_at >= UTC_TIMESTAMP(3) - INTERVAL 10 MINUTE
		)`,
		contest.AIHitsTable, contest.AIHitsTable,
	)
	if _, err := sqlDB.Exec(ahQ, contest.ID, srcIP, domain, srcIP, domain); err != nil {
		log.Printf("[SNIFFER] ai_hits insert error: %v", err)
	}

	// Flag the participant.
	updQ := fmt.Sprintf(
		"UPDATE `%s` SET ai_violation = 1, last_seen = UTC_TIMESTAMP() WHERE ip = ?",
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

	// The frontend sends the user's local BST time as "YYYY-MM-DDTHH:MM".
	// We parse it explicitly in BST (Asia/Dhaka = UTC+6) then store as UTC.
	contestTimeStr := strings.TrimSpace(c.PostForm("contestTime"))
	var startTime time.Time
	if contestTimeStr == "" {
		startTime = time.Now().UTC()
	} else {
		formats := []string{
			"2006-01-02T15:04",
			"2006-01-02T15:04:05",
			time.RFC3339,
		}
		var parseErr error
		for _, f := range formats {
			var t time.Time
			t, parseErr = time.ParseInLocation(f, contestTimeStr, bst)
			if parseErr == nil {
				startTime = t.UTC() // store as UTC
				break
			}
		}
		if parseErr != nil {
			c.JSON(400, gin.H{"error": "invalid contestTime format"})
			return
		}
	}

	durationStr := c.PostForm("duration")
	durationMin, err := time.ParseDuration(durationStr + "m")
	if err != nil || durationMin <= 0 || durationMin > 24*time.Hour {
		c.JSON(400, gin.H{"error": "invalid duration (1–1440 minutes expected)"})
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "csv file required"})
		return
	}
	if !strings.HasSuffix(strings.ToLower(file.Filename), ".csv") {
		c.JSON(400, gin.H{"error": "uploaded file must be a .csv"})
		return
	}

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

	endTime := startTime.Add(durationMin)
	contest := Contest{
		Name:             name,
		StartTime:        startTime,
		EndTime:          endTime,
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
	stopSniffer(contest.ID)
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

	statuses := make([]TeamStatus, 0)
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
			// DB stores UTC; display in BST.
			s.LastSeen = fmtBST(ls.Time)
		}
		statuses = append(statuses, s)
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

	violations := make([]ViolationTeam, 0)
	for rows.Next() {
		var teamName, membersRaw, ip, domain string
		var ls sql.NullTime
		if err := rows.Scan(&teamName, &membersRaw, &ip, &ls, &domain); err != nil {
			log.Printf("[VIOLATIONS] scan error: %v", err)
			continue
		}
		detectedAt := "Unknown"
		if ls.Valid {
			detectedAt = fmtBST(ls.Time)
		}
		violations = append(violations, ViolationTeam{
			TeamName:   teamName,
			Members:    splitMembers(membersRaw),
			IP:         ip,
			DetectedAt: detectedAt,
			Domain:     domain,
		})
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

	hits := make([]AIHitDetail, 0)
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
			hitTime = fmtBST(createdAt.Time)
		}
		team := "Unknown"
		if teamName.Valid {
			team = teamName.String
		}
		members := make([]string, 0)
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
	c.JSON(200, hits)
}

func getContests(c *gin.Context) {
	var list []Contest
	if err := db.Find(&list).Error; err != nil {
		c.JSON(500, gin.H{"error": "failed to fetch contests"})
		return
	}
	c.JSON(200, list)
}

func register(c *gin.Context) {
	var in struct {
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
		Email     string `json:"email"`
		Password  string `json:"password"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}
	in.FirstName = strings.TrimSpace(in.FirstName)
	in.LastName = strings.TrimSpace(in.LastName)
	in.Email = strings.TrimSpace(strings.ToLower(in.Email))

	if in.FirstName == "" || in.Email == "" || in.Password == "" {
		c.JSON(400, gin.H{"error": "firstName, email and password are required"})
		return
	}
	if len(in.Password) < 6 {
		c.JSON(400, gin.H{"error": "password must be at least 6 characters"})
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
		if strings.Contains(err.Error(), "Duplicate") {
			c.JSON(409, gin.H{"error": "email already registered"})
			return
		}
		c.JSON(500, gin.H{"error": "failed to create user"})
		return
	}
	c.JSON(200, gin.H{"message": "success"})
}

func login(c *gin.Context) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}
	var u User
	if err := db.Where("email = ?", strings.TrimSpace(strings.ToLower(in.Email))).First(&u).Error; err == nil {
		if err := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(in.Password)); err == nil {
			token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
				"user_id": u.ID,
				"exp":     time.Now().Add(24 * time.Hour).Unix(),
			})
			t, _ := token.SignedString(jwtSecret)
			c.JSON(200, gin.H{"token": t, "user": u})
			return
		}
	}
	c.JSON(401, gin.H{"error": "invalid email or password"})
}