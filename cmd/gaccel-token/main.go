package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"gaccel-node/internal/auth"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	secret := flag.String("secret", "", "HMAC secret")
	userID := flag.String("user", "", "user id")
	deviceID := flag.String("device", "", "device id")
	ttl := flag.Duration("ttl", 15*time.Minute, "token TTL")
	maxConnections := flag.Int("max-connections", 0, "max user connections override, 0 uses server default")
	rateLimit := flag.Int("rate-limit-mbps", 0, "rate limit override, 0 uses server default")
	allowTCP := flag.Bool("allow-tcp", true, "allow TCP relay")
	allowUDP := flag.Bool("allow-udp", true, "allow UDP relay")
	games := flag.String("games", "", "comma separated game ids")
	regions := flag.String("regions", "", "comma separated region ids")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}
	if *secret == "" {
		*secret = os.Getenv("GACCEL_HMAC_SECRET")
	}
	if *secret == "" {
		fmt.Fprintln(os.Stderr, "missing -secret or GACCEL_HMAC_SECRET")
		os.Exit(1)
	}
	if *userID == "" {
		fmt.Fprintln(os.Stderr, "missing -user")
		os.Exit(1)
	}
	if *ttl <= 0 {
		fmt.Fprintln(os.Stderr, "-ttl must be > 0")
		os.Exit(1)
	}
	if *maxConnections < 0 {
		fmt.Fprintln(os.Stderr, "-max-connections must be >= 0")
		os.Exit(1)
	}
	if *rateLimit < 0 {
		fmt.Fprintln(os.Stderr, "-rate-limit-mbps must be >= 0")
		os.Exit(1)
	}
	now := time.Now()
	claims := auth.TokenClaims{
		Subject:        *userID,
		UserID:         *userID,
		DeviceID:       *deviceID,
		IssuedAt:       now.Unix(),
		NotBefore:      now.Add(-5 * time.Second).Unix(),
		ExpiresAt:      now.Add(*ttl).Unix(),
		MaxConnections: *maxConnections,
		RateLimitMbps:  *rateLimit,
		AllowTCP:       allowTCP,
		AllowUDP:       allowUDP,
		Games:          splitList(*games),
		Regions:        splitList(*regions),
	}
	token, err := auth.SignHMACToken(claims, *secret)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sign token: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(token)
}

func splitList(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			items = append(items, part)
		}
	}
	return items
}
