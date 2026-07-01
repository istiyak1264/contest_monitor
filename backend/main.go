package main

import (
	"context"
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/db"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/api/option"
)

// ── Timezone ──────────────────────────────────────────────────────────────────
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
	ID       string `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password,omitempty"`
	Role string `json:"role"`
}

// sanitizedForClient returns a copy of the user with the password hash removed.
func (u User) sanitizedForClient() User {
	u.Password = ""
	return u
}

type Contest struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
}

// Participant lives at contests/{contestID}/participants/{sanitizedIP}
type Participant struct {
	TeamName    string    `json:"team_name"`
	IP          string    `json:"ip"`
	Members     string    `json:"members"`
	AIViolation bool      `json:"ai_violation"`
	LastSeen    time.Time `json:"last_seen,omitempty"`
}

// TrafficLog lives at contests/{contestID}/traffic_logs/{pushID}
type TrafficLog struct {
	IP        string    `json:"ip"`
	AIService string    `json:"ai_service"`
	Timestamp time.Time `json:"timestamp"`
}

// AIHit lives at contests/{contestID}/ai_hits/{pushID}
type AIHit struct {
	IP        string    `json:"ip"`
	Domain    string    `json:"domain"`
	CreatedAt time.Time `json:"created_at"`
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
	ctx       = context.Background()
	rtdb      *db.Client
	jwtSecret []byte
	aiDomains = []string{
		"chatgpt", "openai", "gemini", "grok", "claude", "anthropic",
		"perplexity", "deepseek", "manus", "stackoverflow", "geeksforgeeks",
	}

	snifferCancels   = make(map[string]context.CancelFunc)
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

// sanitizeKey makes a string safe to use as a Firebase Realtime Database key.
// RTDB forbids '.', '#', '$', '[', ']', '/' in keys — this is used for both
var keyReplacer = strings.NewReplacer(
	".", "_",
	"#", "_",
	"$", "_",
	"[", "_",
	"]", "_",
	"/", "_",
)

func sanitizeKey(s string) string {
	return keyReplacer.Replace(s)
}

// ── Firebase init ─────────────────────────────────────────────────────────────
func initFirebase() {
	dbURL := getEnv("FIREBASE_DATABASE_URL", "")
	if dbURL == "" {
		log.Fatal("[FIREBASE] FIREBASE_DATABASE_URL is required (e.g. https://<project-id>-default-rtdb.<region>.firebasedatabase.app)")
	}

	opt, err := firebaseCredentialOption()
	if err != nil {
		log.Fatalf("[FIREBASE] %v", err)
	}

	app, err := firebase.NewApp(ctx, &firebase.Config{DatabaseURL: dbURL}, opt)
	if err != nil {
		log.Fatalf("[FIREBASE] app init error: %v", err)
	}

	client, err := app.Database(ctx)
	if err != nil {
		log.Fatalf("[FIREBASE] database client error: %v", err)
	}

	rtdb = client
	log.Printf("[FIREBASE] connected to %s", dbURL)
}

func firebaseCredentialOption() (option.ClientOption, error) {
	if credsJSON := os.Getenv("FIREBASE_CREDENTIALS_JSON"); credsJSON != "" {
		return option.WithAuthCredentialsJSON(option.ServiceAccount, []byte(credsJSON)), nil
	}

	credsFile := getEnv("FIREBASE_CREDENTIALS_FILE", "/app/firebase_credentials.json")
	if _, err := os.Stat(credsFile); err != nil {
		return nil, fmt.Errorf("no credentials found: set FIREBASE_CREDENTIALS_JSON or mount a service account file at %s", credsFile)
	}
	return option.WithAuthCredentialsFile(option.ServiceAccount, credsFile), nil
}

// ── JWT secret init ───────────────────────────────────────────────────────────

func initJWTSecret() {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		log.Fatal("[JWT] JWT_SECRET is required — set it in .env")
	}
	jwtSecret = []byte(secret)
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
		c.Set("role", claims["role"])
		c.Next()
	}
}

func requireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, _ := c.Get("role")
		if role != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin access required"})
			return
		}
		c.Next()
	}
}

// ── main ─────────────────────────────────────────────────────────────────────

func main() {
	probeInterfaces()
	initFirebase()
	initJWTSecret()

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
		auth.GET("/contests", getContests)
		auth.GET("/contests/:id/monitor", monitorTelemetry)
		auth.GET("/contests/:id/violations", getViolations)
		auth.GET("/contests/:id/ai-hits", getAIHits)
	}

	admin := r.Group("/", authMiddleware(), requireAdmin())
	{
		admin.POST("/host-contest", hostContest)
		admin.DELETE("/contests/:id", deleteContest)
	}

	port := getEnv("PORT", "8081")
	log.Printf("[GIN] listening on :%s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("[GIN] server error: %v", err)
	}
}

// resumeActiveSniffers restarts packet capture for contests still active after restart.
func resumeActiveSniffers() {
	var contests map[string]Contest
	if err := rtdb.NewRef("contests").Get(ctx, &contests); err != nil {
		log.Printf("[RESUME] could not load contests: %v", err)
		return
	}
	now := time.Now().UTC()
	for id, c := range contests {
		c.ID = id
		if now.Before(c.EndTime) {
			log.Printf("[RESUME] restarting sniffer for contest %s (%s)", c.ID, c.Name)
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
		log.Printf("[SNIFFER] contest %s: no usable interfaces — sniffing disabled", contest.ID)
		return
	}

	sctx, cancel := context.WithDeadline(context.Background(), contest.EndTime)

	snifferCancelsMu.Lock()
	if existing, ok := snifferCancels[contest.ID]; ok {
		existing()
	}
	snifferCancels[contest.ID] = cancel
	snifferCancelsMu.Unlock()

	log.Printf("[SNIFFER] contest %s: starting on %s", contest.ID, strings.Join(ifaces, ", "))
	for _, iface := range ifaces {
		go sniffInterface(sctx, iface, contest)
	}

	go func() {
		<-sctx.Done()
		snifferCancelsMu.Lock()
		delete(snifferCancels, contest.ID)
		snifferCancelsMu.Unlock()
		log.Printf("[SNIFFER] contest %s: all sniffers stopped", contest.ID)
	}()
}

func stopSniffer(contestID string) {
	snifferCancelsMu.Lock()
	defer snifferCancelsMu.Unlock()
	if cancel, ok := snifferCancels[contestID]; ok {
		cancel()
		delete(snifferCancels, contestID)
	}
}

func sniffInterface(sctx context.Context, iface string, contest Contest) {
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

	log.Printf("[SNIFFER] [%s] listening — contest %s (%s → %s BST)",
		iface, contest.ID,
		fmtBST(contest.StartTime),
		fmtBST(contest.EndTime),
	)

	src := gopacket.NewPacketSource(handle, handle.LinkType())
	src.DecodeOptions.Lazy = true
	src.DecodeOptions.NoCopy = true

	for {
		select {
		case <-sctx.Done():
			log.Printf("[SNIFFER] [%s] contest %s: context cancelled — stopping", iface, contest.ID)
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
	now := time.Now().UTC()
	contestRef := rtdb.NewRef("contests/" + contest.ID)

	// Traffic log — append-only history of every AI-domain packet seen.
	if _, err := contestRef.Child("traffic_logs").Push(ctx, TrafficLog{
		IP:        srcIP,
		AIService: domain,
		Timestamp: now,
	}); err != nil {
		log.Printf("[SNIFFER] traffic_logs insert error: %v", err)
	}

	// Deduped ai_hits — skip if the same IP+domain hit within the last 10 minutes.
	ahRef := contestRef.Child("ai_hits")
	var recent map[string]AIHit
	if err := ahRef.OrderByChild("ip").EqualTo(srcIP).Get(ctx, &recent); err != nil {
		log.Printf("[SNIFFER] ai_hits query error: %v", err)
	}
	dup := false
	for _, h := range recent {
		if h.Domain == domain && now.Sub(h.CreatedAt) < 10*time.Minute {
			dup = true
			break
		}
	}
	if !dup {
		if _, err := ahRef.Push(ctx, AIHit{IP: srcIP, Domain: domain, CreatedAt: now}); err != nil {
			log.Printf("[SNIFFER] ai_hits insert error: %v", err)
		}
	}

	// Flag the participant (only if they exist in the roster for this contest).
	pRef := contestRef.Child("participants").Child(sanitizeKey(srcIP))
	var existing Participant
	if err := pRef.Get(ctx, &existing); err != nil {
		log.Printf("[SNIFFER] participant lookup error: %v", err)
		return
	}
	if existing.IP == "" {
		log.Printf("[SNIFFER] domain=%-20s  src=%-18s  contest=%s  rows_updated=0 (unknown participant)", domain, srcIP, contest.ID)
		return
	}
	if err := pRef.Update(ctx, map[string]interface{}{
		"ai_violation": true,
		"last_seen":    now,
	}); err != nil {
		log.Printf("[SNIFFER] participant update error: %v", err)
		return
	}
	log.Printf("[SNIFFER] domain=%-20s  src=%-18s  contest=%s  rows_updated=1", domain, srcIP, contest.ID)
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func hostContest(c *gin.Context) {
	name := strings.TrimSpace(c.PostForm("contestName"))
	if name == "" {
		c.JSON(400, gin.H{"error": "contestName is required"})
		return
	}

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

	participants := make(map[string]Participant)
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
		participants[sanitizeKey(ip)] = Participant{
			TeamName:    teamName,
			IP:          ip,
			Members:     members,
			AIViolation: false,
		}
	}

	// Reserve a new contest ID (Firebase push key — chronologically sortable).
	newRef, err := rtdb.NewRef("contests").Push(ctx, nil)
	if err != nil {
		log.Printf("[HOST] create contest key error: %v", err)
		c.JSON(500, gin.H{"error": "failed to create contest"})
		return
	}
	contestID := newRef.Key

	endTime := startTime.Add(durationMin)
	contest := Contest{
		ID:        contestID,
		Name:      name,
		StartTime: startTime,
		EndTime:   endTime,
	}

	if err := newRef.Set(ctx, contest); err != nil {
		log.Printf("[HOST] write contest error: %v", err)
		c.JSON(500, gin.H{"error": "failed to create contest"})
		return
	}
	if len(participants) > 0 {
		if err := newRef.Child("participants").Set(ctx, participants); err != nil {
			log.Printf("[HOST] write participants error: %v", err)
			c.JSON(500, gin.H{"error": "failed to store participants"})
			return
		}
	}

	go startSniffer(contest)
	c.JSON(200, contest)
}

func deleteContest(c *gin.Context) {
	id := c.Param("id")
	var contest Contest
	if err := rtdb.NewRef("contests/"+id).Get(ctx, &contest); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if contest.Name == "" {
		c.JSON(404, gin.H{"error": "contest not found"})
		return
	}
	stopSniffer(id)
	if err := rtdb.NewRef("contests/" + id).Delete(ctx); err != nil {
		log.Printf("[DELETE] contest %s error: %v", id, err)
		c.JSON(500, gin.H{"error": "failed to delete contest"})
		return
	}
	c.JSON(200, gin.H{"status": "deleted"})
}

func monitorTelemetry(c *gin.Context) {
	id := c.Param("id")

	var contest Contest
	if err := rtdb.NewRef("contests/"+id).Get(ctx, &contest); err != nil || contest.Name == "" {
		c.JSON(404, gin.H{"error": "contest not found"})
		return
	}

	var participants map[string]Participant
	if err := rtdb.NewRef("contests/"+id+"/participants").Get(ctx, &participants); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	statuses := make([]TeamStatus, 0, len(participants))
	for _, p := range participants {
		s := TeamStatus{
			Name:      p.TeamName,
			Members:   p.Members,
			IP:        p.IP,
			IsWarning: p.AIViolation,
		}
		s.AIStatus = "CLEAN"
		if s.IsWarning {
			s.AIStatus = "AI SITE DETECTED"
		}
		s.LastSeen = "Never"
		if !p.LastSeen.IsZero() {
			s.LastSeen = fmtBST(p.LastSeen)
		}
		statuses = append(statuses, s)
	}
	c.JSON(200, statuses)
}

func getViolations(c *gin.Context) {
	id := c.Param("id")

	var contest Contest
	if err := rtdb.NewRef("contests/"+id).Get(ctx, &contest); err != nil || contest.Name == "" {
		c.JSON(404, gin.H{"error": "contest not found"})
		return
	}

	var participants map[string]Participant
	if err := rtdb.NewRef("contests/"+id+"/participants").Get(ctx, &participants); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	violations := make([]ViolationTeam, 0)
	for _, p := range participants {
		if !p.AIViolation {
			continue
		}
		detectedAt := "Unknown"
		if !p.LastSeen.IsZero() {
			detectedAt = fmtBST(p.LastSeen)
		}
		violations = append(violations, ViolationTeam{
			TeamName:   p.TeamName,
			Members:    splitMembers(p.Members),
			IP:         p.IP,
			DetectedAt: detectedAt,
			Domain:     latestDomainForIP(id, p.IP),
		})
	}
	c.JSON(200, violations)
}

func latestDomainForIP(contestID, ip string) string {
	var hits map[string]AIHit
	if err := rtdb.NewRef("contests/"+contestID+"/ai_hits").OrderByChild("ip").EqualTo(ip).Get(ctx, &hits); err != nil {
		log.Printf("[VIOLATIONS] ai_hits query error: %v", err)
		return ""
	}
	var latest AIHit
	for _, h := range hits {
		if h.CreatedAt.After(latest.CreatedAt) {
			latest = h
		}
	}
	return latest.Domain
}

func getAIHits(c *gin.Context) {
	id := c.Param("id")

	var contest Contest
	if err := rtdb.NewRef("contests/"+id).Get(ctx, &contest); err != nil || contest.Name == "" {
		c.JSON(404, gin.H{"error": "contest not found"})
		return
	}

	var hitsMap map[string]AIHit
	if err := rtdb.NewRef("contests/"+id+"/ai_hits").Get(ctx, &hitsMap); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	var participants map[string]Participant
	rtdb.NewRef("contests/" + id + "/participants").Get(ctx, &participants) // best-effort join

	byIP := make(map[string]Participant, len(participants))
	for _, p := range participants {
		byIP[p.IP] = p
	}

	ordered := make([]AIHit, 0, len(hitsMap))
	for _, h := range hitsMap {
		ordered = append(ordered, h)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].CreatedAt.After(ordered[j].CreatedAt) })

	hits := make([]AIHitDetail, 0, len(ordered))
	for _, h := range ordered {
		team := "Unknown"
		members := make([]string, 0)
		if p, ok := byIP[h.IP]; ok {
			team = p.TeamName
			members = splitMembers(p.Members)
		}
		hitTime := "Unknown"
		if !h.CreatedAt.IsZero() {
			hitTime = fmtBST(h.CreatedAt)
		}
		hits = append(hits, AIHitDetail{
			IP:       h.IP,
			TeamName: team,
			Members:  members,
			Domain:   h.Domain,
			HitTime:  hitTime,
		})
	}
	c.JSON(200, hits)
}

func getContests(c *gin.Context) {
	var contestsMap map[string]Contest
	if err := rtdb.NewRef("contests").Get(ctx, &contestsMap); err != nil {
		c.JSON(500, gin.H{"error": "failed to fetch contests"})
		return
	}
	list := make([]Contest, 0, len(contestsMap))
	for id, ct := range contestsMap {
		ct.ID = id
		list = append(list, ct)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].StartTime.After(list[j].StartTime) })
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

	emailKey := sanitizeKey(in.Email)
	emailRef := rtdb.NewRef("users_by_email/" + emailKey)

	var existingUID string
	if err := emailRef.Get(ctx, &existingUID); err == nil && existingUID != "" {
		c.JSON(409, gin.H{"error": "email already registered"})
		return
	}

	pw, err := bcrypt.GenerateFromPassword([]byte(in.Password), 10)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to hash password"})
		return
	}

	usersRef := rtdb.NewRef("users")
	newRef, err := usersRef.Push(ctx, nil)
	if err != nil {
		log.Printf("[REGISTER] create user key error: %v", err)
		c.JSON(500, gin.H{"error": "failed to create user"})
		return
	}

	user := User{
		ID:       newRef.Key,
		Name:     strings.TrimSpace(in.FirstName + " " + in.LastName),
		Email:    in.Email,
		Password: string(pw),
		Role:     "user",
	}
	if err := newRef.Set(ctx, user); err != nil {
		log.Printf("[REGISTER] write user error: %v", err)
		c.JSON(500, gin.H{"error": "failed to create user"})
		return
	}
	if err := emailRef.Set(ctx, newRef.Key); err != nil {
		log.Printf("[REGISTER] write email index error: %v", err)
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

	emailKey := sanitizeKey(strings.TrimSpace(strings.ToLower(in.Email)))

	var uid string
	if err := rtdb.NewRef("users_by_email/"+emailKey).Get(ctx, &uid); err != nil || uid == "" {
		c.JSON(401, gin.H{"error": "invalid email or password"})
		return
	}

	var u User
	if err := rtdb.NewRef("users/"+uid).Get(ctx, &u); err == nil && u.Password != "" {
		if err := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(in.Password)); err == nil {
			role := u.Role
			if role == "" {
				role = "user"
			}
			token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
				"user_id": u.ID,
				"role":    role,
				"exp":     time.Now().Add(24 * time.Hour).Unix(),
			})
			t, signErr := token.SignedString(jwtSecret)
			if signErr != nil {
				c.JSON(500, gin.H{"error": "failed to issue token"})
				return
			}
			resp := u.sanitizedForClient()
			resp.Role = role
			c.JSON(200, gin.H{"token": t, "user": resp})
			return
		}
	}
	c.JSON(401, gin.H{"error": "invalid email or password"})
}

var _ = nowBST