package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/mail"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

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

// fmtBST formats a time as HH:MM:SS in BST.
func fmtBST(t time.Time) string { return t.In(bst).Format("15:04:05") }

// ── Timeouts ──────────────────────────────────────────────────────────────────
// dbOpTimeout bounds every Firebase RTDB call. Without this, a blocked or
// throttled network (captive portals, restrictive wifi, DNS failures on the
// path to Google's servers) makes requests hang indefinitely with no
// feedback to the client. With it, requests fail fast with a clear error.
const dbOpTimeout = 8 * time.Second

// withDBTimeout returns a context bounded by dbOpTimeout, rooted in the
// application's background context. Callers must defer the returned cancel.
func withDBTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), dbOpTimeout)
}

// isDBUnreachable is a small helper so handlers can respond with a
// consistent, honest status code (503, not 500) when the problem is
// connectivity to Firebase rather than a bug in the request itself.
func dbUnreachableJSON(c *gin.Context, action string, err error) {
	log.Printf("[DB] %s error: %v", action, err)
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"error": "cannot reach database — check the server's internet connection",
	})
}

// ── Models ────────────────────────────────────────────────────────────────────
type User struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password,omitempty"`
	Role     string `json:"role"`
}

// sanitizedForClient returns a copy of the user with the password hash removed.
func (u User) sanitizedForClient() User {
	u.Password = ""
	return u
}

type Contest struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	StartTime        time.Time `json:"start_time"`
	EndTime          time.Time `json:"end_time"`
	CaptureConsented bool      `json:"capture_consented"`
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
		"chatgpt.com", "openai.com", "gemini.google.com", "grok.com", "claude.ai",
		"anthropic.com", "perplexity.ai", "deepseek.com", "copilot.microsoft.com",
		"poe.com", "phind.com", "blackbox.ai", "you.com", "aistudio.google.com",
	}

	snifferCancels   = make(map[string]context.CancelFunc)
	snifferCancelsMu sync.Mutex
	hitQueue         chan hitEvent
	recentHits       = make(map[string]time.Time)
	recentHitsMu     sync.Mutex
	registrationMu   sync.Mutex
	publicLimiter    = newRateLimiter()
)

type hitEvent struct {
	contest Contest
	srcIP   string
	domain  string
}

type rateEntry struct {
	count int
	reset time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	entries map[string]rateEntry
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{entries: make(map[string]rateEntry)}
}

func (l *rateLimiter) allow(key string, limit int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	entry := l.entries[key]
	if entry.reset.IsZero() || now.After(entry.reset) {
		entry = rateEntry{reset: now.Add(window)}
	}
	if entry.count >= limit {
		l.entries[key] = entry
		return false
	}
	entry.count++
	l.entries[key] = entry
	return true
}

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
	candidates := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(":/='\";,()[]", r)
	})
	for _, candidate := range candidates {
		candidate = strings.Trim(candidate, ".")
		for _, domain := range aiDomains {
			if candidate == domain || strings.HasSuffix(candidate, "."+domain) {
				return domain
			}
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
// RTDB forbids '.', '#', '$', '[', ']', '/' in keys.
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
	if len(secret) < 32 {
		log.Fatal("[JWT] JWT_SECRET must be at least 32 characters")
	}
	jwtSecret = []byte(secret)
}

// ── JWT Middleware ────────────────────────────────────────────────────────────

func authMiddleware() gin.HandlerFunc {
	parser := jwt.NewParser(jwt.WithValidMethods([]string{"HS256"}))
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authorization header required"})
			return
		}
		tokenStr := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		token, err := parser.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
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
		userID, userOK := claims["user_id"].(string)
		role, roleOK := claims["role"].(string)
		if !userOK || userID == "" || !roleOK || role == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token claims"})
			return
		}
		c.Set("user_id", userID)
		c.Set("role", role)
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

func corsMiddleware() gin.HandlerFunc {
	allowed := make(map[string]struct{})
	for _, origin := range strings.Split(getEnv("ALLOWED_ORIGINS", "http://localhost:3000,http://127.0.0.1:3000"), ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			allowed[origin] = struct{}{}
		}
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			if _, ok := allowed[origin]; ok {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Vary", "Origin")
			}
		}
		if c.Request.Method == http.MethodOptions {
			if origin == "" {
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
			if _, ok := allowed[origin]; !ok {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "origin not allowed; configure this LAN origin or use the frontend proxy"})
				return
			}
			c.Header("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func requestBodyLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}

func publicRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodPost && (c.Request.URL.Path == "/login" || c.Request.URL.Path == "/register") {
			ip := normalizeIP(c.Request.RemoteAddr)
			if !publicLimiter.allow(ip+":"+c.Request.URL.Path, 10, time.Minute) {
				c.Header("Retry-After", "60")
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "too many attempts; try again later"})
				return
			}
		}
		c.Next()
	}
}

// ── main ─────────────────────────────────────────────────────────────────────

func main() {
	initFirebase()
	initJWTSecret()
	startHitRecorder()
	if captureEnabled() {
		probeInterfaces()
	}
	resumeActiveSniffers()

	gin.SetMode(getEnv("GIN_MODE", "release"))
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), corsMiddleware(), requestBodyLimit(10<<20), publicRateLimit())

	r.POST("/login", login)
	r.POST("/register", register)

	// Health is intentionally unauthenticated and contains no user or contest data.
	r.GET("/health", healthCheck)

	auth := r.Group("/", authMiddleware())
	auth.GET("/contests", getContests)
	auth.GET("/contests/:id/public-violations", getPublicViolations)

	// Detailed telemetry and roster data are administrator-only because they
	// contain participant identifiers and network-derived signals.
	admin := r.Group("/", authMiddleware(), requireAdmin())
	{
		admin.POST("/host-contest", hostContest)
		admin.DELETE("/contests/:id", deleteContest)
		admin.GET("/contests/:id/monitor", monitorTelemetry)
		admin.GET("/contests/:id/violations", getViolations)
		admin.GET("/contests/:id/ai-hits", getAIHits)
	}

	port := getEnv("PORT", "8081")
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	log.Printf("[HTTP] listening on %s (LAN access is available through the host address)", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[HTTP] server error: %v", err)
	}
}

func healthCheck(c *gin.Context) {
	dbCtx, cancel := withDBTimeout()
	defer cancel()

	var probe string
	if err := rtdb.NewRef("__health").Get(dbCtx, &probe); err != nil {
		log.Printf("[HEALTH] db unreachable: %v", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "db_unreachable",
			"error":  err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "capture_enabled": captureEnabled()})
}

// resumeActiveSniffers restarts packet capture for contests still active after restart.
func resumeActiveSniffers() {
	if !captureEnabled() {
		log.Println("[RESUME] packet capture disabled; no sniffers started")
		return
	}
	dbCtx, cancel := withDBTimeout()
	defer cancel()

	var contests map[string]Contest
	if err := rtdb.NewRef("contests").Get(dbCtx, &contests); err != nil {
		log.Printf("[RESUME] could not load contests (db unreachable?): %v", err)
		return
	}
	now := time.Now().UTC()
	for id, c := range contests {
		c.ID = id
		if c.CaptureConsented && now.Before(c.EndTime) {
			log.Printf("[RESUME] restarting consented sniffer for contest %s (%s)", c.ID, c.Name)
			startSniffer(c)
		}
	}
}

// ── Interface probing ─────────────────────────────────────────────────────────

func captureEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(getEnv("CAPTURE_ENABLED", "false")), "true")
}

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
	if !captureEnabled() || !contest.CaptureConsented {
		log.Printf("[SNIFFER] contest %s: capture disabled or consent not recorded", contest.ID)
		return
	}
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

func startHitRecorder() {
	if hitQueue != nil {
		return
	}
	hitQueue = make(chan hitEvent, 1024)
	for worker := 0; worker < 2; worker++ {
		go func() {
			for event := range hitQueue {
				recordHitSync(event.contest, event.srcIP, event.domain)
			}
		}()
	}
}

// recordHit is intentionally non-blocking so a Firebase outage cannot stop
// packet capture. It records only a domain/IP signal, never packet payloads.
func recordHit(contest Contest, srcIP, domain string) {
	if !captureEnabled() || !contest.CaptureConsented || hitQueue == nil {
		return
	}
	key := contest.ID + "|" + srcIP + "|" + domain
	now := time.Now().UTC()
	recentHitsMu.Lock()
	if previous, ok := recentHits[key]; ok && now.Sub(previous) < 10*time.Minute {
		recentHitsMu.Unlock()
		return
	}
	recentHits[key] = now
	if len(recentHits) > 10000 {
		for k, t := range recentHits {
			if now.Sub(t) > 10*time.Minute {
				delete(recentHits, k)
			}
		}
	}
	recentHitsMu.Unlock()

	select {
	case hitQueue <- hitEvent{contest: contest, srcIP: srcIP, domain: domain}:
	default:
		log.Printf("[SNIFFER] recorder queue full; dropping metadata signal for %s", srcIP)
	}
}

func recordHitSync(contest Contest, srcIP, domain string) {
	dbCtx, cancel := withDBTimeout()
	defer cancel()

	now := time.Now().UTC()
	contestRef := rtdb.NewRef("contests/" + contest.ID)
	if _, err := contestRef.Child("traffic_logs").Push(dbCtx, TrafficLog{
		IP:        srcIP,
		AIService: domain,
		Timestamp: now,
	}); err != nil {
		log.Printf("[SNIFFER] traffic_logs insert error: %v", err)
	}
	if _, err := contestRef.Child("ai_hits").Push(dbCtx, AIHit{
		IP: srcIP, Domain: domain, CreatedAt: now,
	}); err != nil {
		log.Printf("[SNIFFER] ai_hits insert error: %v", err)
	}

	pRef := contestRef.Child("participants").Child(sanitizeKey(srcIP))
	var existing Participant
	if err := pRef.Get(dbCtx, &existing); err != nil {
		log.Printf("[SNIFFER] participant lookup error: %v", err)
		return
	}
	if existing.IP == "" {
		log.Printf("[SNIFFER] domain=%-20s  src=%-18s  contest=%s  rows_updated=0 (unknown participant)", domain, srcIP, contest.ID)
		return
	}
	if err := pRef.Update(dbCtx, map[string]interface{}{
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
	if name == "" || len([]rune(name)) > 120 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "contestName is required and must be at most 120 characters"})
		return
	}
	if strings.ToLower(strings.TrimSpace(c.PostForm("captureConsent"))) != "true" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "explicit participant-consent acknowledgement is required before capture can start"})
		return
	}
	if !captureEnabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "packet capture is disabled; set CAPTURE_ENABLED=true only on the authorized contest host"})
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

	durationMinutes, err := strconv.Atoi(strings.TrimSpace(c.PostForm("duration")))
	if err != nil || durationMinutes < 1 || durationMinutes > 24*60 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid duration (1–1440 whole minutes expected)"})
		return
	}
	durationMin := time.Duration(durationMinutes) * time.Minute

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "csv file required"})
		return
	}
	if file.Size <= 0 || file.Size > 5<<20 || !strings.HasSuffix(strings.ToLower(file.Filename), ".csv") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "uploaded CSV must be non-empty, have a .csv extension, and be at most 5 MiB"})
		return
	}

	f, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to open uploaded file"})
		return
	}
	defer f.Close()

	reader := csv.NewReader(io.LimitReader(f, 5<<20))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil || len(records) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "CSV must contain a header and at least one participant row"})
		return
	}
	if len(records[0]) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "CSV header must contain team_name and ip"})
		return
	}
	headerTeam := strings.TrimPrefix(strings.TrimSpace(records[0][0]), "\ufeff")
	if !strings.EqualFold(headerTeam, "team_name") || !strings.EqualFold(strings.TrimSpace(records[0][1]), "ip") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "CSV header must begin with team_name, ip"})
		return
	}

	participants := make(map[string]Participant, len(records)-1)
	for rowNumber := 1; rowNumber < len(records); rowNumber++ {
		row := records[rowNumber]
		if len(row) < 2 {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("CSV row %d must contain team_name and ip", rowNumber+1)})
			return
		}
		teamName := strings.TrimSpace(row[0])
		if teamName == "" || len([]rune(teamName)) > 120 {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("CSV row %d has an invalid team name", rowNumber+1)})
			return
		}
		ipValue := strings.TrimSpace(row[1])
		parsedIP := net.ParseIP(ipValue)
		if parsedIP == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("CSV row %d has an invalid IP address", rowNumber+1)})
			return
		}
		ip := normalizeIP(parsedIP.String())
		key := sanitizeKey(ip)
		if _, exists := participants[key]; exists {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("CSV contains duplicate IP address %s", ip)})
			return
		}
		memberValues := make([]string, 0, len(row)-2)
		for _, member := range row[2:] {
			member = strings.TrimSpace(member)
			if member != "" {
				memberValues = append(memberValues, member)
			}
		}
		members := strings.Join(memberValues, ", ")
		if len([]rune(members)) > 1000 {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("CSV row %d has too much member data", rowNumber+1)})
			return
		}
		participants[key] = Participant{
			TeamName: teamName,
			IP:       ip,
			Members:  members,
		}
	}

	dbCtx, cancel := withDBTimeout()
	defer cancel()

	// Reserve a new contest ID (Firebase push key — chronologically sortable).
	newRef, err := rtdb.NewRef("contests").Push(dbCtx, nil)
	if err != nil {
		dbUnreachableJSON(c, "create contest key", err)
		return
	}
	contestID := newRef.Key

	endTime := startTime.Add(durationMin)
	contest := Contest{
		ID:               contestID,
		Name:             name,
		StartTime:        startTime,
		EndTime:          endTime,
		CaptureConsented: true,
	}

	if err := newRef.Set(dbCtx, contest); err != nil {
		dbUnreachableJSON(c, "write contest", err)
		return
	}
	if len(participants) > 0 {
		if err := newRef.Child("participants").Set(dbCtx, participants); err != nil {
			_ = newRef.Delete(dbCtx)
			dbUnreachableJSON(c, "write participants", err)
			return
		}
	}

	if captureEnabled() {
		go startSniffer(contest)
	}
	c.JSON(http.StatusCreated, gin.H{
		"contest":         contest,
		"capture_enabled": captureEnabled(),
		"message":         "contest created; packet metadata capture is active only when CAPTURE_ENABLED=true and this consent acknowledgement is recorded",
	})
}

func deleteContest(c *gin.Context) {
	id := c.Param("id")

	dbCtx, cancel := withDBTimeout()
	defer cancel()

	var contest Contest
	if err := rtdb.NewRef("contests/"+id).Get(dbCtx, &contest); err != nil {
		dbUnreachableJSON(c, "fetch contest", err)
		return
	}
	if contest.Name == "" {
		c.JSON(404, gin.H{"error": "contest not found"})
		return
	}
	stopSniffer(id)
	if err := rtdb.NewRef("contests/" + id).Delete(dbCtx); err != nil {
		dbUnreachableJSON(c, "delete contest", err)
		return
	}
	c.JSON(200, gin.H{"status": "deleted"})
}

func monitorTelemetry(c *gin.Context) {
	id := c.Param("id")

	dbCtx, cancel := withDBTimeout()
	defer cancel()

	var contest Contest
	if err := rtdb.NewRef("contests/"+id).Get(dbCtx, &contest); err != nil {
		dbUnreachableJSON(c, "fetch contest", err)
		return
	}
	if contest.Name == "" {
		c.JSON(404, gin.H{"error": "contest not found"})
		return
	}

	var participants map[string]Participant
	if err := rtdb.NewRef("contests/"+id+"/participants").Get(dbCtx, &participants); err != nil {
		dbUnreachableJSON(c, "fetch participants", err)
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

	dbCtx, cancel := withDBTimeout()
	defer cancel()

	var contest Contest
	if err := rtdb.NewRef("contests/"+id).Get(dbCtx, &contest); err != nil {
		dbUnreachableJSON(c, "fetch contest", err)
		return
	}
	if contest.Name == "" {
		c.JSON(404, gin.H{"error": "contest not found"})
		return
	}

	var participants map[string]Participant
	if err := rtdb.NewRef("contests/"+id+"/participants").Get(dbCtx, &participants); err != nil {
		dbUnreachableJSON(c, "fetch participants", err)
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
			Domain:     latestDomainForIP(dbCtx, id, p.IP),
		})
	}
	c.JSON(200, violations)
}

type PublicViolation struct {
	TeamName   string `json:"team_name"`
	DetectedAt string `json:"detected_at"`
	Domain     string `json:"domain"`
}

func getPublicViolations(c *gin.Context) {
	id := c.Param("id")
	dbCtx, cancel := withDBTimeout()
	defer cancel()

	var contest Contest
	if err := rtdb.NewRef("contests/"+id).Get(dbCtx, &contest); err != nil {
		dbUnreachableJSON(c, "fetch contest", err)
		return
	}
	if contest.Name == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "contest not found"})
		return
	}

	var participants map[string]Participant
	if err := rtdb.NewRef("contests/"+id+"/participants").Get(dbCtx, &participants); err != nil {
		dbUnreachableJSON(c, "fetch participants", err)
		return
	}

	violations := make([]PublicViolation, 0)
	for _, participant := range participants {
		if !participant.AIViolation {
			continue
		}
		detectedAt := "Unknown"
		if !participant.LastSeen.IsZero() {
			detectedAt = fmtBST(participant.LastSeen)
		}
		violations = append(violations, PublicViolation{
			TeamName:   participant.TeamName,
			DetectedAt: detectedAt,
			Domain:     latestDomainForIP(dbCtx, id, participant.IP),
		})
	}
	sort.Slice(violations, func(i, j int) bool {
		return violations[i].TeamName < violations[j].TeamName
	})
	c.JSON(http.StatusOK, violations)
}

func latestDomainForIP(dbCtx context.Context, contestID, ip string) string {
	var hits map[string]AIHit
	if err := rtdb.NewRef("contests/"+contestID+"/ai_hits").OrderByChild("ip").EqualTo(ip).Get(dbCtx, &hits); err != nil {
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

	dbCtx, cancel := withDBTimeout()
	defer cancel()

	var contest Contest
	if err := rtdb.NewRef("contests/"+id).Get(dbCtx, &contest); err != nil {
		dbUnreachableJSON(c, "fetch contest", err)
		return
	}
	if contest.Name == "" {
		c.JSON(404, gin.H{"error": "contest not found"})
		return
	}

	var hitsMap map[string]AIHit
	if err := rtdb.NewRef("contests/"+id+"/ai_hits").Get(dbCtx, &hitsMap); err != nil {
		dbUnreachableJSON(c, "fetch ai_hits", err)
		return
	}
	var participants map[string]Participant
	if err := rtdb.NewRef("contests/"+id+"/participants").Get(dbCtx, &participants); err != nil {
		// best-effort join — log but don't fail the whole request over it
		log.Printf("[AI-HITS] participants fetch error: %v", err)
	}

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
	dbCtx, cancel := withDBTimeout()
	defer cancel()

	var contestsMap map[string]Contest
	if err := rtdb.NewRef("contests").Get(dbCtx, &contestsMap); err != nil {
		dbUnreachableJSON(c, "fetch contests", err)
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

func emailIndexKey(email string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(email))
}

func validateEmail(email string) bool {
	parsed, err := mail.ParseAddress(email)
	return err == nil && parsed.Address == email && len(email) <= 254
}

func register(c *gin.Context) {
	var in struct {
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
		Email     string `json:"email"`
		Password  string `json:"password"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	in.FirstName = strings.TrimSpace(in.FirstName)
	in.LastName = strings.TrimSpace(in.LastName)
	in.Email = strings.TrimSpace(strings.ToLower(in.Email))

	if in.FirstName == "" || len([]rune(in.FirstName)) > 80 || len([]rune(in.LastName)) > 80 || !validateEmail(in.Email) || in.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid name, email, and password are required"})
		return
	}
	if len(in.Password) < 10 || len(in.Password) > 200 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password must be between 10 and 200 characters"})
		return
	}

	registrationMu.Lock()
	defer registrationMu.Unlock()
	dbCtx, cancel := withDBTimeout()
	defer cancel()

	emailRef := rtdb.NewRef("users_by_email/" + emailIndexKey(in.Email))
	var existingUID string
	if err := emailRef.Get(dbCtx, &existingUID); err != nil {
		dbUnreachableJSON(c, "check existing email", err)
		return
	}
	if existingUID != "" {
		c.JSON(http.StatusConflict, gin.H{"error": "email already registered"})
		return
	}

	pw, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("[REGISTER] password hash error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
		return
	}
	newRef, err := rtdb.NewRef("users").Push(dbCtx, nil)
	if err != nil {
		dbUnreachableJSON(c, "create user key", err)
		return
	}
	user := User{ID: newRef.Key, Name: strings.TrimSpace(in.FirstName + " " + in.LastName), Email: in.Email, Password: string(pw), Role: "user"}
	if err := newRef.Set(dbCtx, user); err != nil {
		dbUnreachableJSON(c, "write user", err)
		return
	}
	if err := emailRef.Set(dbCtx, newRef.Key); err != nil {
		dbUnreachableJSON(c, "write email index", err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "success"})
}

func login(c *gin.Context) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	in.Email = strings.TrimSpace(strings.ToLower(in.Email))
	if !validateEmail(in.Email) || in.Password == "" || len(in.Password) > 200 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		return
	}

	dbCtx, cancel := withDBTimeout()
	defer cancel()

	var uid string
	if err := rtdb.NewRef("users_by_email/"+emailIndexKey(in.Email)).Get(dbCtx, &uid); err != nil {
		dbUnreachableJSON(c, "lookup email index", err)
		return
	}
	if uid == "" {
		// Backward-compatible lookup for accounts created before the encoded index.
		if err := rtdb.NewRef("users_by_email/"+sanitizeKey(in.Email)).Get(dbCtx, &uid); err != nil {
			dbUnreachableJSON(c, "lookup legacy email index", err)
			return
		}
	}
	if uid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		return
	}

	var u User
	if err := rtdb.NewRef("users/"+uid).Get(dbCtx, &u); err != nil {
		dbUnreachableJSON(c, "fetch user", err)
		return
	}
	if u.Password == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(in.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		return
	}

	role := u.Role
	if role == "" {
		role = "user"
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": u.ID,
		"role":    role,
		"iat":     time.Now().Unix(),
		"exp":     time.Now().Add(12 * time.Hour).Unix(),
	})
	t, signErr := token.SignedString(jwtSecret)
	if signErr != nil {
		log.Printf("[LOGIN] token sign error: %v", signErr)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to issue token"})
		return
	}

	resp := u.sanitizedForClient()
	resp.Role = role
	c.JSON(200, gin.H{"token": t, "user": resp})
}
