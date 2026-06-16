package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/quic-go/quic-go"

	"gaccel-node/internal/protocol"
)

var version = "dev"

type app struct {
	configPath string
}

type appConfig struct {
	NodeAddr       string `json:"node_addr"`
	ALPN           string `json:"alpn"`
	SNI            string `json:"sni"`
	Insecure       bool   `json:"insecure"`
	TokenAPIURL    string `json:"token_api_url"`
	TokenAPIKey    string `json:"token_api_key"`
	UserID         string `json:"user_id"`
	DeviceID       string `json:"device_id"`
	TTLSeconds     int64  `json:"ttl_seconds"`
	Token          string `json:"token"`
	TargetHost     string `json:"target_host"`
	TargetPort     int    `json:"target_port"`
	HTTPPath       string `json:"http_path"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	ClientID       string `json:"client_id"`
	ClientVersion  string `json:"client_version"`
	ClientPlatform string `json:"client_platform"`
}

type configResponse struct {
	Config appConfig `json:"config"`
	Path   string    `json:"path"`
}

type tokenIssueRequest struct {
	UserID     string `json:"user_id"`
	DeviceID   string `json:"device_id,omitempty"`
	TTLSeconds int64  `json:"ttl_seconds,omitempty"`
	AllowTCP   *bool  `json:"allow_tcp,omitempty"`
	AllowUDP   *bool  `json:"allow_udp,omitempty"`
}

type tokenIssueResponse struct {
	Token            string    `json:"token"`
	TokenType        string    `json:"token_type"`
	UserID           string    `json:"user_id"`
	DeviceID         string    `json:"device_id,omitempty"`
	ExpiresAt        time.Time `json:"expires_at"`
	ExpiresInSeconds int64     `json:"expires_in_seconds"`
}

type testResult struct {
	OK          bool     `json:"ok"`
	Status      string   `json:"status,omitempty"`
	LatencyMS   int64    `json:"latency_ms,omitempty"`
	ContentType string   `json:"content_type,omitempty"`
	Location    string   `json:"location,omitempty"`
	BodyPreview string   `json:"body_preview,omitempty"`
	Logs        []string `json:"logs"`
	Error       string   `json:"error,omitempty"`
}

type lineCodec struct {
	reader *bufio.Reader
	writer io.Writer
}

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	listen := flag.String("listen", "127.0.0.1:0", "local web console listen address")
	configPath := flag.String("config", "", "config file path")
	noOpen := flag.Bool("no-open", false, "do not open browser automatically")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	path := strings.TrimSpace(*configPath)
	if path == "" {
		path = defaultConfigPath()
	}
	application := &app{configPath: path}

	mux := http.NewServeMux()
	mux.HandleFunc("/", application.handleIndex)
	mux.HandleFunc("/api/config", application.handleConfig)
	mux.HandleFunc("/api/token", application.handleToken)
	mux.HandleFunc("/api/test", application.handleTest)

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintf(os.Stderr, "listen failed: %v\n", err)
		os.Exit(1)
	}
	defer ln.Close()

	url := "http://" + ln.Addr().String()
	fmt.Printf("gaccel Steam demo %s\n", version)
	fmt.Printf("web console: %s\n", url)
	fmt.Printf("config file: %s\n", path)
	if !*noOpen {
		_ = openBrowser(url)
	}

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(os.Stderr, "server stopped: %v\n", err)
		os.Exit(1)
	}
}

func (a *app) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, indexHTML)
}

func (a *app) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, err := a.loadConfig()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, configResponse{Config: cfg, Path: a.configPath})
	case http.MethodPost:
		var cfg appConfig
		if err := decodeJSON(r, &cfg); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		cfg = normalizeConfig(cfg)
		if err := a.saveConfig(cfg); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, configResponse{Config: cfg, Path: a.configPath})
	default:
		w.Header().Set("Allow", "GET, POST")
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
}

func (a *app) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	var cfg appConfig
	if err := decodeJSON(r, &cfg); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cfg = normalizeConfig(cfg)
	token, err := requestToken(r.Context(), cfg)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cfg.Token = token.Token
	writeJSON(w, http.StatusOK, map[string]any{
		"config":     cfg,
		"token":      token.Token,
		"expires_at": token.ExpiresAt,
		"expires_in": token.ExpiresInSeconds,
	})
}

func (a *app) handleTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	var cfg appConfig
	if err := decodeJSON(r, &cfg); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cfg = normalizeConfig(cfg)
	result := runSteamTest(r.Context(), cfg)
	status := http.StatusOK
	if !result.OK {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, result)
}

func (a *app) loadConfig() (appConfig, error) {
	cfg := defaultConfig()
	data, err := os.ReadFile(a.configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return appConfig{}, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return appConfig{}, err
	}
	return normalizeConfig(cfg), nil
}

func (a *app) saveConfig(cfg appConfig) error {
	if err := os.MkdirAll(filepath.Dir(a.configPath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(normalizeConfig(cfg), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(a.configPath, append(data, '\n'), 0o600)
}

func defaultConfig() appConfig {
	return appConfig{
		NodeAddr:       "195.245.242.9:5555",
		ALPN:           "gaccel/1",
		Insecure:       true,
		TokenAPIURL:    "http://127.0.0.1:8088/token",
		UserID:         "dev",
		DeviceID:       "steam-demo",
		TTLSeconds:     3600,
		TargetHost:     "steamcommunity.com",
		TargetPort:     443,
		HTTPPath:       "/",
		TimeoutSeconds: 30,
		ClientID:       "steam-demo",
		ClientVersion:  version,
		ClientPlatform: runtime.GOOS + "/" + runtime.GOARCH,
	}
}

func normalizeConfig(cfg appConfig) appConfig {
	def := defaultConfig()
	if strings.TrimSpace(cfg.NodeAddr) == "" {
		cfg.NodeAddr = def.NodeAddr
	}
	if strings.TrimSpace(cfg.ALPN) == "" {
		cfg.ALPN = def.ALPN
	}
	if strings.TrimSpace(cfg.TokenAPIURL) == "" {
		cfg.TokenAPIURL = def.TokenAPIURL
	}
	if strings.TrimSpace(cfg.UserID) == "" {
		cfg.UserID = def.UserID
	}
	if strings.TrimSpace(cfg.DeviceID) == "" {
		cfg.DeviceID = def.DeviceID
	}
	if cfg.TTLSeconds <= 0 {
		cfg.TTLSeconds = def.TTLSeconds
	}
	if strings.TrimSpace(cfg.TargetHost) == "" {
		cfg.TargetHost = def.TargetHost
	}
	if cfg.TargetPort <= 0 {
		cfg.TargetPort = def.TargetPort
	}
	if strings.TrimSpace(cfg.HTTPPath) == "" {
		cfg.HTTPPath = def.HTTPPath
	}
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = def.TimeoutSeconds
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		cfg.ClientID = def.ClientID
	}
	if strings.TrimSpace(cfg.ClientVersion) == "" {
		cfg.ClientVersion = def.ClientVersion
	}
	if strings.TrimSpace(cfg.ClientPlatform) == "" {
		cfg.ClientPlatform = def.ClientPlatform
	}
	cfg.NodeAddr = strings.TrimSpace(cfg.NodeAddr)
	cfg.ALPN = strings.TrimSpace(cfg.ALPN)
	cfg.SNI = strings.TrimSpace(cfg.SNI)
	cfg.TokenAPIURL = strings.TrimSpace(cfg.TokenAPIURL)
	cfg.TokenAPIKey = strings.TrimSpace(cfg.TokenAPIKey)
	cfg.UserID = strings.TrimSpace(cfg.UserID)
	cfg.DeviceID = strings.TrimSpace(cfg.DeviceID)
	cfg.Token = strings.TrimSpace(cfg.Token)
	cfg.TargetHost = strings.TrimSpace(cfg.TargetHost)
	cfg.HTTPPath = strings.TrimSpace(cfg.HTTPPath)
	if !strings.HasPrefix(cfg.HTTPPath, "/") {
		cfg.HTTPPath = "/" + cfg.HTTPPath
	}
	cfg.ClientID = strings.TrimSpace(cfg.ClientID)
	cfg.ClientVersion = strings.TrimSpace(cfg.ClientVersion)
	cfg.ClientPlatform = strings.TrimSpace(cfg.ClientPlatform)
	return cfg
}

func requestToken(ctx context.Context, cfg appConfig) (*tokenIssueResponse, error) {
	if cfg.TokenAPIKey == "" {
		return nil, errors.New("token api key is required")
	}
	allowTCP := true
	allowUDP := true
	body, err := json.Marshal(tokenIssueRequest{
		UserID:     cfg.UserID,
		DeviceID:   cfg.DeviceID,
		TTLSeconds: cfg.TTLSeconds,
		AllowTCP:   &allowTCP,
		AllowUDP:   &allowUDP,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenAPIURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.TokenAPIKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("token api returned %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	var token tokenIssueResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, err
	}
	if token.Token == "" {
		return nil, errors.New("token api returned an empty token")
	}
	return &token, nil
}

func runSteamTest(parent context.Context, cfg appConfig) testResult {
	result := testResult{Logs: make([]string, 0, 8)}
	logf := func(format string, args ...any) {
		result.Logs = append(result.Logs, fmt.Sprintf(format, args...))
	}

	if !looksLikeJWT(cfg.Token) {
		result.Error = "JWT token is required. Use the token returned by POST /token, not the token API key."
		logf(result.Error)
		return result
	}

	ctx, cancel := context.WithTimeout(parent, time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer cancel()

	latency, status, contentType, location, preview, err := probeHTTPS(ctx, cfg, logf)
	if err != nil {
		result.Error = err.Error()
		logf("failed: %s", err)
		return result
	}
	result.OK = true
	result.Status = status
	result.LatencyMS = latency.Milliseconds()
	result.ContentType = contentType
	result.Location = location
	result.BodyPreview = preview
	logf("done: %s in %s", status, latency)
	return result
}

func probeHTTPS(ctx context.Context, cfg appConfig, logf func(string, ...any)) (time.Duration, string, string, string, string, error) {
	tlsConfig := &tls.Config{
		NextProtos:         []string{cfg.ALPN},
		InsecureSkipVerify: cfg.Insecure,
		ServerName:         serverName(cfg.NodeAddr, cfg.SNI),
		MinVersion:         tls.VersionTLS13,
	}
	conn, err := quic.DialAddr(ctx, cfg.NodeAddr, tlsConfig, &quic.Config{EnableDatagrams: true})
	if err != nil {
		return 0, "", "", "", "", err
	}
	defer conn.CloseWithError(0, "steam demo done")
	logf("quic connected: %s", cfg.NodeAddr)

	control, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return 0, "", "", "", "", err
	}
	controlCodec := newLineCodec(control)
	if err := authenticate(ctx, controlCodec, cfg, logf); err != nil {
		return 0, "", "", "", "", err
	}

	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return 0, "", "", "", "", err
	}
	codec := newLineCodec(stream)
	if err := codec.Write(protocol.Message{
		Type:       protocol.MessageOpenTCP,
		TargetHost: cfg.TargetHost,
		TargetPort: cfg.TargetPort,
	}); err != nil {
		return 0, "", "", "", "", err
	}
	msg, err := readWithContext(ctx, codec)
	if err != nil {
		return 0, "", "", "", "", err
	}
	if msg.Type != protocol.MessageOpenTCP {
		return 0, "", "", "", "", messageError(msg, "open tcp failed")
	}
	logf("tcp flow opened: flow_id=%d target=%s:%d", msg.FlowID, cfg.TargetHost, cfg.TargetPort)

	rawConn := &streamConn{
		stream: stream,
		reader: codec.reader,
		local:  relayAddr("gaccel-steam-demo"),
		remote: relayAddr(fmt.Sprintf("%s:%d", cfg.TargetHost, cfg.TargetPort)),
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = rawConn.SetDeadline(deadline)
	}

	start := time.Now()
	tlsConn := tls.Client(rawConn, &tls.Config{
		ServerName: cfg.TargetHost,
		MinVersion: tls.VersionTLS12,
	})
	defer tlsConn.Close()
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return 0, "", "", "", "", err
	}
	logf("tls established: %s", cfg.TargetHost)

	request := fmt.Sprintf(
		"GET %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: gaccel-steam-demo/%s\r\nAccept: */*\r\nConnection: close\r\n\r\n",
		cfg.HTTPPath,
		cfg.TargetHost,
		version,
	)
	if _, err := tlsConn.Write([]byte(request)); err != nil {
		return 0, "", "", "", "", err
	}
	resp, err := http.ReadResponse(bufio.NewReader(tlsConn), nil)
	if err != nil {
		return 0, "", "", "", "", err
	}
	defer resp.Body.Close()

	preview, err := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if err != nil {
		return 0, "", "", "", "", err
	}
	return time.Since(start), resp.Status, resp.Header.Get("Content-Type"), resp.Header.Get("Location"), string(preview), nil
}

func authenticate(ctx context.Context, codec *lineCodec, cfg appConfig, logf func(string, ...any)) error {
	if err := writeWithContext(ctx, func() error {
		return codec.Write(protocol.Message{
			Type:           protocol.MessageHello,
			Version:        protocol.Version,
			ClientID:       cfg.ClientID,
			ClientVersion:  cfg.ClientVersion,
			ClientPlatform: cfg.ClientPlatform,
		})
	}); err != nil {
		return err
	}
	msg, err := readWithContext(ctx, codec)
	if err != nil {
		return err
	}
	if msg.Type != protocol.MessageHello {
		return fmt.Errorf("unexpected hello response: %s", msg.Type)
	}
	if msg.Server != nil {
		logf(
			"server: protocol=%d alpn=%s keepalive=%ds datagram_payload=%d capabilities=%s",
			msg.Server.ProtocolVersion,
			msg.Server.ALPN,
			msg.Server.KeepaliveIntervalSeconds,
			msg.Server.RecommendedDatagramPayloadBytes,
			strings.Join(msg.Server.Capabilities, ","),
		)
	}

	if err := writeWithContext(ctx, func() error {
		return codec.Write(protocol.Message{
			Type:           protocol.MessageAuth,
			Version:        protocol.Version,
			Token:          cfg.Token,
			ClientID:       cfg.ClientID,
			ClientVersion:  cfg.ClientVersion,
			ClientPlatform: cfg.ClientPlatform,
		})
	}); err != nil {
		return err
	}
	msg, err = readWithContext(ctx, codec)
	if err != nil {
		return err
	}
	if msg.Type != protocol.MessageAuthOK {
		return messageError(msg, "auth failed")
	}
	logf("authenticated: user_id=%s device_id=%s", msg.UserID, msg.DeviceID)
	return nil
}

func newLineCodec(rw io.ReadWriter) *lineCodec {
	return &lineCodec{
		reader: bufio.NewReader(rw),
		writer: rw,
	}
}

func (c *lineCodec) Read() (*protocol.Message, error) {
	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	var msg protocol.Message
	if err := json.Unmarshal(line, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func (c *lineCodec) Write(msg protocol.Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = c.writer.Write(data)
	return err
}

func readWithContext(ctx context.Context, codec *lineCodec) (*protocol.Message, error) {
	type result struct {
		msg *protocol.Message
		err error
	}
	ch := make(chan result, 1)
	go func() {
		msg, err := codec.Read()
		ch <- result{msg: msg, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		return res.msg, res.err
	}
}

func writeWithContext(ctx context.Context, write func() error) error {
	ch := make(chan error, 1)
	go func() {
		ch <- write()
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-ch:
		return err
	}
}

func messageError(msg *protocol.Message, fallback string) error {
	if msg == nil {
		return errors.New(fallback)
	}
	if msg.Type == protocol.MessageError {
		return fmt.Errorf("%s: %s", msg.ErrorCode, msg.Error)
	}
	return fmt.Errorf("%s: response=%s", fallback, msg.Type)
}

type streamConn struct {
	stream *quic.Stream
	reader *bufio.Reader
	local  net.Addr
	remote net.Addr
}

func (c *streamConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func (c *streamConn) Write(p []byte) (int, error) {
	return c.stream.Write(p)
}

func (c *streamConn) Close() error {
	return c.stream.Close()
}

func (c *streamConn) LocalAddr() net.Addr {
	return c.local
}

func (c *streamConn) RemoteAddr() net.Addr {
	return c.remote
}

func (c *streamConn) SetDeadline(t time.Time) error {
	if err := c.stream.SetReadDeadline(t); err != nil {
		return err
	}
	return c.stream.SetWriteDeadline(t)
}

func (c *streamConn) SetReadDeadline(t time.Time) error {
	return c.stream.SetReadDeadline(t)
}

func (c *streamConn) SetWriteDeadline(t time.Time) error {
	return c.stream.SetWriteDeadline(t)
}

type relayAddr string

func (a relayAddr) Network() string {
	return "quic-relay"
}

func (a relayAddr) String() string {
	return string(a)
}

func serverName(addr, explicit string) string {
	if explicit != "" {
		return explicit
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if net.ParseIP(host) != nil {
		return ""
	}
	return host
}

func looksLikeJWT(token string) bool {
	token = strings.TrimSpace(token)
	parts := strings.Split(token, ".")
	return len(parts) == 3 && strings.HasPrefix(parts[0], "eyJ") && parts[1] != "" && parts[2] != ""
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 256*1024))
	decoder.DisallowUnknownFields()
	return decoder.Decode(v)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func defaultConfigPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "steam-demo-config.json"
	}
	return filepath.Join(filepath.Dir(exe), "steam-demo-config.json")
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

const indexHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>gaccel Steam QUIC Demo</title>
  <style>
    :root {
      --bg: #f5f7f4;
      --surface: #ffffff;
      --text: #17201b;
      --muted: #66716b;
      --border: #d7ddd6;
      --accent: #1f6f55;
      --accent-dark: #174f3f;
      --danger: #a34035;
      --warning: #8a6217;
      --code: #202823;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      background: var(--bg);
      color: var(--text);
      font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      line-height: 1.5;
    }
    button, input, textarea {
      font: inherit;
    }
    .shell {
      min-height: 100dvh;
      display: grid;
      grid-template-rows: auto 1fr;
    }
    header {
      border-bottom: 1px solid var(--border);
      background: rgba(255,255,255,0.88);
      backdrop-filter: blur(12px);
    }
    .topbar {
      max-width: 1320px;
      margin: 0 auto;
      padding: 18px 22px;
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 16px;
    }
    .brand {
      display: flex;
      align-items: center;
      gap: 12px;
      min-width: 0;
    }
    .mark {
      width: 32px;
      height: 32px;
      border-radius: 8px;
      background: var(--text);
      color: #ffffff;
      display: grid;
      place-items: center;
      font-weight: 800;
      letter-spacing: 0;
    }
    h1 {
      margin: 0;
      font-size: 18px;
      line-height: 1.2;
      letter-spacing: 0;
    }
    .subtitle {
      margin: 2px 0 0;
      color: var(--muted);
      font-size: 13px;
    }
    .badge {
      border: 1px solid var(--border);
      border-radius: 999px;
      padding: 6px 10px;
      background: var(--surface);
      color: var(--muted);
      font-size: 12px;
      white-space: nowrap;
    }
    main {
      max-width: 1320px;
      width: 100%;
      margin: 0 auto;
      padding: 22px;
      display: grid;
      grid-template-columns: minmax(320px, 440px) minmax(0, 1fr);
      gap: 18px;
    }
    .panel {
      background: var(--surface);
      border: 1px solid var(--border);
      border-radius: 8px;
      overflow: hidden;
    }
    .panel-head {
      padding: 14px 16px;
      border-bottom: 1px solid var(--border);
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 12px;
    }
    .panel-head h2 {
      margin: 0;
      font-size: 15px;
      letter-spacing: 0;
    }
    .panel-body {
      padding: 16px;
    }
    .grid {
      display: grid;
      gap: 14px;
    }
    .two {
      grid-template-columns: 1fr 1fr;
    }
    label {
      display: block;
      font-size: 12px;
      font-weight: 700;
      color: var(--text);
      margin-bottom: 6px;
    }
    input, textarea {
      width: 100%;
      border: 1px solid var(--border);
      border-radius: 6px;
      background: #fbfcfb;
      color: var(--text);
      padding: 9px 10px;
      outline: none;
      transition: border-color 160ms ease, box-shadow 160ms ease, background 160ms ease;
    }
    textarea {
      min-height: 92px;
      resize: vertical;
      font-family: ui-monospace, "Cascadia Mono", Consolas, monospace;
      font-size: 12px;
      line-height: 1.45;
    }
    input:focus, textarea:focus {
      border-color: var(--accent);
      box-shadow: 0 0 0 3px rgba(31,111,85,0.12);
      background: #ffffff;
    }
    .checkline {
      display: flex;
      align-items: center;
      gap: 8px;
      padding-top: 22px;
      color: var(--muted);
      font-size: 13px;
    }
    .checkline input {
      width: 16px;
      height: 16px;
      accent-color: var(--accent);
    }
    .section {
      border-top: 1px solid var(--border);
      padding-top: 16px;
      margin-top: 16px;
    }
    .section:first-child {
      border-top: 0;
      padding-top: 0;
      margin-top: 0;
    }
    .section-title {
      margin: 0 0 12px;
      font-size: 13px;
      color: var(--muted);
      font-weight: 800;
      letter-spacing: 0;
    }
    .actions {
      display: flex;
      flex-wrap: wrap;
      gap: 10px;
      margin-top: 16px;
    }
    button {
      border: 1px solid var(--border);
      border-radius: 6px;
      background: #ffffff;
      color: var(--text);
      padding: 9px 12px;
      cursor: pointer;
      transition: transform 120ms ease, background 160ms ease, border-color 160ms ease;
    }
    button:hover {
      background: #f7faf7;
      border-color: #b9c4bd;
    }
    button:active {
      transform: translateY(1px);
    }
    button.primary {
      background: var(--accent);
      border-color: var(--accent);
      color: #ffffff;
    }
    button.primary:hover {
      background: var(--accent-dark);
    }
    button:disabled {
      cursor: not-allowed;
      opacity: 0.62;
    }
    .path {
      margin: 10px 0 0;
      color: var(--muted);
      font-size: 12px;
      word-break: break-all;
    }
    .result-shell {
      display: grid;
      gap: 18px;
    }
    .metrics {
      display: grid;
      grid-template-columns: repeat(4, minmax(0, 1fr));
      gap: 10px;
    }
    .metric {
      border: 1px solid var(--border);
      border-radius: 8px;
      padding: 12px;
      background: #fbfcfb;
    }
    .metric span {
      display: block;
      color: var(--muted);
      font-size: 12px;
    }
    .metric strong {
      display: block;
      margin-top: 6px;
      font-family: ui-monospace, "Cascadia Mono", Consolas, monospace;
      font-size: 18px;
      line-height: 1.2;
      overflow-wrap: anywhere;
    }
    .state {
      padding: 12px 14px;
      border: 1px solid var(--border);
      border-radius: 8px;
      background: #fbfcfb;
      color: var(--muted);
    }
    .state.ok {
      border-color: rgba(31,111,85,0.35);
      color: var(--accent-dark);
      background: rgba(31,111,85,0.08);
    }
    .state.error {
      border-color: rgba(163,64,53,0.35);
      color: var(--danger);
      background: rgba(163,64,53,0.08);
    }
    pre {
      margin: 0;
      max-height: 330px;
      overflow: auto;
      border: 1px solid var(--border);
      border-radius: 8px;
      background: var(--code);
      color: #e8eee9;
      padding: 14px;
      font-family: ui-monospace, "Cascadia Mono", Consolas, monospace;
      font-size: 12px;
      line-height: 1.55;
      white-space: pre-wrap;
      word-break: break-word;
    }
    .preview {
      background: #fbfcfb;
      color: var(--text);
      max-height: 420px;
    }
    .hint {
      color: var(--muted);
      font-size: 12px;
      margin: 7px 0 0;
    }
    @media (max-width: 920px) {
      .topbar {
        align-items: flex-start;
        flex-direction: column;
      }
      main {
        grid-template-columns: 1fr;
        padding: 14px;
      }
      .metrics {
        grid-template-columns: 1fr 1fr;
      }
      .two {
        grid-template-columns: 1fr;
      }
      .checkline {
        padding-top: 0;
      }
    }
  </style>
</head>
<body>
  <div class="shell">
    <header>
      <div class="topbar">
        <div class="brand">
          <div class="mark">G</div>
          <div>
            <h1>gaccel Steam QUIC Demo</h1>
            <p class="subtitle">QUIC 原生链路测试控制台</p>
          </div>
        </div>
        <div class="badge" id="configPath">配置文件加载中</div>
      </div>
    </header>

    <main>
      <section class="panel">
        <div class="panel-head">
          <h2>配置</h2>
          <button id="loadConfig" type="button">加载</button>
        </div>
        <div class="panel-body">
          <div class="section">
            <p class="section-title">节点</p>
            <div class="grid">
              <div>
                <label for="node_addr">节点地址</label>
                <input id="node_addr" autocomplete="off">
              </div>
              <div class="two grid">
                <div>
                  <label for="alpn">ALPN</label>
                  <input id="alpn" autocomplete="off">
                </div>
                <div>
                  <label for="sni">SNI</label>
                  <input id="sni" autocomplete="off">
                </div>
              </div>
              <label class="checkline" for="insecure">
                <input id="insecure" type="checkbox">
                跳过节点 TLS 证书校验
              </label>
            </div>
          </div>

          <div class="section">
            <p class="section-title">Token</p>
            <div class="grid">
              <div>
                <label for="token_api_url">Token API</label>
                <input id="token_api_url" autocomplete="off">
              </div>
              <div>
                <label for="token_api_key">Token API Key</label>
                <input id="token_api_key" autocomplete="off">
              </div>
              <div class="two grid">
                <div>
                  <label for="user_id">用户 ID</label>
                  <input id="user_id" autocomplete="off">
                </div>
                <div>
                  <label for="device_id">设备 ID</label>
                  <input id="device_id" autocomplete="off">
                </div>
              </div>
              <div>
                <label for="ttl_seconds">TTL 秒数</label>
                <input id="ttl_seconds" type="number" min="1" step="1">
              </div>
              <div>
                <label for="token">JWT Token</label>
                <textarea id="token" spellcheck="false"></textarea>
                <p class="hint">这里填 POST /token 返回的 token，不填 API Key。</p>
              </div>
            </div>
            <div class="actions">
              <button id="issueToken" type="button">申请 Token</button>
              <button id="saveConfig" type="button">保存配置</button>
            </div>
            <p class="path" id="pathLine"></p>
          </div>

          <div class="section">
            <p class="section-title">Steam 社区目标</p>
            <div class="grid">
              <div class="two grid">
                <div>
                  <label for="target_host">目标域名</label>
                  <input id="target_host" autocomplete="off">
                </div>
                <div>
                  <label for="target_port">目标端口</label>
                  <input id="target_port" type="number" min="1" max="65535" step="1">
                </div>
              </div>
              <div class="two grid">
                <div>
                  <label for="http_path">HTTP 路径</label>
                  <input id="http_path" autocomplete="off">
                </div>
                <div>
                  <label for="timeout_seconds">超时秒数</label>
                  <input id="timeout_seconds" type="number" min="1" step="1">
                </div>
              </div>
              <div class="two grid">
                <div>
                  <label for="client_id">Client ID</label>
                  <input id="client_id" autocomplete="off">
                </div>
                <div>
                  <label for="client_version">Client Version</label>
                  <input id="client_version" autocomplete="off">
                </div>
              </div>
              <div>
                <label for="client_platform">Client Platform</label>
                <input id="client_platform" autocomplete="off">
              </div>
            </div>
            <div class="actions">
              <button id="runTest" class="primary" type="button">运行 QUIC 测试</button>
            </div>
          </div>
        </div>
      </section>

      <section class="result-shell">
        <div class="panel">
          <div class="panel-head">
            <h2>结果</h2>
            <span class="badge" id="runState">待测试</span>
          </div>
          <div class="panel-body grid">
            <div id="message" class="state">填写配置后运行测试。</div>
            <div class="metrics">
              <div class="metric"><span>HTTP 状态</span><strong id="metricStatus">-</strong></div>
              <div class="metric"><span>延迟</span><strong id="metricLatency">-</strong></div>
              <div class="metric"><span>Content-Type</span><strong id="metricContentType">-</strong></div>
              <div class="metric"><span>Location</span><strong id="metricLocation">-</strong></div>
            </div>
          </div>
        </div>

        <div class="panel">
          <div class="panel-head"><h2>链路日志</h2></div>
          <div class="panel-body">
            <pre id="logs">等待运行。</pre>
          </div>
        </div>

        <div class="panel">
          <div class="panel-head"><h2>响应预览</h2></div>
          <div class="panel-body">
            <pre id="preview" class="preview">测试成功后显示 Steam Community HTML 片段。</pre>
          </div>
        </div>
      </section>
    </main>
  </div>

  <script>
    var fields = [
      "node_addr", "alpn", "sni", "token_api_url", "token_api_key", "user_id",
      "device_id", "ttl_seconds", "token", "target_host", "target_port",
      "http_path", "timeout_seconds", "client_id", "client_version", "client_platform"
    ];

    function el(id) { return document.getElementById(id); }
    function numberValue(id) {
      var value = parseInt(el(id).value, 10);
      return Number.isFinite(value) ? value : 0;
    }
    function configFromForm() {
      return {
        node_addr: el("node_addr").value,
        alpn: el("alpn").value,
        sni: el("sni").value,
        insecure: el("insecure").checked,
        token_api_url: el("token_api_url").value,
        token_api_key: el("token_api_key").value,
        user_id: el("user_id").value,
        device_id: el("device_id").value,
        ttl_seconds: numberValue("ttl_seconds"),
        token: el("token").value,
        target_host: el("target_host").value,
        target_port: numberValue("target_port"),
        http_path: el("http_path").value,
        timeout_seconds: numberValue("timeout_seconds"),
        client_id: el("client_id").value,
        client_version: el("client_version").value,
        client_platform: el("client_platform").value
      };
    }
    function fillConfig(cfg) {
      fields.forEach(function(name) {
        if (cfg[name] !== undefined) el(name).value = cfg[name];
      });
      el("insecure").checked = !!cfg.insecure;
    }
    function setMessage(kind, text) {
      var box = el("message");
      box.className = "state " + (kind || "");
      box.textContent = text;
    }
    function setBusy(button, busy, label) {
      button.disabled = busy;
      if (busy) {
        button.dataset.oldText = button.textContent;
        button.textContent = label;
      } else if (button.dataset.oldText) {
        button.textContent = button.dataset.oldText;
      }
    }
    async function postJSON(path, body) {
      var res = await fetch(path, {
        method: "POST",
        headers: {"Content-Type": "application/json"},
        body: JSON.stringify(body)
      });
      var data = await res.json();
      if (!res.ok) throw new Error(data.error || data.message || res.statusText);
      return data;
    }
    async function loadConfig() {
      var res = await fetch("/api/config");
      var data = await res.json();
      fillConfig(data.config);
      el("configPath").textContent = data.path;
      el("pathLine").textContent = "保存路径：" + data.path;
      setMessage("", "配置已加载。");
    }
    async function saveConfig() {
      var btn = el("saveConfig");
      setBusy(btn, true, "保存中");
      try {
        var data = await postJSON("/api/config", configFromForm());
        fillConfig(data.config);
        el("configPath").textContent = data.path;
        el("pathLine").textContent = "保存路径：" + data.path;
        setMessage("ok", "配置已保存。");
      } catch (err) {
        setMessage("error", err.message);
      } finally {
        setBusy(btn, false);
      }
    }
    async function issueToken() {
      var btn = el("issueToken");
      setBusy(btn, true, "申请中");
      try {
        var data = await postJSON("/api/token", configFromForm());
        fillConfig(data.config);
        setMessage("ok", "Token 已获取，过期时间：" + data.expires_at);
      } catch (err) {
        setMessage("error", err.message);
      } finally {
        setBusy(btn, false);
      }
    }
    function resetResult() {
      el("metricStatus").textContent = "-";
      el("metricLatency").textContent = "-";
      el("metricContentType").textContent = "-";
      el("metricLocation").textContent = "-";
      el("logs").textContent = "运行中。";
      el("preview").textContent = "";
      el("runState").textContent = "运行中";
    }
    async function runTest() {
      var btn = el("runTest");
      setBusy(btn, true, "测试中");
      resetResult();
      setMessage("", "正在通过 QUIC 节点测试 Steam 社区。");
      try {
        var data = await postJSON("/api/test", configFromForm());
        el("metricStatus").textContent = data.status || "-";
        el("metricLatency").textContent = data.latency_ms ? data.latency_ms + "ms" : "-";
        el("metricContentType").textContent = data.content_type || "-";
        el("metricLocation").textContent = data.location || "-";
        el("logs").textContent = (data.logs || []).join("\n");
        el("preview").textContent = data.body_preview || "";
        el("runState").textContent = "通过";
        setMessage("ok", "Steam 社区 QUIC 转发测试通过。");
      } catch (err) {
        el("runState").textContent = "失败";
        setMessage("error", err.message);
        el("logs").textContent = err.message;
      } finally {
        setBusy(btn, false);
      }
    }

    el("loadConfig").addEventListener("click", loadConfig);
    el("saveConfig").addEventListener("click", saveConfig);
    el("issueToken").addEventListener("click", issueToken);
    el("runTest").addEventListener("click", runTest);
    loadConfig().catch(function(err) { setMessage("error", err.message); });
  </script>
</body>
</html>`
