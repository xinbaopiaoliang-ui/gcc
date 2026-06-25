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
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/quic-go/quic-go"

	"gaccel-node/internal/protocol"
)

var version = "dev"

type lineCodec struct {
	reader *bufio.Reader
	writer io.Writer
}

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	addr := flag.String("addr", "127.0.0.1:443", "QUIC server address")
	alpn := flag.String("alpn", "gaccel/1", "QUIC ALPN")
	sni := flag.String("sni", "", "TLS server name")
	token := flag.String("token", "dev-token", "auth token")
	mode := flag.String("mode", "ping", "probe mode: ping, keepalive, udp, tcp, https, steam")
	targetHost := flag.String("target-host", "127.0.0.1", "relay target host")
	targetPort := flag.Int("target-port", 7, "relay target port")
	httpPath := flag.String("http-path", "/", "HTTP path for https/steam probes")
	payload := flag.String("payload", "ping", "payload for udp/tcp probes")
	count := flag.Int("count", 1, "number of udp packets or ping requests")
	interval := flag.Duration("interval", 0, "interval between keepalive pings")
	timeout := flag.Duration("timeout", 5*time.Second, "operation timeout")
	insecure := flag.Bool("insecure", true, "skip TLS certificate verification")
	clientID := flag.String("client-id", "", "client instance id")
	clientVersion := flag.String("client-version", "", "client version")
	clientPlatform := flag.String("client-platform", "", "client platform, for example windows/amd64")
	gameID := flag.String("game-id", "", "flow metadata game_id")
	policyID := flag.String("policy-id", "", "flow metadata policy_id")
	ruleID := flag.String("rule-id", "", "flow metadata rule_id")
	clientConfigRevision := flag.String("client-config-revision", "", "flow metadata client_config_revision")
	processName := flag.String("process-name", "", "flow metadata process_name")
	captureMode := flag.String("capture-mode", "", "flow metadata capture_mode")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	client := clientInfo{
		ID:       *clientID,
		Version:  *clientVersion,
		Platform: *clientPlatform,
	}
	metadata := flowMetadataConfig{
		GameID:               *gameID,
		PolicyID:             *policyID,
		RuleID:               *ruleID,
		ClientConfigRevision: *clientConfigRevision,
		ProcessName:          *processName,
		CaptureMode:          *captureMode,
	}
	if err := run(*addr, *alpn, *sni, *token, *mode, *targetHost, *targetPort, *httpPath, []byte(*payload), *count, *interval, *timeout, *insecure, client, metadata); err != nil {
		fmt.Fprintf(os.Stderr, "probe failed: %v\n", err)
		os.Exit(1)
	}
}

type clientInfo struct {
	ID       string
	Version  string
	Platform string
}

type flowMetadataConfig struct {
	GameID               string
	PolicyID             string
	RuleID               string
	ClientConfigRevision string
	ProcessName          string
	CaptureMode          string
}

func (m flowMetadataConfig) raw(network string) json.RawMessage {
	metadata := protocol.FlowMetadata{
		GameID:               strings.TrimSpace(m.GameID),
		PolicyID:             strings.TrimSpace(m.PolicyID),
		RuleID:               strings.TrimSpace(m.RuleID),
		Network:              strings.ToLower(strings.TrimSpace(network)),
		ProcessName:          strings.TrimSpace(m.ProcessName),
		ClientConfigRevision: strings.TrimSpace(m.ClientConfigRevision),
		CaptureMode:          strings.TrimSpace(m.CaptureMode),
	}
	if metadata.GameID == "" &&
		metadata.PolicyID == "" &&
		metadata.RuleID == "" &&
		metadata.ClientConfigRevision == "" &&
		metadata.ProcessName == "" &&
		metadata.CaptureMode == "" {
		return nil
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return nil
	}
	return data
}

func run(addr, alpn, sni, token, mode, targetHost string, targetPort int, httpPath string, payload []byte, count int, interval, timeout time.Duration, insecure bool, client clientInfo, metadata flowMetadataConfig) error {
	if count <= 0 {
		count = 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	tlsConfig := &tls.Config{
		NextProtos:         []string{alpn},
		InsecureSkipVerify: insecure,
		ServerName:         serverName(addr, sni),
		MinVersion:         tls.VersionTLS13,
	}
	conn, err := quic.DialAddr(ctx, addr, tlsConfig, &quic.Config{
		EnableDatagrams: true,
	})
	if err != nil {
		return err
	}
	defer conn.CloseWithError(0, "probe done")

	control, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return err
	}
	controlCodec := newLineCodec(control)
	if err := authenticate(ctx, controlCodec, token, client); err != nil {
		return err
	}

	switch strings.ToLower(mode) {
	case "ping":
		return probePing(ctx, controlCodec, count, interval)
	case "keepalive":
		if interval <= 0 {
			interval = 15 * time.Second
		}
		return probePing(ctx, controlCodec, count, interval)
	case "udp":
		return probeUDP(ctx, conn, controlCodec, targetHost, targetPort, payload, count, metadata)
	case "tcp":
		return probeTCP(ctx, conn, targetHost, targetPort, payload, metadata)
	case "https":
		if targetPort == 7 {
			targetPort = 443
		}
		return probeHTTPS(ctx, conn, targetHost, targetPort, httpPath, metadata)
	case "steam":
		if targetHost == "" || targetHost == "127.0.0.1" {
			targetHost = "steamcommunity.com"
		}
		if targetPort == 7 {
			targetPort = 443
		}
		return probeHTTPS(ctx, conn, targetHost, targetPort, httpPath, metadata)
	default:
		return fmt.Errorf("unknown mode %q", mode)
	}
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
		return fmt.Errorf("unexpected hello response: %s", msg.Type)
	}
	printServerInfo(msg.Server)

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
	if msg.UserID != "" || msg.DeviceID != "" {
		fmt.Printf("authenticated user_id=%s device_id=%s\n", msg.UserID, msg.DeviceID)
	} else {
		fmt.Println("authenticated")
	}
	return nil
}

func probePing(ctx context.Context, codec *lineCodec, count int, interval time.Duration) error {
	for i := 0; i < count; i++ {
		start := time.Now()
		if err := writeWithContext(ctx, func() error {
			return codec.Write(protocol.Message{Type: protocol.MessagePing})
		}); err != nil {
			return err
		}
		msg, err := readWithContext(ctx, codec)
		if err != nil {
			return err
		}
		if msg.Type != protocol.MessagePong {
			return messageError(msg, "unexpected ping response")
		}
		fmt.Printf("ping %d ok latency=%s\n", i+1, time.Since(start))
		if interval > 0 && i+1 < count {
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return nil
}

func printServerInfo(info *protocol.ServerInfo) {
	if info == nil {
		return
	}
	fmt.Printf(
		"server protocol=%d alpn=%s keepalive=%ds datagram_payload=%d capabilities=%s\n",
		info.ProtocolVersion,
		info.ALPN,
		info.KeepaliveIntervalSeconds,
		info.RecommendedDatagramPayloadBytes,
		strings.Join(info.Capabilities, ","),
	)
}

func probeUDP(ctx context.Context, conn *quic.Conn, codec *lineCodec, targetHost string, targetPort int, payload []byte, count int, metadata flowMetadataConfig) error {
	if err := codec.Write(protocol.Message{
		Type:       protocol.MessageOpenUDP,
		TargetHost: targetHost,
		TargetPort: targetPort,
		Metadata:   metadata.raw("udp"),
	}); err != nil {
		return err
	}
	msg, err := readWithContext(ctx, codec)
	if err != nil {
		return err
	}
	if msg.Type != protocol.MessageOpenUDP {
		return messageError(msg, "open udp failed")
	}
	flowID := msg.FlowID
	fmt.Printf("udp flow opened flow_id=%d target=%s:%d\n", flowID, targetHost, targetPort)

	startAll := time.Now()
	totalBytes := 0
	var minLatency time.Duration
	var maxLatency time.Duration
	var totalLatency time.Duration
	for i := 0; i < count; i++ {
		packet := protocol.MarshalDatagram(protocol.Datagram{
			Version: protocol.Version,
			Type:    protocol.DatagramTypeUDP,
			FlowID:  flowID,
			Seq:     uint32(i + 1),
			Payload: payload,
		})
		start := time.Now()
		if err := conn.SendDatagram(packet); err != nil {
			return err
		}
		response, err := receiveFlowDatagram(ctx, conn, flowID)
		if err != nil {
			return err
		}
		latency := time.Since(start)
		if i == 0 || latency < minLatency {
			minLatency = latency
		}
		if latency > maxLatency {
			maxLatency = latency
		}
		totalLatency += latency
		totalBytes += len(response.Payload)
		fmt.Printf("udp %d ok bytes=%d latency=%s payload=%q\n", i+1, len(response.Payload), latency, string(response.Payload))
	}
	elapsed := time.Since(startAll)
	avgLatency := totalLatency / time.Duration(count)
	mbps := float64(totalBytes*8) / elapsed.Seconds() / 1000 / 1000
	fmt.Printf("udp summary count=%d bytes=%d elapsed=%s avg=%s min=%s max=%s throughput=%.3fMbps\n", count, totalBytes, elapsed, avgLatency, minLatency, maxLatency, mbps)
	return nil
}

func probeTCP(ctx context.Context, conn *quic.Conn, targetHost string, targetPort int, payload []byte, metadata flowMetadataConfig) error {
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return err
	}
	codec := newLineCodec(stream)
	if err := codec.Write(protocol.Message{
		Type:       protocol.MessageOpenTCP,
		TargetHost: targetHost,
		TargetPort: targetPort,
		Metadata:   metadata.raw("tcp"),
	}); err != nil {
		return err
	}
	msg, err := readWithContext(ctx, codec)
	if err != nil {
		return err
	}
	if msg.Type != protocol.MessageOpenTCP {
		return messageError(msg, "open tcp failed")
	}
	fmt.Printf("tcp flow opened flow_id=%d target=%s:%d\n", msg.FlowID, targetHost, targetPort)

	start := time.Now()
	if _, err := stream.Write(payload); err != nil {
		return err
	}
	if closeWriter, ok := any(stream).(interface{ CloseWrite() error }); ok {
		_ = closeWriter.CloseWrite()
	}

	buf := make([]byte, 4096)
	n, err := readRawWithContext(ctx, codec.reader, buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	fmt.Printf("tcp ok bytes=%d latency=%s payload=%q\n", n, time.Since(start), string(buf[:n]))
	return nil
}

func probeHTTPS(ctx context.Context, conn *quic.Conn, targetHost string, targetPort int, path string, metadata flowMetadataConfig) error {
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return err
	}
	codec := newLineCodec(stream)
	if err := codec.Write(protocol.Message{
		Type:       protocol.MessageOpenTCP,
		TargetHost: targetHost,
		TargetPort: targetPort,
		Metadata:   metadata.raw("tcp"),
	}); err != nil {
		return err
	}
	msg, err := readWithContext(ctx, codec)
	if err != nil {
		return err
	}
	if msg.Type != protocol.MessageOpenTCP {
		return messageError(msg, "open tcp failed")
	}
	fmt.Printf("tcp flow opened flow_id=%d target=%s:%d\n", msg.FlowID, targetHost, targetPort)

	rawConn := &streamConn{
		stream: stream,
		reader: codec.reader,
		local:  relayAddr("gaccel-probe"),
		remote: relayAddr(fmt.Sprintf("%s:%d", targetHost, targetPort)),
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = rawConn.SetDeadline(deadline)
	}

	start := time.Now()
	tlsConn := tls.Client(rawConn, &tls.Config{
		ServerName: targetHost,
		MinVersion: tls.VersionTLS12,
	})
	defer tlsConn.Close()
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return err
	}

	request := fmt.Sprintf(
		"GET %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: gaccel-probe/%s\r\nAccept: */*\r\nConnection: close\r\n\r\n",
		path,
		targetHost,
		version,
	)
	if _, err := tlsConn.Write([]byte(request)); err != nil {
		return err
	}
	resp, err := http.ReadResponse(bufio.NewReader(tlsConn), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	preview, err := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if err != nil {
		return err
	}
	contentType := resp.Header.Get("Content-Type")
	location := resp.Header.Get("Location")
	fmt.Printf("https ok status=%s latency=%s content_type=%q location=%q body_preview=%q\n", resp.Status, time.Since(start), contentType, location, string(preview))
	return nil
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

func receiveFlowDatagram(ctx context.Context, conn *quic.Conn, flowID uint32) (*protocol.Datagram, error) {
	for {
		packet, err := conn.ReceiveDatagram(ctx)
		if err != nil {
			return nil, err
		}
		datagram, err := protocol.ParseDatagram(packet)
		if err != nil {
			return nil, err
		}
		if datagram.FlowID == flowID {
			return datagram, nil
		}
	}
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

func readRawWithContext(ctx context.Context, reader *bufio.Reader, buf []byte) (int, error) {
	type result struct {
		n   int
		err error
	}
	ch := make(chan result, 1)
	go func() {
		n, err := reader.Read(buf)
		ch <- result{n: n, err: err}
	}()
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case res := <-ch:
		return res.n, res.err
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
