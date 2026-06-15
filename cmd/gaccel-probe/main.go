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
	mode := flag.String("mode", "ping", "probe mode: ping, udp, tcp")
	targetHost := flag.String("target-host", "127.0.0.1", "relay target host")
	targetPort := flag.Int("target-port", 7, "relay target port")
	payload := flag.String("payload", "ping", "payload for udp/tcp probes")
	count := flag.Int("count", 1, "number of udp packets or ping requests")
	timeout := flag.Duration("timeout", 5*time.Second, "operation timeout")
	insecure := flag.Bool("insecure", true, "skip TLS certificate verification")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	if err := run(*addr, *alpn, *sni, *token, *mode, *targetHost, *targetPort, []byte(*payload), *count, *timeout, *insecure); err != nil {
		fmt.Fprintf(os.Stderr, "probe failed: %v\n", err)
		os.Exit(1)
	}
}

func run(addr, alpn, sni, token, mode, targetHost string, targetPort int, payload []byte, count int, timeout time.Duration, insecure bool) error {
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
	if err := authenticate(ctx, controlCodec, token); err != nil {
		return err
	}

	switch strings.ToLower(mode) {
	case "ping":
		return probePing(ctx, controlCodec, count)
	case "udp":
		return probeUDP(ctx, conn, controlCodec, targetHost, targetPort, payload, count)
	case "tcp":
		return probeTCP(ctx, conn, targetHost, targetPort, payload)
	default:
		return fmt.Errorf("unknown mode %q", mode)
	}
}

func authenticate(ctx context.Context, codec *lineCodec, token string) error {
	if err := writeWithContext(ctx, func() error {
		return codec.Write(protocol.Message{Type: protocol.MessageHello, Version: protocol.Version})
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

	if err := writeWithContext(ctx, func() error {
		return codec.Write(protocol.Message{Type: protocol.MessageAuth, Token: token})
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
	fmt.Println("authenticated")
	return nil
}

func probePing(ctx context.Context, codec *lineCodec, count int) error {
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
	}
	return nil
}

func probeUDP(ctx context.Context, conn *quic.Conn, codec *lineCodec, targetHost string, targetPort int, payload []byte, count int) error {
	if err := codec.Write(protocol.Message{
		Type:       protocol.MessageOpenUDP,
		TargetHost: targetHost,
		TargetPort: targetPort,
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

func probeTCP(ctx context.Context, conn *quic.Conn, targetHost string, targetPort int, payload []byte) error {
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return err
	}
	codec := newLineCodec(stream)
	if err := codec.Write(protocol.Message{
		Type:       protocol.MessageOpenTCP,
		TargetHost: targetHost,
		TargetPort: targetPort,
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
