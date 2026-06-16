package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/quic-go/quic-go"

	"gaccel-node/internal/protocol"
)

const (
	defaultAllowedHosts = "steamcommunity.com,.steamcommunity.com,steampowered.com,.steampowered.com,steamstatic.com,.steamstatic.com,steamusercontent.com,.steamusercontent.com,steamcontent.com,.steamcontent.com,akamaihd.net,.akamaihd.net"
	defaultAllowedPorts = "443"
)

var version = "dev"

type lineCodec struct {
	reader *bufio.Reader
	writer io.Writer
}

type clientInfo struct {
	ID       string
	Version  string
	Platform string
}

type relayClient struct {
	conn   *quic.Conn
	logger *slog.Logger
}

type proxyServer struct {
	listen        string
	relay         *relayClient
	rules         allowRules
	dialTimeout   time.Duration
	logRequests   bool
	logger        *slog.Logger
	activeTunnels sync.WaitGroup
}

type allowRules struct {
	hosts []string
	ports map[int]struct{}
}

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	listen := flag.String("listen", "127.0.0.1:18080", "local HTTP CONNECT listen address")
	addr := flag.String("addr", "127.0.0.1:5555", "QUIC node address")
	alpn := flag.String("alpn", "gaccel/1", "QUIC ALPN")
	sni := flag.String("sni", "", "TLS server name")
	token := flag.String("token", "", "auth token")
	insecure := flag.Bool("insecure", true, "skip TLS certificate verification")
	clientID := flag.String("client-id", "steam-connect-demo", "client instance id")
	clientVersion := flag.String("client-version", version, "client version")
	clientPlatform := flag.String("client-platform", runtime.GOOS+"/"+runtime.GOARCH, "client platform")
	allowedHosts := flag.String("allowed-hosts", defaultAllowedHosts, "comma-separated allowed CONNECT hosts; prefix a rule with . to allow subdomains")
	allowedPorts := flag.String("allowed-ports", defaultAllowedPorts, "comma-separated allowed CONNECT ports")
	dialTimeout := flag.Duration("dial-timeout", 10*time.Second, "timeout for opening a relay flow")
	keepaliveInterval := flag.Duration("keepalive-interval", 15*time.Second, "QUIC control ping interval")
	allowNonLocalListen := flag.Bool("allow-nonlocal-listen", false, "allow listening on non-loopback addresses")
	logRequests := flag.Bool("log-requests", true, "log CONNECT requests")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}
	if strings.TrimSpace(*token) == "" {
		fmt.Fprintln(os.Stderr, "-token is required")
		os.Exit(2)
	}
	if !*allowNonLocalListen && !isLoopbackListen(*listen) {
		fmt.Fprintf(os.Stderr, "refusing to listen on non-loopback address %q; use -allow-nonlocal-listen to override\n", *listen)
		os.Exit(2)
	}

	rules, err := parseAllowRules(*allowedHosts, *allowedPorts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid allow rules: %v\n", err)
		os.Exit(2)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	relay, err := connectRelay(ctx, relayOptions{
		addr:     *addr,
		alpn:     *alpn,
		sni:      *sni,
		token:    strings.TrimSpace(*token),
		insecure: *insecure,
		client: clientInfo{
			ID:       *clientID,
			Version:  *clientVersion,
			Platform: *clientPlatform,
		},
		logger: logger,
	})
	if err != nil {
		logger.Error("connect relay failed", "error", err)
		os.Exit(1)
	}
	defer relay.conn.CloseWithError(0, "demo stopped")

	go relay.keepalive(ctx, *keepaliveInterval)

	server := &proxyServer{
		listen:      *listen,
		relay:       relay,
		rules:       rules,
		dialTimeout: *dialTimeout,
		logRequests: *logRequests,
		logger:      logger.With("component", "connect-demo"),
	}
	logger.Info("local CONNECT demo listening", "listen", *listen, "node", *addr, "allowed_hosts", *allowedHosts, "allowed_ports", *allowedPorts)
	logger.Info("set browser or Steam proxy to this local HTTP proxy for testing", "proxy", "http://"+*listen)
	if err := server.ListenAndServe(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("proxy stopped", "error", err)
		os.Exit(1)
	}
}

type relayOptions struct {
	addr     string
	alpn     string
	sni      string
	token    string
	insecure bool
	client   clientInfo
	logger   *slog.Logger
}

func connectRelay(ctx context.Context, opts relayOptions) (*relayClient, error) {
	tlsConfig := &tls.Config{
		NextProtos:         []string{opts.alpn},
		InsecureSkipVerify: opts.insecure,
		ServerName:         serverName(opts.addr, opts.sni),
		MinVersion:         tls.VersionTLS13,
	}
	conn, err := quic.DialAddr(ctx, opts.addr, tlsConfig, &quic.Config{
		EnableDatagrams: true,
	})
	if err != nil {
		return nil, err
	}

	control, err := conn.OpenStreamSync(ctx)
	if err != nil {
		conn.CloseWithError(0, "open control failed")
		return nil, err
	}
	defer control.Close()
	codec := newLineCodec(control)
	if err := authenticate(ctx, codec, opts.token, opts.client); err != nil {
		conn.CloseWithError(0, "auth failed")
		return nil, err
	}
	opts.logger.Info("relay authenticated", "node", opts.addr, "client_id", opts.client.ID, "client_version", opts.client.Version)
	return &relayClient{conn: conn, logger: opts.logger}, nil
}

func authenticate(ctx context.Context, codec *lineCodec, token string, client clientInfo) error {
	if err := writeWithContext(ctx, func() error {
		return codec.Write(protocol.Message{
			Type:           protocol.MessageHello,
			Version:        protocol.Version,
			ClientID:       client.ID,
			ClientVersion:  client.Version,
			ClientPlatform: client.Platform,
		})
	}); err != nil {
		return err
	}
	msg, err := readWithContext(ctx, codec)
	if err != nil {
		return err
	}
	if msg.Type != protocol.MessageHello {
		return messageError(msg, "unexpected hello response")
	}

	if err := writeWithContext(ctx, func() error {
		return codec.Write(protocol.Message{
			Type:           protocol.MessageAuth,
			Version:        protocol.Version,
			Token:          token,
			ClientID:       client.ID,
			ClientVersion:  client.Version,
			ClientPlatform: client.Platform,
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
	return nil
}

func (c *relayClient) keepalive(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	control, err := c.conn.OpenStreamSync(ctx)
	if err != nil {
		c.logger.Warn("open keepalive stream failed", "error", err)
		return
	}
	defer control.Close()
	codec := newLineCodec(control)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := writeWithContext(pingCtx, func() error {
				return codec.Write(protocol.Message{Type: protocol.MessagePing})
			})
			if err == nil {
				var msg *protocol.Message
				msg, err = readWithContext(pingCtx, codec)
				if err == nil && msg.Type != protocol.MessagePong {
					err = messageError(msg, "unexpected ping response")
				}
			}
			cancel()
			if err != nil {
				c.logger.Warn("keepalive failed", "error", err)
				c.conn.CloseWithError(0, "keepalive failed")
				return
			}
		}
	}
}

func (c *relayClient) openTCP(ctx context.Context, host string, port int) (*quic.Stream, *bufio.Reader, uint32, error) {
	stream, err := c.conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, nil, 0, err
	}
	reader := bufio.NewReader(stream)
	codec := &lineCodec{reader: reader, writer: stream}
	if err := codec.Write(protocol.Message{
		Type:       protocol.MessageOpenTCP,
		TargetHost: host,
		TargetPort: port,
	}); err != nil {
		stream.Close()
		return nil, nil, 0, err
	}
	msg, err := readWithContext(ctx, codec)
	if err != nil {
		stream.Close()
		return nil, nil, 0, err
	}
	if msg.Type != protocol.MessageOpenTCP {
		stream.Close()
		return nil, nil, 0, messageError(msg, "open tcp failed")
	}
	return stream, reader, msg.FlowID, nil
}

func (s *proxyServer) ListenAndServe(ctx context.Context) error {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", s.listen)
	if err != nil {
		return err
	}
	defer listener.Close()

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				s.activeTunnels.Wait()
				return ctx.Err()
			}
			return err
		}
		s.activeTunnels.Add(1)
		go func() {
			defer s.activeTunnels.Done()
			s.handleConn(ctx, conn)
		}()
	}
}

func (s *proxyServer) handleConn(parent context.Context, conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	req, err := http.ReadRequest(reader)
	if err != nil {
		s.writeError(conn, http.StatusBadRequest, "bad request")
		return
	}
	defer req.Body.Close()
	if req.Method != http.MethodConnect {
		s.writeError(conn, http.StatusMethodNotAllowed, "CONNECT only")
		return
	}

	host, port, err := parseConnectTarget(req.Host)
	if err != nil {
		s.writeError(conn, http.StatusBadRequest, err.Error())
		return
	}
	if !s.rules.Allow(host, port) {
		s.writeError(conn, http.StatusForbidden, "target not allowed")
		if s.logRequests {
			s.logger.Warn("connect denied", "target", net.JoinHostPort(host, strconv.Itoa(port)), "remote", conn.RemoteAddr())
		}
		return
	}

	ctx, cancel := context.WithTimeout(parent, s.dialTimeout)
	stream, remoteReader, flowID, err := s.relay.openTCP(ctx, host, port)
	cancel()
	if err != nil {
		s.writeError(conn, http.StatusBadGateway, err.Error())
		if s.logRequests {
			s.logger.Warn("connect open failed", "target", net.JoinHostPort(host, strconv.Itoa(port)), "error", err)
		}
		return
	}
	defer stream.Close()

	if _, err := io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\nProxy-Agent: gaccel-connect-demo/"+version+"\r\n\r\n"); err != nil {
		return
	}
	if s.logRequests {
		s.logger.Info("connect opened", "target", net.JoinHostPort(host, strconv.Itoa(port)), "flow_id", flowID, "remote", conn.RemoteAddr())
	}

	start := time.Now()
	clientReader := io.MultiReader(reader, conn)
	errCh := make(chan error, 2)
	go func() {
		_, err := io.Copy(stream, clientReader)
		_ = stream.Close()
		errCh <- err
	}()
	go func() {
		_, err := io.Copy(conn, remoteReader)
		_ = closeWrite(conn)
		errCh <- err
	}()
	err = <-errCh
	_ = conn.Close()
	_ = stream.Close()
	if s.logRequests {
		if err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.EOF) {
			s.logger.Info("connect closed", "target", net.JoinHostPort(host, strconv.Itoa(port)), "flow_id", flowID, "duration", time.Since(start), "error", err)
		} else {
			s.logger.Info("connect closed", "target", net.JoinHostPort(host, strconv.Itoa(port)), "flow_id", flowID, "duration", time.Since(start))
		}
	}
}

func (s *proxyServer) writeError(w io.Writer, status int, text string) {
	body := text + "\n"
	_, _ = fmt.Fprintf(w, "HTTP/1.1 %d %s\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", status, http.StatusText(status), len(body), body)
}

func parseAllowRules(hostRules, portRules string) (allowRules, error) {
	hosts := splitCSV(hostRules)
	if len(hosts) == 0 {
		return allowRules{}, errors.New("allowed hosts are required")
	}
	ports := map[int]struct{}{}
	for _, value := range splitCSV(portRules) {
		port, err := strconv.Atoi(value)
		if err != nil || port <= 0 || port > 65535 {
			return allowRules{}, fmt.Errorf("invalid port %q", value)
		}
		ports[port] = struct{}{}
	}
	if len(ports) == 0 {
		return allowRules{}, errors.New("allowed ports are required")
	}
	return allowRules{hosts: hosts, ports: ports}, nil
}

func (r allowRules) Allow(host string, port int) bool {
	host = normalizeHost(host)
	if _, ok := r.ports[port]; !ok {
		return false
	}
	for _, rule := range r.hosts {
		rule = normalizeHost(rule)
		if strings.HasPrefix(rule, "*.") {
			rule = "." + strings.TrimPrefix(rule, "*.")
		}
		if strings.HasPrefix(rule, ".") {
			if strings.HasSuffix(host, rule) {
				return true
			}
			continue
		}
		if host == rule {
			return true
		}
	}
	return false
}

func parseConnectTarget(target string) (string, int, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", 0, errors.New("CONNECT target is required")
	}
	if strings.Contains(target, "://") {
		return "", 0, errors.New("CONNECT target must be host:port")
	}

	host, portValue, err := net.SplitHostPort(target)
	if err != nil {
		if strings.Contains(err.Error(), "missing port in address") && !strings.Contains(target, "]") {
			host = target
			portValue = "443"
		} else {
			return "", 0, fmt.Errorf("invalid CONNECT target %q", target)
		}
	}
	host = normalizeHost(host)
	if host == "" {
		return "", 0, errors.New("CONNECT host is required")
	}
	port, err := strconv.Atoi(portValue)
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, fmt.Errorf("invalid CONNECT port %q", portValue)
	}
	return host, port, nil
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	host = strings.Trim(host, "[]")
	host = strings.TrimSuffix(host, ".")
	return host
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func isLoopbackListen(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func closeWrite(conn net.Conn) error {
	type closeWriter interface {
		CloseWrite() error
	}
	if closer, ok := conn.(closeWriter); ok {
		return closer.CloseWrite()
	}
	return conn.Close()
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
	if msg.Type == "" {
		return nil, errors.New("message type is required")
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
