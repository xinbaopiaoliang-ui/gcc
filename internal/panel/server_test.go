package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"gaccel-node/internal/config"
	"gaccel-node/internal/metrics"
	"gaccel-node/internal/panelcommand"
	"gaccel-node/internal/sessions"

	"golang.org/x/crypto/bcrypt"
)

func TestHealthEndpoint(t *testing.T) {
	cfg := DefaultConfig()
	server := NewServer(cfg, slog.Default(), "0.5.0-test")
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("status payload = %#v", payload)
	}
	if payload["service"] != "gaccel-panel" {
		t.Fatalf("service payload = %#v", payload)
	}
	if payload["version"] != "0.5.0-test" {
		t.Fatalf("version payload = %#v", payload)
	}
}

func TestClientIPStripsRemotePort(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.RemoteAddr = "203.0.113.9:49152"
	if got := clientIP(req); got != "203.0.113.9" {
		t.Fatalf("clientIP = %q, want %q", got, "203.0.113.9")
	}
	req.Header.Set("X-Forwarded-For", "198.51.100.10, 203.0.113.9")
	if got := clientIP(req); got != "198.51.100.10" {
		t.Fatalf("forwarded clientIP = %q, want %q", got, "198.51.100.10")
	}
}

func TestTrafficOverviewEndpointAggregatesReports(t *testing.T) {
	cfg := DefaultConfig()
	store := newFakeNodeStore()
	addFakePanelUser(t, store, "admin", "secret-password")
	now := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)
	latestReportAt := now.Add(-1 * time.Minute)
	store.nodes["node-hk-01"] = Node{
		ID:                    1,
		NodeID:                "node-hk-01",
		Name:                  "HK 01",
		Region:                "香港",
		EndpointHost:          "195.245.242.9",
		EndpointPort:          5555,
		Status:                "online",
		CurrentVersion:        "0.6.9",
		CurrentPolicyRevision: "policy-old",
		DesiredPolicyRevision: "policy-new",
		LastReportAt:          &latestReportAt,
		Tags:                  []string{"steam"},
		Labels:                map[string]string{"line": "premium"},
	}
	firstMetrics := metrics.Snapshot{
		TCPClientToTarget: 100,
		TCPTargetToClient: 200,
		UDPClientToTarget: 50,
		UDPTargetToClient: 40,
		Users: []metrics.UserSnapshot{{
			UserID:            "user-1",
			TCPClientToTarget: 100,
			TCPTargetToClient: 200,
		}},
		FlowEvents: []metrics.FlowEventSnapshot{{
			Network: "tcp", Event: "open", Reason: "success", GameID: "steam", PolicyID: "steam-web", Count: 1,
		}, {
			Network: "udp", Event: "drop", Reason: "send_queue_overflow", GameID: "steam", PolicyID: "steam-game", Count: 1,
		}},
	}
	latestMetrics := metrics.Snapshot{
		ActiveQUICConnections: 2,
		ActiveTCPFlows:        3,
		ActiveUDPFlows:        1,
		TCPClientToTarget:     350,
		TCPTargetToClient:     620,
		UDPClientToTarget:     90,
		UDPTargetToClient:     130,
		Users: []metrics.UserSnapshot{{
			UserID:            "user-1",
			ActiveConnections: 2,
			TCPClientToTarget: 350,
			TCPTargetToClient: 620,
			UDPClientToTarget: 90,
			UDPTargetToClient: 130,
		}},
		FlowEvents: []metrics.FlowEventSnapshot{
			{Network: "tcp", Event: "open", Reason: "success", GameID: "steam", PolicyID: "steam-web", Count: 3},
			{Network: "tcp", Event: "open", Reason: "denied", GameID: "steam", PolicyID: "steam-web", Count: 2},
			{Network: "udp", Event: "close", Reason: "session_closed", GameID: "steam", PolicyID: "steam-game", Count: 4},
			{Network: "udp", Event: "drop", Reason: "send_queue_overflow", GameID: "steam", PolicyID: "steam-game", Count: 5},
		},
	}
	firstRaw, err := jsonRaw(firstMetrics)
	if err != nil {
		t.Fatalf("marshal first metrics: %v", err)
	}
	latestRaw, err := jsonRaw(latestMetrics)
	if err != nil {
		t.Fatalf("marshal latest metrics: %v", err)
	}
	store.reports = append(store.reports,
		NodeReport{
			ID:                  1,
			NodeID:              "node-hk-01",
			Version:             "0.6.9",
			RoutePolicyRevision: "policy-old",
			Metrics:             firstRaw,
			ReportedAt:          now.Add(-10 * time.Minute),
			CreatedAt:           now.Add(-10 * time.Minute),
		},
		NodeReport{
			ID:                    2,
			NodeID:                "node-hk-01",
			Version:               "0.6.9",
			RoutePolicyRevision:   "policy-old",
			ActiveQUICConnections: 2,
			ActiveTCPFlows:        3,
			ActiveUDPFlows:        1,
			Metrics:               latestRaw,
			ReportedAt:            latestReportAt,
			CreatedAt:             latestReportAt,
		},
	)

	server := NewServer(cfg, slog.Default(), "0.6.10-test", store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	cookie := loginPanel(t, mux, "admin", "secret-password")
	rec := serveJSONWithCookie(mux, http.MethodGet, "/api/panel/traffic/overview?window_hours=24&limit=10", "", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("traffic overview status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Traffic TrafficOverview `json:"traffic"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode traffic overview: %v body=%s", err, rec.Body.String())
	}
	if payload.Traffic.SampleMode != "window_delta" {
		t.Fatalf("sample mode = %q", payload.Traffic.SampleMode)
	}
	if payload.Traffic.Totals.TotalBytes != 800 {
		t.Fatalf("total bytes = %d, want 800", payload.Traffic.Totals.TotalBytes)
	}
	if payload.Traffic.Totals.FlowOpenErrors != 2 {
		t.Fatalf("flow open errors = %d, want 2", payload.Traffic.Totals.FlowOpenErrors)
	}
	if payload.Traffic.Totals.UDPPacketDrops != 4 {
		t.Fatalf("udp packet drops = %d, want 4", payload.Traffic.Totals.UDPPacketDrops)
	}
	if len(payload.Traffic.Users) != 1 || payload.Traffic.Users[0].UserID != "user-1" {
		t.Fatalf("unexpected user stats: %#v", payload.Traffic.Users)
	}
	if len(payload.Traffic.PolicyConsistency) != 1 || payload.Traffic.PolicyConsistency[0].State != "pending" {
		t.Fatalf("unexpected policy consistency: %#v", payload.Traffic.PolicyConsistency)
	}
}

func TestNodeEndpointsRequireAPIKey(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.BackendAPIKeys = []string{"panel-key"}
	store := newFakeNodeStore()
	server := NewServer(cfg, slog.Default(), "0.5.1-test", store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/panel/nodes", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestPanelLoginFlow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.BackendAPIKeys = []string{"panel-key"}
	cfg.Session.Secret = "session-secret-session-secret"
	store := newFakeNodeStore()
	addFakePanelUser(t, store, "admin", "secret-password")
	server := NewServer(cfg, slog.Default(), "0.5.6-test", store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	rec := serveJSON(mux, http.MethodPost, "/api/panel/login", `{"username":"admin","password":"wrong"}`, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	cookie := loginPanel(t, mux, "admin", "secret-password")
	rec = serveJSONWithCookie(mux, http.MethodGet, "/api/panel/me", "", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("me status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"username":"admin"`) {
		t.Fatalf("me response missing user: %s", rec.Body.String())
	}

	rec = serveJSONWithCookie(mux, http.MethodPost, "/api/panel/logout", "", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout status = %d body=%s", rec.Code, rec.Body.String())
	}
	hasClearCookie := false
	for _, logoutCookie := range rec.Result().Cookies() {
		if logoutCookie.Name == "gaccel_panel_session" && logoutCookie.MaxAge < 0 {
			hasClearCookie = true
		}
	}
	if !hasClearCookie {
		t.Fatalf("logout did not clear session cookie")
	}
}

func TestTokenDefaultsPanelAndBackendEndpoints(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.BackendAPIKeys = []string{"panel-key"}
	cfg.Session.Secret = "session-secret-session-secret"
	store := newFakeNodeStore()
	addFakePanelUser(t, store, "admin", "secret-password")
	server := NewServer(cfg, slog.Default(), "0.6.8-test", store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	cookie := loginPanel(t, mux, "admin", "secret-password")
	rec := serveJSONWithCookie(mux, http.MethodGet, "/api/panel/token-defaults", "", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("panel token defaults status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		TokenDefaults TokenDefaults `json:"token_defaults"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode token defaults: %v", err)
	}
	if payload.TokenDefaults.NodeHardLimit != TokenDefaultNodeHardLimit {
		t.Fatalf("node hard limit = %d, want %d", payload.TokenDefaults.NodeHardLimit, TokenDefaultNodeHardLimit)
	}
	if len(payload.TokenDefaults.Plans) != 4 || payload.TokenDefaults.Plans[1].PlanID != "standard" {
		t.Fatalf("unexpected defaults: %#v", payload.TokenDefaults.Plans)
	}

	update := `{"plans":[
		{"plan_id":"trial","name":"免费/测试","max_connections":32,"rate_limit_mbps":50,"allow_tcp":true,"allow_udp":true,"sort_order":10},
		{"plan_id":"standard","name":"普通","max_connections":64,"rate_limit_mbps":100,"allow_tcp":true,"allow_udp":true,"sort_order":20},
		{"plan_id":"advanced","name":"高级","max_connections":128,"rate_limit_mbps":200,"allow_tcp":true,"allow_udp":true,"sort_order":30},
		{"plan_id":"premium","name":"旗舰","max_connections":256,"rate_limit_mbps":500,"allow_tcp":true,"allow_udp":true,"sort_order":40}
	]}`
	rec = serveJSONWithCookie(mux, http.MethodPut, "/api/panel/token-defaults", update, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("update token defaults status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = serveJSON(mux, http.MethodGet, "/api/backend/token-defaults", "", "panel-key")
	if rec.Code != http.StatusOK {
		t.Fatalf("backend token defaults status = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode backend token defaults: %v", err)
	}
	if payload.TokenDefaults.Plans[3].MaxConnections != 256 || payload.TokenDefaults.Plans[3].RateLimitMbps != 500 {
		t.Fatalf("backend defaults not updated: %#v", payload.TokenDefaults.Plans[3])
	}
}

func TestTokenDefaultsRejectsOverHardLimit(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Session.Secret = "session-secret-session-secret"
	store := newFakeNodeStore()
	addFakePanelUser(t, store, "admin", "secret-password")
	server := NewServer(cfg, slog.Default(), "0.6.8-test", store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	cookie := loginPanel(t, mux, "admin", "secret-password")
	rec := serveJSONWithCookie(mux, http.MethodPut, "/api/panel/token-defaults", `{"plans":[{"plan_id":"bad","name":"超限","max_connections":513,"rate_limit_mbps":100,"allow_tcp":true,"allow_udp":true}]}`, cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "max_connections") {
		t.Fatalf("response should mention max_connections: %s", rec.Body.String())
	}
}

func TestPanelCORSAllowsConfiguredFrontendOrigin(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.BackendAPIKeys = []string{"panel-key"}
	cfg.Session.Secret = "session-secret-session-secret"
	cfg.CORS.AllowedOrigins = []string{"http://103.201.131.99:9788"}
	server := NewServer(cfg, slog.Default(), "0.5.9-test")
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	handler := server.withCORS(mux)

	req := httptest.NewRequest(http.MethodOptions, "/api/panel/login", nil)
	req.Header.Set("Origin", "http://103.201.131.99:9788")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://103.201.131.99:9788" {
		t.Fatalf("allow origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "Authorization") {
		t.Fatalf("allow headers missing Authorization: %q", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "http://evil.example")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("disallowed origin should not be reflected, got %q", got)
	}
}

func TestPanelLoginRateLimit(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.BackendAPIKeys = []string{"panel-key"}
	cfg.Session.Secret = "session-secret-session-secret"
	store := newFakeNodeStore()
	addFakePanelUser(t, store, "admin", "secret-password")
	server := NewServer(cfg, slog.Default(), "0.5.7-test", store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	for i := 0; i < 5; i++ {
		rec := serveJSON(mux, http.MethodPost, "/api/panel/login", `{"username":"admin","password":"wrong"}`, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want %d body=%s", i+1, rec.Code, http.StatusUnauthorized, rec.Body.String())
		}
	}
	rec := serveJSON(mux, http.MethodPost, "/api/panel/login", `{"username":"admin","password":"secret-password"}`, "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked login status = %d, want %d body=%s", rec.Code, http.StatusTooManyRequests, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "login_rate_limited") {
		t.Fatalf("blocked login missing code: %s", rec.Body.String())
	}
}

func TestPanelChangePassword(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.BackendAPIKeys = []string{"panel-key"}
	cfg.Session.Secret = "session-secret-session-secret"
	store := newFakeNodeStore()
	addFakePanelUser(t, store, "admin", "secret-password")
	server := NewServer(cfg, slog.Default(), "0.5.7-test", store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	cookie := loginPanel(t, mux, "admin", "secret-password")
	rec := serveJSONWithCookie(mux, http.MethodPost, "/api/panel/me/password", `{"current_password":"wrong","new_password":"new-secret-password"}`, cookie)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong current password status = %d, want %d body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	rec = serveJSONWithCookie(mux, http.MethodPost, "/api/panel/me/password", `{"current_password":"secret-password","new_password":"short"}`, cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("weak password status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	rec = serveJSONWithCookie(mux, http.MethodPost, "/api/panel/me/password", `{"current_password":"secret-password","new_password":"new-secret-password"}`, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("change password status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = serveJSON(mux, http.MethodPost, "/api/panel/login", `{"username":"admin","password":"secret-password"}`, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("old password login status = %d, want %d body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	loginPanel(t, mux, "admin", "new-secret-password")
}

func TestPanelUserManagementFlow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.BackendAPIKeys = []string{"panel-key"}
	cfg.Session.Secret = "session-secret-session-secret"
	store := newFakeNodeStore()
	admin := addFakePanelUser(t, store, "admin", "secret-password")
	server := NewServer(cfg, slog.Default(), "0.5.8-test", store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	cookie := loginPanel(t, mux, "admin", "secret-password")

	createReq := `{"username":"ops","password":"operator-password","role":"operator","status":"active"}`
	rec := serveJSONWithCookie(mux, http.MethodPost, "/api/panel/users", createReq, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("create user status = %d body=%s", rec.Code, rec.Body.String())
	}
	var createPayload struct {
		User PanelUser `json:"user"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&createPayload); err != nil {
		t.Fatalf("decode create user: %v", err)
	}
	if createPayload.User.Username != "ops" || createPayload.User.Role != PanelUserRoleOperator {
		t.Fatalf("created user = %#v", createPayload.User)
	}

	rec = serveJSONWithCookie(mux, http.MethodGet, "/api/panel/users", "", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("list users status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"count":2`) {
		t.Fatalf("list users response = %s", rec.Body.String())
	}

	resetReq := `{"new_password":"reset-password"}`
	rec = serveJSONWithCookie(mux, http.MethodPost, "/api/panel/users/"+strconv.FormatUint(createPayload.User.ID, 10)+"/password", resetReq, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset password status = %d body=%s", rec.Code, rec.Body.String())
	}
	loginPanel(t, mux, "ops", "reset-password")

	updateReq := `{"role":"viewer","status":"disabled"}`
	rec = serveJSONWithCookie(mux, http.MethodPut, "/api/panel/users/"+strconv.FormatUint(createPayload.User.ID, 10), updateReq, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("update user status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = serveJSON(mux, http.MethodPost, "/api/panel/login", `{"username":"ops","password":"reset-password"}`, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("disabled user login status = %d, want %d body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}

	selfUpdateReq := `{"role":"viewer","status":"active"}`
	rec = serveJSONWithCookie(mux, http.MethodPut, "/api/panel/users/"+strconv.FormatUint(admin.ID, 10), selfUpdateReq, cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("self demote status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestPanelRoleRestrictions(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.BackendAPIKeys = []string{"panel-key"}
	cfg.Session.Secret = "session-secret-session-secret"
	store := newFakeNodeStore()
	addFakePanelUser(t, store, "admin", "secret-password")
	addFakePanelUserWithRole(t, store, "ops", "operator-password", PanelUserRoleOperator)
	server := NewServer(cfg, slog.Default(), "0.5.8-test", store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	operatorCookie := loginPanel(t, mux, "ops", "operator-password")

	rec := serveJSONWithCookie(mux, http.MethodGet, "/api/panel/nodes", "", operatorCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("operator list nodes status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = serveJSONWithCookie(mux, http.MethodGet, "/api/panel/users", "", operatorCookie)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("operator list users status = %d, want %d body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	createReq := `{"node_id":"node-hk-01","name":"Node","endpoint_host":"195.245.242.9","endpoint_port":5555}`
	rec = serveJSONWithCookie(mux, http.MethodPost, "/api/panel/nodes", createReq, operatorCookie)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("operator create node status = %d, want %d body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestPanelBackendAPIKeysViewRequiresAdmin(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.BackendAPIKeys = []string{"backend-key-one", "backend-key-two"}
	cfg.Session.Secret = "session-secret-session-secret"
	store := newFakeNodeStore()
	addFakePanelUser(t, store, "admin", "secret-password")
	addFakePanelUserWithRole(t, store, "ops", "operator-password", PanelUserRoleOperator)
	server := NewServer(cfg, slog.Default(), "0.7.6-test", store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	operatorCookie := loginPanel(t, mux, "ops", "operator-password")
	rec := serveJSONWithCookie(mux, http.MethodGet, "/api/panel/security/backend-api-keys", "", operatorCookie)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("operator backend api keys status = %d, want %d body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}

	adminCookie := loginPanel(t, mux, "admin", "secret-password")
	rec = serveJSONWithCookie(mux, http.MethodGet, "/api/panel/security/backend-api-keys", "", adminCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin backend api keys status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload BackendAPIKeysResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode backend api keys: %v", err)
	}
	if payload.Count != 2 || len(payload.Keys) != 2 {
		t.Fatalf("unexpected backend api keys count: %#v", payload)
	}
	if payload.Keys[0].Key != "backend-key-one" || payload.Keys[0].Masked == payload.Keys[0].Key || payload.Keys[0].Length != len("backend-key-one") {
		t.Fatalf("unexpected first key payload: %#v", payload.Keys[0])
	}
	if len(store.audit) == 0 || store.audit[len(store.audit)-1].Action != "panel.security.backend_api_keys.view" {
		t.Fatalf("backend api key view audit missing: %#v", store.audit)
	}
}

func TestPanelNodeCRUD(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.BackendAPIKeys = []string{"panel-key"}
	cfg.Session.Secret = "session-secret-session-secret"
	store := newFakeNodeStore()
	addFakePanelUser(t, store, "admin", "secret-password")
	server := NewServer(cfg, slog.Default(), "0.5.1-test", store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	cookie := loginPanel(t, mux, "admin", "secret-password")
	store.audit = nil

	createReq := `{
		"node_id": "node-hk-01",
		"name": "Hong Kong 01",
		"region": "hk",
		"provider": "test",
		"line_type": "premium",
		"endpoint_host": "195.245.242.9",
		"endpoint_port": 5555,
		"ssh_host": "195.245.242.9",
		"tags": ["steam", "quic"],
		"labels": {"provider": "test"},
		"desired_version": "v0.4.6",
		"desired_policy_revision": "20260616.1"
	}`
	rec := serveJSONWithCookie(mux, http.MethodPost, "/api/panel/nodes", createReq, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	var createPayload struct {
		Node Node `json:"node"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&createPayload); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if createPayload.Node.NodeID != "node-hk-01" {
		t.Fatalf("created node_id = %q", createPayload.Node.NodeID)
	}
	if createPayload.Node.AdminPort != 5557 || createPayload.Node.SSHPort != 22 {
		t.Fatalf("defaults not applied: %#v", createPayload.Node)
	}

	rec = serveJSONWithCookie(mux, http.MethodGet, "/api/panel/nodes?q=hk", "", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", rec.Code, rec.Body.String())
	}
	var listPayload struct {
		Nodes []Node `json:"nodes"`
		Count int    `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&listPayload); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if listPayload.Count != 1 {
		t.Fatalf("list count = %d", listPayload.Count)
	}

	rec = serveJSONWithCookie(mux, http.MethodGet, "/api/panel/nodes/node-hk-01", "", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", rec.Code, rec.Body.String())
	}

	updateReq := `{
		"name": "Hong Kong 01 Updated",
		"region": "hk",
		"endpoint_host": "node.example.com",
		"endpoint_port": 5555,
		"allow_udp": false
	}`
	rec = serveJSON(mux, http.MethodPut, "/api/backend/nodes/node-hk-01", updateReq, "panel-key")
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", rec.Code, rec.Body.String())
	}
	if store.nodes["node-hk-01"].AllowUDP {
		t.Fatal("allow_udp was not updated")
	}

	rec = serveJSON(mux, http.MethodDelete, "/api/backend/nodes/node-hk-01", "", "panel-key")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(store.nodes) != 0 {
		t.Fatalf("store still has nodes: %#v", store.nodes)
	}
	if len(store.audit) != 3 {
		t.Fatalf("audit entries = %d, want 3", len(store.audit))
	}
}

func TestBackendNodeUpsertStoresEncryptedHMACSecret(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.BackendAPIKeys = []string{"panel-key"}
	cfg.Security.MasterKey = "panel-master-key-panel-master-key"
	store := newFakeNodeStore()
	server := NewServer(cfg, slog.Default(), "0.6.4-test", store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	createReq := `{
		"node_id": "node-hk-01",
		"name": "Hong Kong 01",
		"endpoint_host": "195.245.242.9",
		"endpoint_port": 5555,
		"ssh_host": "195.245.242.9",
		"hmac_secret": "backend-issued-node-secret-123456"
	}`
	rec := serveJSON(mux, http.MethodPost, "/api/backend/nodes", createReq, "panel-key")
	if rec.Code != http.StatusOK {
		t.Fatalf("backend create status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "backend-issued-node-secret-123456") {
		t.Fatalf("response leaked hmac secret: %s", rec.Body.String())
	}
	node := store.nodes["node-hk-01"]
	if !node.HMACSecretConfigured || node.HMACSecretEncrypted == "" {
		t.Fatalf("node hmac secret not stored: %#v", node)
	}
	if node.HMACSecretEncrypted == "backend-issued-node-secret-123456" {
		t.Fatalf("node hmac secret stored as plaintext")
	}
	if node.HMACSecretSource != "backend" || node.HMACSecretUpdatedAt == nil {
		t.Fatalf("unexpected hmac metadata: %#v", node)
	}
	if len(store.audit) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(store.audit))
	}
	auditPayload, ok := store.audit[0].Request.(map[string]any)
	if !ok || auditPayload["hmac_secret"] != "[redacted]" {
		t.Fatalf("audit hmac was not redacted: %#v", store.audit[0].Request)
	}

	encryptedBefore := node.HMACSecretEncrypted
	updateReq := `{
		"name": "Hong Kong 01 Updated",
		"endpoint_host": "195.245.242.9",
		"endpoint_port": 5555
	}`
	rec = serveJSON(mux, http.MethodPut, "/api/backend/nodes/node-hk-01", updateReq, "panel-key")
	if rec.Code != http.StatusOK {
		t.Fatalf("backend update status = %d body=%s", rec.Code, rec.Body.String())
	}
	if store.nodes["node-hk-01"].HMACSecretEncrypted != encryptedBefore {
		t.Fatalf("hmac secret should be preserved when omitted")
	}
}

func TestPanelNodeHMACSecretRepairFlow(t *testing.T) {
	oldBox, err := NewSecretBox("old-master-key-old-master-key")
	if err != nil {
		t.Fatalf("old secret box: %v", err)
	}
	encryptedWithOldKey, err := oldBox.Encrypt("backend-issued-node-secret-123456")
	if err != nil {
		t.Fatalf("encrypt old hmac secret: %v", err)
	}
	cfg := DefaultConfig()
	cfg.Security.MasterKey = "new-master-key-new-master-key"
	cfg.Session.Secret = "session-secret-session-secret"
	store := newFakeNodeStore()
	addFakePanelUser(t, store, "admin", "secret-password")
	addFakePanelUserWithRole(t, store, "ops", "operator-password", PanelUserRoleOperator)
	store.nodes["node-hk-01"] = Node{
		ID:                   1,
		NodeID:               "node-hk-01",
		Name:                 "Hong Kong 01",
		EndpointHost:         "195.245.242.9",
		EndpointPort:         5555,
		ALPN:                 "gaccel/1",
		AdminHost:            "127.0.0.1",
		AdminPort:            5557,
		SSHHost:              "195.245.242.9",
		SSHPort:              22,
		SSHUser:              "root",
		AllowTCP:             true,
		AllowUDP:             true,
		HMACSecretEncrypted:  encryptedWithOldKey,
		HMACSecretConfigured: true,
		HMACSecretSource:     "backend",
		Status:               "online",
		CreatedAt:            time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
		UpdatedAt:            time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
	}
	server := NewServer(cfg, slog.Default(), "0.7.7-test", store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	operatorCookie := loginPanel(t, mux, "ops", "operator-password")
	rec := serveJSONWithCookie(mux, http.MethodGet, "/api/panel/nodes/node-hk-01/hmac-secret", "", operatorCookie)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("operator status = %d, want %d body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}

	adminCookie := loginPanel(t, mux, "admin", "secret-password")
	rec = serveJSONWithCookie(mux, http.MethodGet, "/api/panel/nodes/node-hk-01/hmac-secret", "", adminCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("get hmac status = %d body=%s", rec.Code, rec.Body.String())
	}
	var statusPayload struct {
		HMACSecret NodeHMACSecretStatus `json:"hmac_secret"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&statusPayload); err != nil {
		t.Fatalf("decode hmac status: %v", err)
	}
	if statusPayload.HMACSecret.Status != "decrypt_failed" || !statusPayload.HMACSecret.CanClear {
		t.Fatalf("unexpected hmac status: %#v", statusPayload.HMACSecret)
	}

	syncReq := `{"hmac_secret":"panel-resynced-node-secret-123456"}`
	rec = serveJSONWithCookie(mux, http.MethodPut, "/api/panel/nodes/node-hk-01/hmac-secret", syncReq, adminCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("sync hmac status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "panel-resynced-node-secret-123456") {
		t.Fatalf("sync response leaked hmac secret: %s", rec.Body.String())
	}
	var syncPayload struct {
		Node       Node                 `json:"node"`
		HMACSecret NodeHMACSecretStatus `json:"hmac_secret"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&syncPayload); err != nil {
		t.Fatalf("decode sync payload: %v", err)
	}
	if syncPayload.HMACSecret.Status != "ok" || syncPayload.HMACSecret.SecretFingerprint == "" {
		t.Fatalf("unexpected synced hmac status: %#v", syncPayload.HMACSecret)
	}
	if store.nodes["node-hk-01"].HMACSecretSource != "panel" {
		t.Fatalf("hmac source = %q, want panel", store.nodes["node-hk-01"].HMACSecretSource)
	}

	rec = serveJSONWithCookie(mux, http.MethodDelete, "/api/panel/nodes/node-hk-01/hmac-secret", "", adminCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear hmac status = %d body=%s", rec.Code, rec.Body.String())
	}
	if store.nodes["node-hk-01"].HMACSecretConfigured || store.nodes["node-hk-01"].HMACSecretEncrypted != "" {
		t.Fatalf("hmac secret was not cleared: %#v", store.nodes["node-hk-01"])
	}
	if len(store.audit) < 2 {
		t.Fatalf("audit entries = %d, want at least 2", len(store.audit))
	}
	if store.audit[len(store.audit)-2].Action != "panel.node.hmac_secret.sync" ||
		store.audit[len(store.audit)-1].Action != "panel.node.hmac_secret.clear" {
		t.Fatalf("unexpected audit actions: %#v", store.audit)
	}
	auditPayload, ok := store.audit[len(store.audit)-2].Request.(map[string]any)
	if !ok || auditPayload["hmac_secret"] != "[redacted]" {
		t.Fatalf("sync audit hmac was not redacted: %#v", store.audit[len(store.audit)-2].Request)
	}
}

func TestPanelNodeRejectsMismatchedPathNodeID(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.BackendAPIKeys = []string{"panel-key"}
	cfg.Session.Secret = "session-secret-session-secret"
	store := newFakeNodeStore()
	addFakePanelUser(t, store, "admin", "secret-password")
	server := NewServer(cfg, slog.Default(), "0.5.1-test", store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	cookie := loginPanel(t, mux, "admin", "secret-password")

	body := `{"node_id":"other","name":"Node","endpoint_host":"node.example.com","endpoint_port":5555}`
	rec := serveJSONWithCookie(mux, http.MethodPut, "/api/panel/nodes/node-hk-01", body, cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestWebRootServesStaticAndSPAFallback(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(root+"/index.html", []byte("<main>panel app</main>"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.Mkdir(root+"/assets", 0o755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if err := os.WriteFile(root+"/assets/app.js", []byte("console.log('panel')"), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	cfg := DefaultConfig()
	cfg.Web.Root = root
	server := NewServer(cfg, slog.Default(), "0.5.1-test")
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "panel app") {
		t.Fatalf("index response code=%d body=%q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "panel") {
		t.Fatalf("asset response code=%d body=%q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nodes/node-hk-01", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "panel app") {
		t.Fatalf("fallback response code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestNodeReportAndCommandFlow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.BackendAPIKeys = []string{"panel-key"}
	cfg.NodeCommand.Secret = "command-secret-command-secret"
	cfg.Session.Secret = "session-secret-session-secret"
	store := newFakeNodeStore()
	addFakePanelUser(t, store, "admin", "secret-password")
	server := NewServer(cfg, slog.Default(), "0.5.2-test", store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	cookie := loginPanel(t, mux, "admin", "secret-password")

	createReq := `{
		"node_id": "node-hk-01",
		"name": "Hong Kong 01",
		"endpoint_host": "195.245.242.9",
		"endpoint_port": 5555
	}`
	rec := serveJSONWithCookie(mux, http.MethodPost, "/api/panel/nodes", createReq, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}

	reportReq := `{
		"status": "ok",
		"version": "0.4.6-local",
		"timestamp": "2026-06-16T12:00:00Z",
		"node": {"id": "node-hk-01", "region": "hk", "tags": ["steam"], "labels": {"line": "premium"}},
		"server": {"listen": ":5555", "alpn": "gaccel/1"},
		"route_policies": {"revision": "20260616.1", "policy_count": 2},
		"metrics": {"active_quic_connections": 3, "active_tcp_flows": 1, "active_udp_flows": 2}
	}`
	rec = serveJSON(mux, http.MethodPost, "/api/nodes/report", reportReq, "panel-key")
	if rec.Code != http.StatusOK {
		t.Fatalf("report status = %d body=%s", rec.Code, rec.Body.String())
	}
	node := store.nodes["node-hk-01"]
	if node.Status != "online" || node.CurrentVersion != "0.4.6-local" || node.CurrentPolicyRevision != "20260616.1" {
		t.Fatalf("node not updated from report: %#v", node)
	}
	if len(store.reports) != 1 {
		t.Fatalf("reports = %d, want 1", len(store.reports))
	}

	policyReq := `{"route_policies_yaml":"route_policies:\n  revision: \"20260616.2\"\n  policies: []\n"}`
	rec = serveJSONWithCookie(mux, http.MethodPost, "/api/panel/nodes/node-hk-01/commands/apply_policy", policyReq, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("create task status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(store.tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(store.tasks))
	}

	rec = serveJSON(mux, http.MethodGet, "/api/nodes/commands?node_id=node-hk-01", "", "panel-key")
	if rec.Code != http.StatusOK {
		t.Fatalf("commands status = %d body=%s", rec.Code, rec.Body.String())
	}
	timestamp := rec.Header().Get(panelcommand.HeaderTimestamp)
	nonce := rec.Header().Get(panelcommand.HeaderNonce)
	signature := rec.Header().Get(panelcommand.HeaderSignature)
	expected := panelcommand.SignBody(cfg.NodeCommand.Secret, timestamp, nonce, rec.Body.Bytes())
	if signature != expected {
		t.Fatalf("signature = %q, want %q", signature, expected)
	}
	var envelope panelcommand.Envelope
	if err := json.NewDecoder(rec.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode commands: %v", err)
	}
	if len(envelope.Commands) != 1 || envelope.Commands[0].Type != panelcommand.CommandApplyPolicy {
		t.Fatalf("unexpected commands: %#v", envelope.Commands)
	}
	taskID := envelope.Commands[0].ID
	if store.tasks[taskID].Status != TaskStatusRunning {
		t.Fatalf("task status after claim = %q", store.tasks[taskID].Status)
	}

	reportResultReq := `{
		"status": "ok",
		"version": "0.4.6-local",
		"timestamp": "2026-06-16T12:00:30Z",
		"node": {"id": "node-hk-01"},
		"server": {"listen": ":5555", "alpn": "gaccel/1"},
		"route_policies": {"revision": "20260616.2", "policy_count": 2},
		"metrics": {},
		"panel_commands": [{"id": "` + taskID + `", "type": "apply_policy", "ok": true, "executed_at": "2026-06-16T12:00:29Z"}]
	}`
	rec = serveJSON(mux, http.MethodPost, "/api/nodes/report", reportResultReq, "panel-key")
	if rec.Code != http.StatusOK {
		t.Fatalf("report result status = %d body=%s", rec.Code, rec.Body.String())
	}
	if store.tasks[taskID].Status != TaskStatusSuccess {
		t.Fatalf("task status after report = %q", store.tasks[taskID].Status)
	}
}

func TestClientSessionsFromNodeReport(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.BackendAPIKeys = []string{"panel-key"}
	cfg.Session.Secret = "session-secret-session-secret"
	store := newFakeNodeStore()
	addFakePanelUser(t, store, "admin", "secret-password")
	store.nodes["node-hk-01"] = Node{
		NodeID:       "node-hk-01",
		Name:         "Hong Kong 01",
		EndpointHost: "195.245.242.9",
		EndpointPort: 5555,
		ALPN:         "gaccel/1",
		Status:       "online",
		AllowTCP:     true,
		AllowUDP:     true,
		CreatedAt:    time.Date(2026, 6, 23, 10, 0, 0, 0, time.UTC),
		UpdatedAt:    time.Date(2026, 6, 23, 10, 0, 0, 0, time.UTC),
	}
	server := NewServer(cfg, slog.Default(), "0.6.16-test", store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	cookie := loginPanel(t, mux, "admin", "secret-password")

	reportReq := `{
		"status": "ok",
		"version": "0.6.16-test",
		"timestamp": "2026-06-23T10:01:00Z",
		"node": {"id": "node-hk-01"},
		"server": {"listen": ":5555", "alpn": "gaccel/1"},
		"route_policies": {"revision": "20260623.1", "policy_count": 1},
		"metrics": {"active_quic_connections": 1},
		"sessions": [{
			"id": "sess-1",
			"remote_addr": "123.234.174.226:52616",
			"user_id": "user-1",
			"device_id": "win-device-1",
			"client_id": "velox-gaccel",
			"client_version": "0.1.0",
			"client_platform": "windows/x86_64",
			"protocol_version": 1,
			"authenticated": true,
			"max_connections": 64,
			"rate_limit_mbps": 100,
			"allow_tcp": true,
			"allow_udp": true,
			"game_ids": ["steam"],
			"policy_ids": ["steam-web"],
			"config_revision": "20260623.1",
			"created_at": "2026-06-23T10:00:00Z",
			"authenticated_at": "2026-06-23T10:00:01Z",
			"last_seen": "2026-06-23T10:01:00Z",
			"last_ping_at": "2026-06-23T10:00:45Z",
			"connected_duration_seconds": 60,
			"udp_flows": 1,
			"tcp_flows": 2,
			"tcp_client_to_target_bytes": 100,
			"tcp_target_to_client_bytes": 300
		}],
		"session_events": [{
			"sequence": 3,
			"type": "session_ended",
			"session_id": "sess-1",
			"remote_addr": "123.234.174.226:52616",
			"user_id": "user-1",
			"device_id": "win-device-1",
			"client_id": "velox-gaccel",
			"client_version": "0.1.0",
			"client_platform": "windows/x86_64",
			"protocol_version": 1,
			"status": "closed",
			"close_reason": "heartbeat_timeout",
			"close_source": "node",
			"game_ids": ["steam"],
			"policy_ids": ["steam-web"],
			"config_revision": "20260623.1",
			"connected_at": "2026-06-23T10:00:00Z",
			"authenticated_at": "2026-06-23T10:00:01Z",
			"last_seen_at": "2026-06-23T10:01:00Z",
			"ended_at": "2026-06-23T10:01:45Z",
			"duration_seconds": 105,
			"tcp_client_to_target_bytes": 100,
			"tcp_target_to_client_bytes": 300
		}]
	}`
	rec := serveJSON(mux, http.MethodPost, "/api/nodes/report", reportReq, "panel-key")
	if rec.Code != http.StatusOK {
		t.Fatalf("report status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = serveJSONWithCookie(mux, http.MethodGet, "/api/panel/client-sessions?window_hours=24&user_id=user-1", "", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("sessions status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Sessions []ClientSession       `json:"sessions"`
		Overview ClientSessionOverview `json:"overview"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	if len(payload.Sessions) != 1 {
		t.Fatalf("len(sessions) = %d, want 1", len(payload.Sessions))
	}
	if payload.Sessions[0].Status != ClientSessionStatusClosed || payload.Sessions[0].CloseReason != "heartbeat_timeout" {
		t.Fatalf("session state = %#v", payload.Sessions[0])
	}
	if payload.Overview.TimeoutSessions != 1 {
		t.Fatalf("TimeoutSessions = %d, want 1", payload.Overview.TimeoutSessions)
	}
}

func TestNodeCredentialEndpointsStoreEncryptedSecret(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.BackendAPIKeys = []string{"panel-key"}
	cfg.Security.MasterKey = "test-master-key-with-enough-entropy"
	cfg.NodeCommand.Secret = "command-secret-command-secret"
	cfg.Session.Secret = "session-secret-session-secret"
	store := newFakeNodeStore()
	addFakePanelUser(t, store, "admin", "secret-password")
	server := NewServer(cfg, slog.Default(), "0.5.3-test", store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	cookie := loginPanel(t, mux, "admin", "secret-password")

	createReq := `{
		"node_id": "node-hk-01",
		"name": "Hong Kong 01",
		"endpoint_host": "195.245.242.9",
		"endpoint_port": 5555
	}`
	rec := serveJSONWithCookie(mux, http.MethodPost, "/api/panel/nodes", createReq, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}

	credentialReq := `{
		"auth_type": "password",
		"username": "root",
		"password": "secret-password",
		"sudo_mode": "root",
		"is_one_time": true
	}`
	rec = serveJSONWithCookie(mux, http.MethodPut, "/api/panel/nodes/node-hk-01/credential", credentialReq, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("credential status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret-password") {
		t.Fatalf("response leaked plaintext password: %s", rec.Body.String())
	}
	credential := store.creds["node-hk-01"]
	if !credential.HasPassword || credential.PasswordEncrypted == "" {
		t.Fatalf("credential missing password state: %#v", credential)
	}
	if strings.Contains(credential.PasswordEncrypted, "secret-password") {
		t.Fatalf("stored credential contains plaintext")
	}
	plaintext, err := server.secrets.Decrypt(credential.PasswordEncrypted)
	if err != nil {
		t.Fatalf("decrypt stored credential: %v", err)
	}
	if plaintext != "secret-password" {
		t.Fatalf("stored secret = %q", plaintext)
	}

	rec = serveJSONWithCookie(mux, http.MethodGet, "/api/panel/nodes/node-hk-01/credential", "", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("get credential status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"has_password":true`) {
		t.Fatalf("credential status response missing has_password: %s", rec.Body.String())
	}

	rec = serveJSONWithCookie(mux, http.MethodDelete, "/api/panel/nodes/node-hk-01/credential", "", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete credential status = %d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := store.creds["node-hk-01"]; ok {
		t.Fatalf("credential was not deleted")
	}
}

func TestDeployNodeTaskEndpointRequiresStoredHMACSecret(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.BackendAPIKeys = []string{"panel-key"}
	cfg.Security.MasterKey = "test-master-key-with-enough-entropy"
	cfg.NodeCommand.Secret = "command-secret-command-secret"
	cfg.Session.Secret = "session-secret-session-secret"
	store := newFakeNodeStore()
	addFakePanelUser(t, store, "admin", "secret-password")
	server := NewServer(cfg, slog.Default(), "0.6.4-test", store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	cookie := loginPanel(t, mux, "admin", "secret-password")

	createReq := `{
		"node_id": "node-hk-01",
		"name": "Hong Kong 01",
		"endpoint_host": "195.245.242.9",
		"endpoint_port": 5555
	}`
	rec := serveJSONWithCookie(mux, http.MethodPost, "/api/panel/nodes", createReq, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	credentialReq := `{
		"auth_type": "password",
		"username": "root",
		"password": "secret-password",
		"sudo_mode": "root",
		"is_one_time": true
	}`
	rec = serveJSONWithCookie(mux, http.MethodPut, "/api/panel/nodes/node-hk-01/credential", credentialReq, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("credential status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = serveJSONWithCookie(mux, http.MethodPost, "/api/panel/nodes/node-hk-01/deploy", `{"version":"v0.6.6"}`, cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("deploy status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "hmac_secret_required") {
		t.Fatalf("deploy response should explain missing hmac secret: %s", rec.Body.String())
	}
}

func TestUpdateNodeTaskEndpointRequiresCredential(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.BackendAPIKeys = []string{"panel-key"}
	cfg.Session.Secret = "session-secret-session-secret"
	store := newFakeNodeStore()
	addFakePanelUser(t, store, "admin", "secret-password")
	server := NewServer(cfg, slog.Default(), "0.5.4-test", store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	cookie := loginPanel(t, mux, "admin", "secret-password")

	createReq := `{
		"node_id": "node-hk-01",
		"name": "Hong Kong 01",
		"endpoint_host": "195.245.242.9",
		"endpoint_port": 5555
	}`
	rec := serveJSONWithCookie(mux, http.MethodPost, "/api/panel/nodes", createReq, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = serveJSONWithCookie(mux, http.MethodPost, "/api/panel/nodes/node-hk-01/update", `{"version":"v0.5.4"}`, cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("update status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "credential_required") {
		t.Fatalf("update response missing credential_required: %s", rec.Body.String())
	}
}

func TestRepairAdminTaskEndpointRequiresCredential(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.BackendAPIKeys = []string{"panel-key"}
	cfg.Session.Secret = "session-secret-session-secret"
	store := newFakeNodeStore()
	addFakePanelUser(t, store, "admin", "secret-password")
	server := NewServer(cfg, slog.Default(), "0.6.1-test", store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	cookie := loginPanel(t, mux, "admin", "secret-password")

	createReq := `{
		"node_id": "node-hk-01",
		"name": "Hong Kong 01",
		"endpoint_host": "195.245.242.9",
		"endpoint_port": 5555,
		"admin_host": "195.245.242.9",
		"admin_port": 5557
	}`
	rec := serveJSONWithCookie(mux, http.MethodPost, "/api/panel/nodes", createReq, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = serveJSONWithCookie(mux, http.MethodPost, "/api/panel/nodes/node-hk-01/repair-admin", `{}`, cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("repair status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "credential_required") {
		t.Fatalf("repair response missing credential_required: %s", rec.Body.String())
	}
}

func TestTuneUDPBufferTaskEndpointRequiresCredential(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.BackendAPIKeys = []string{"panel-key"}
	cfg.Session.Secret = "session-secret-session-secret"
	store := newFakeNodeStore()
	addFakePanelUser(t, store, "admin", "secret-password")
	server := NewServer(cfg, slog.Default(), "0.6.7-test", store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	cookie := loginPanel(t, mux, "admin", "secret-password")

	createReq := `{
		"node_id": "node-hk-01",
		"name": "Hong Kong 01",
		"endpoint_host": "195.245.242.9",
		"endpoint_port": 5555,
		"admin_host": "195.245.242.9",
		"admin_port": 5557
	}`
	rec := serveJSONWithCookie(mux, http.MethodPost, "/api/panel/nodes", createReq, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = serveJSONWithCookie(mux, http.MethodPost, "/api/panel/nodes/node-hk-01/tune-udp-buffer", `{}`, cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("tune status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "credential_required") {
		t.Fatalf("tune response missing credential_required: %s", rec.Body.String())
	}
}

func TestNewTuneUDPBufferTaskUsesReasonableDefaults(t *testing.T) {
	task, err := NewTuneUDPBufferTask("node-hk-01", TuneUDPBufferTaskRequest{}, time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("NewTuneUDPBufferTask: %v", err)
	}
	if task.Type != TaskTypeRestartNode {
		t.Fatalf("task type = %q, want %q", task.Type, TaskTypeRestartNode)
	}
	var req TuneUDPBufferTaskRequest
	raw := mustJSONRaw(t, task.RequestJSON)
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if req.Operation != tuneUDPBufferOperation {
		t.Fatalf("operation = %q, want %q", req.Operation, tuneUDPBufferOperation)
	}
	if req.ReceiveBufferBytes != defaultQUICUDPBufferBytes || req.SendBufferBytes != defaultQUICUDPBufferBytes {
		t.Fatalf("buffer defaults = %d/%d, want %d", req.ReceiveBufferBytes, req.SendBufferBytes, defaultQUICUDPBufferBytes)
	}
}

func TestNodeAdminListenAddressFollowsAdminHost(t *testing.T) {
	publicNode := Node{AdminHost: "195.245.242.9", AdminPort: 5557}
	if got := nodeAdminListenAddress(publicNode); got != "0.0.0.0:5557" {
		t.Fatalf("public admin listen = %q, want 0.0.0.0:5557", got)
	}
	loopbackNode := Node{AdminHost: "127.0.0.1", AdminPort: 5557}
	if got := nodeAdminListenAddress(loopbackNode); got != "127.0.0.1:5557" {
		t.Fatalf("loopback admin listen = %q, want 127.0.0.1:5557", got)
	}
	defaultPortNode := Node{AdminHost: "195.245.242.9"}
	if got := nodeAdminListenAddress(defaultPortNode); got != "0.0.0.0:5557" {
		t.Fatalf("default admin listen = %q, want 0.0.0.0:5557", got)
	}
}

func TestPolicyRevisionDesiredPolicyFlow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.BackendAPIKeys = []string{"panel-key"}
	cfg.NodeCommand.Secret = "command-secret-command-secret"
	store := newFakeNodeStore()
	server := NewServer(cfg, slog.Default(), "0.5.5-test", store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	createNodeReq := `{
		"node_id": "node-hk-01",
		"name": "Hong Kong 01",
		"endpoint_host": "195.245.242.9",
		"endpoint_port": 5555
	}`
	rec := serveJSON(mux, http.MethodPost, "/api/backend/nodes", createNodeReq, "panel-key")
	if rec.Code != http.StatusOK {
		t.Fatalf("create node status = %d body=%s", rec.Code, rec.Body.String())
	}

	policyYAML := "route_policies:\n  revision: \"20260617.1\"\n  policies: []\n"
	policyReq := map[string]string{
		"revision":            "20260617.1",
		"route_policies_yaml": policyYAML,
	}
	policyRaw, _ := json.Marshal(policyReq)
	rec = serveJSON(mux, http.MethodPost, "/api/backend/policy-revisions", string(policyRaw), "panel-key")
	if rec.Code != http.StatusOK {
		t.Fatalf("policy revision status = %d body=%s", rec.Code, rec.Body.String())
	}
	if store.policies["20260617.1"].Source != PolicySourceBackend {
		t.Fatalf("policy source = %q", store.policies["20260617.1"].Source)
	}

	desiredReq := `{"revision":"20260617.1","create_task":true,"priority":25}`
	rec = serveJSON(mux, http.MethodPost, "/api/backend/nodes/node-hk-01/desired-policy", desiredReq, "panel-key")
	if rec.Code != http.StatusOK {
		t.Fatalf("desired policy status = %d body=%s", rec.Code, rec.Body.String())
	}
	if store.nodes["node-hk-01"].DesiredPolicyRevision != "20260617.1" {
		t.Fatalf("desired policy not updated: %#v", store.nodes["node-hk-01"])
	}
	nodePolicy := store.nodePolicies[nodePolicyKey("node-hk-01", "20260617.1")]
	if !nodePolicy.Desired || nodePolicy.Applied {
		t.Fatalf("unexpected node policy state after desired set: %#v", nodePolicy)
	}
	if len(store.tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(store.tasks))
	}

	rec = serveJSON(mux, http.MethodGet, "/api/nodes/commands?node_id=node-hk-01", "", "panel-key")
	if rec.Code != http.StatusOK {
		t.Fatalf("commands status = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope panelcommand.Envelope
	if err := json.NewDecoder(rec.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode commands: %v", err)
	}
	if len(envelope.Commands) != 1 || envelope.Commands[0].Type != panelcommand.CommandApplyPolicy {
		t.Fatalf("unexpected commands: %#v", envelope.Commands)
	}
	taskID := envelope.Commands[0].ID

	reportReq := `{
		"status": "ok",
		"version": "0.5.5-local",
		"timestamp": "2026-06-17T09:00:00Z",
		"node": {"id": "node-hk-01"},
		"server": {"listen": ":5555", "alpn": "gaccel/1"},
		"route_policies": {"revision": "20260617.1", "policy_count": 0},
		"metrics": {},
		"panel_commands": [{"id": "` + taskID + `", "type": "apply_policy", "ok": true, "executed_at": "2026-06-17T09:00:00Z"}]
	}`
	rec = serveJSON(mux, http.MethodPost, "/api/nodes/report", reportReq, "panel-key")
	if rec.Code != http.StatusOK {
		t.Fatalf("report status = %d body=%s", rec.Code, rec.Body.String())
	}
	nodePolicy = store.nodePolicies[nodePolicyKey("node-hk-01", "20260617.1")]
	if !nodePolicy.Applied || nodePolicy.AppliedAt == nil {
		t.Fatalf("node policy was not marked applied: %#v", nodePolicy)
	}
	if store.tasks[taskID].Status != TaskStatusSuccess {
		t.Fatalf("task status = %q, want success", store.tasks[taskID].Status)
	}
}

func TestPolicyValidationEndpointSummarizesRoutePolicies(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.BackendAPIKeys = []string{"panel-key"}
	cfg.Session.Secret = "session-secret-session-secret"
	store := newFakeNodeStore()
	addFakePanelUser(t, store, "admin", "secret-password")
	server := NewServer(cfg, slog.Default(), "0.5.16-test", store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	cookie := loginPanel(t, mux, "admin", "secret-password")

	policyYAML := `route_policies:
  revision: "20260618.1"
  mode: "client_decision"
  policies:
    - policy_id: "steam-web-v1"
      game_id: "steam"
      allow_tcp: true
      allow_udp: true
      rules:
        - rule_id: "steam-community-tcp-443"
          network: "tcp"
          target_type: "domain_suffix"
          target_value: ".steamcommunity.com"
          port_start: 443
          port_end: 443
          action: "quic_relay"
`
	body, _ := json.Marshal(PolicyValidationRequest{
		Revision:          "20260618.1",
		RoutePoliciesYAML: policyYAML,
	})
	rec := serveJSONWithCookie(mux, http.MethodPost, "/api/panel/policy-revisions/validate", string(body), cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("validate status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload PolicyValidationResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode validation: %v", err)
	}
	if !payload.Valid {
		t.Fatalf("policy should be valid: %#v", payload.Errors)
	}
	if payload.Summary.PolicyCount != 1 || payload.Summary.RuleCount != 1 || payload.Summary.RelayRuleCount != 1 {
		t.Fatalf("unexpected summary: %#v", payload.Summary)
	}
	if payload.Summary.Mode != config.RoutePoliciesModeClientDecision {
		t.Fatalf("unexpected mode: %q", payload.Summary.Mode)
	}
	if len(payload.Summary.Games) != 1 || payload.Summary.Games[0] != "steam" {
		t.Fatalf("unexpected games: %#v", payload.Summary.Games)
	}
}

func TestPolicyValidationClientDecisionAllowsEmptyPoliciesWithoutWarning(t *testing.T) {
	resp := ValidatePolicyPackage(PolicyValidationRequest{
		Revision: "client-decision-empty",
		RoutePoliciesYAML: `route_policies:
  revision: "client-decision-empty"
  mode: "client_decision"
  policies: []
`,
	})
	if !resp.Valid {
		t.Fatalf("policy should be valid: %#v", resp.Errors)
	}
	if resp.Summary.Mode != config.RoutePoliciesModeClientDecision {
		t.Fatalf("mode = %q, want client_decision", resp.Summary.Mode)
	}
	for _, warning := range resp.Warnings {
		if strings.Contains(warning, "per-game route") {
			t.Fatalf("unexpected strict-mode warning: %#v", resp.Warnings)
		}
	}
}

func TestNodeSyncStatusEndpointReportsPolicyDrift(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.BackendAPIKeys = []string{"panel-key"}
	store := newFakeNodeStore()
	server := NewServer(cfg, slog.Default(), "0.5.17-test", store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	store.nodes["node-hk-01"] = Node{
		NodeID:                "node-hk-01",
		Name:                  "Hong Kong 01",
		EndpointHost:          "195.245.242.9",
		EndpointPort:          5555,
		AdminHost:             "127.0.0.1",
		AdminPort:             5557,
		SSHHost:               "195.245.242.9",
		SSHPort:               22,
		SSHUser:               "root",
		Status:                "online",
		CurrentPolicyRevision: "20260617.1",
		DesiredPolicyRevision: "20260618.1",
		CurrentVersion:        "v0.5.16",
		DesiredVersion:        "v0.5.16",
	}
	reportedAt := time.Date(2026, 6, 18, 10, 0, 0, 0, time.UTC)
	store.reports = append(store.reports, NodeReport{
		NodeID:              "node-hk-01",
		RoutePolicyRevision: "20260617.1",
		ReportedAt:          reportedAt,
		CreatedAt:           reportedAt,
	})
	rec := serveJSON(mux, http.MethodGet, "/api/backend/nodes/node-hk-01/sync-status", "", "panel-key")
	if rec.Code != http.StatusOK {
		t.Fatalf("sync status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		SyncStatus NodeSyncStatus `json:"sync_status"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode sync status: %v", err)
	}
	if payload.SyncStatus.PolicyState != "pending" {
		t.Fatalf("policy_state = %q, want pending", payload.SyncStatus.PolicyState)
	}
	if len(payload.SyncStatus.Recommendations) == 0 {
		t.Fatalf("expected recommendations: %#v", payload.SyncStatus)
	}
}

func TestRetryTaskEndpointCopiesTerminalTask(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.BackendAPIKeys = []string{"panel-key"}
	cfg.Session.Secret = "session-secret-session-secret"
	store := newFakeNodeStore()
	addFakePanelUser(t, store, "admin", "secret-password")
	server := NewServer(cfg, slog.Default(), "0.5.19-test", store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	cookie := loginPanel(t, mux, "admin", "secret-password")

	oldTask, err := store.CreateNodeTask(context.Background(), NodeTaskInput{
		TaskID:      "apply-policy-old",
		NodeID:      "node-hk-01",
		Type:        TaskTypeApplyPolicy,
		Status:      TaskStatusFailed,
		Priority:    25,
		RequestJSON: map[string]any{"route_policies_yaml": "route_policies:\n  revision: r1\n  policies: []\n"},
	})
	if err != nil {
		t.Fatalf("create old task: %v", err)
	}
	rec := serveJSONWithCookie(mux, http.MethodPost, "/api/panel/tasks/"+oldTask.TaskID+"/retry", `{"priority":15}`, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("retry status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Task NodeTask `json:"task"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode retry response: %v", err)
	}
	if payload.Task.TaskID == oldTask.TaskID || payload.Task.Status != TaskStatusPending || payload.Task.Priority != 15 {
		t.Fatalf("unexpected retry task: %#v", payload.Task)
	}
	if payload.Task.Type != TaskTypeApplyPolicy || payload.Task.NodeID != "node-hk-01" {
		t.Fatalf("retry task lost type/node: %#v", payload.Task)
	}
	if len(store.logs) == 0 || !strings.Contains(store.logs[len(store.logs)-1].Message, oldTask.TaskID) {
		t.Fatalf("retry log missing: %#v", store.logs)
	}
}

func TestPanelSystemCheck(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Listen = "127.0.0.1:18091"
	cfg.PublicBaseURL = "http://103.201.131.99:9788"
	cfg.Web.Root = t.TempDir()
	cfg.CORS.AllowedOrigins = []string{"http://103.201.131.99:9788"}
	cfg.Security.MasterKey = "master-key-master-key-master-key"
	cfg.Security.BackendAPIKeys = []string{"panel-key"}
	cfg.Session.Secret = "session-secret-session-secret"
	cfg.NodeCommand.Secret = "command-secret-command-secret"
	store := newFakeNodeStore()
	addFakePanelUser(t, store, "admin", "secret-password")
	server := NewServer(cfg, slog.Default(), "0.5.22-test", store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	cookie := loginPanel(t, mux, "admin", "secret-password")
	rec := serveJSONWithCookie(mux, http.MethodGet, "/api/panel/system/check", "", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("system check status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload SystemCheckResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode system check: %v", err)
	}
	if payload.Status == DiagnosticStatusError || payload.Summary.Error != 0 {
		t.Fatalf("unexpected system check payload: %#v", payload)
	}
	if payload.Config.BackendAPIKeyCount != 1 || payload.Version != "0.5.22-test" {
		t.Fatalf("system check config/version mismatch: %#v", payload.Config)
	}

	rec = serveJSON(mux, http.MethodGet, "/api/backend/system/check", "", "panel-key")
	if rec.Code != http.StatusOK {
		t.Fatalf("backend system check status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestNodeDiagnostics(t *testing.T) {
	admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
		case "/status":
			writeJSON(w, http.StatusOK, map[string]any{
				"status": "ok",
				"server": map[string]any{
					"listen": ":5555",
					"alpn":   "gaccel/1",
				},
				"node": map[string]any{
					"id": "node-hk-01",
				},
				"route_policies": map[string]any{
					"revision":     "20260618.1",
					"policy_count": 1,
				},
			})
		case "/panel/commands":
			writeJSON(w, http.StatusOK, map[string]any{"commands": []any{}})
		case "/sessions":
			writeJSON(w, http.StatusOK, map[string]any{
				"sessions": []any{
					map[string]any{
						"id":        "1",
						"user_id":   "user-1",
						"device_id": "win-device-1",
						"flows": []any{
							map[string]any{
								"flow_id":   "1",
								"game_id":   "steam",
								"policy_id": "steam-web-v1",
								"network":   "tcp",
							},
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer admin.Close()

	adminURL, err := url.Parse(admin.URL)
	if err != nil {
		t.Fatalf("parse admin url: %v", err)
	}
	adminPort, err := strconv.Atoi(adminURL.Port())
	if err != nil {
		t.Fatalf("parse admin port: %v", err)
	}

	cfg := DefaultConfig()
	cfg.Security.MasterKey = "master-key-master-key-master-key"
	cfg.Security.BackendAPIKeys = []string{"panel-key"}
	cfg.Session.Secret = "session-secret-session-secret"
	cfg.NodeCommand.Secret = "command-secret-command-secret"
	store := newFakeNodeStore()
	addFakePanelUser(t, store, "admin", "secret-password")
	reportedAt := time.Now().UTC()
	store.nodes["node-hk-01"] = Node{
		ID:                    1,
		NodeID:                "node-hk-01",
		Name:                  "HK 01",
		EndpointHost:          "195.245.242.9",
		EndpointPort:          5555,
		ALPN:                  "gaccel/1",
		AdminHost:             adminURL.Hostname(),
		AdminPort:             adminPort,
		AllowTCP:              true,
		AllowUDP:              true,
		HMACSecretConfigured:  true,
		HMACSecretSource:      "backend",
		Status:                "online",
		CurrentPolicyRevision: "20260618.1",
		DesiredPolicyRevision: "20260618.1",
		LastReportAt:          &reportedAt,
	}
	server := NewServer(cfg, slog.Default(), "0.5.23-test", store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	cookie := loginPanel(t, mux, "admin", "secret-password")
	rec := serveJSONWithCookie(mux, http.MethodGet, "/api/panel/nodes/node-hk-01/diagnostics", "", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("diagnostics status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload NodeDiagnosticsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode diagnostics: %v", err)
	}
	if payload.NodeID != "node-hk-01" || payload.Summary.Error != 0 {
		t.Fatalf("unexpected diagnostics payload: %#v", payload)
	}
	if !strings.Contains(payload.AdminURL, adminURL.Hostname()) {
		t.Fatalf("admin url mismatch: %s", payload.AdminURL)
	}
}

func serveJSON(mux http.Handler, method string, path string, body string, apiKey string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func serveJSONWithCookie(mux http.Handler, method string, path string, body string, cookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		if strings.HasPrefix(cookie, "Bearer ") {
			req.Header.Set("Authorization", cookie)
		} else {
			req.Header.Set("Cookie", cookie)
		}
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func addFakePanelUser(t *testing.T, store *fakeNodeStore, username string, password string) PanelUser {
	return addFakePanelUserWithRole(t, store, username, password, PanelUserRoleAdmin)
}

func addFakePanelUserWithRole(t *testing.T, store *fakeNodeStore, username string, password string, role string) PanelUser {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("generate test password hash: %v", err)
	}
	normalizedRole, err := normalizePanelRole(role)
	if err != nil {
		t.Fatalf("normalize test role: %v", err)
	}
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	user := PanelUser{
		ID:           uint64(len(store.users) + 1),
		Username:     username,
		PasswordHash: string(hash),
		Role:         normalizedRole,
		Status:       PanelUserStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	store.users[user.ID] = user
	store.usersByName[user.Username] = user.ID
	return user
}

func loginPanel(t *testing.T, mux http.Handler, username string, password string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	if err != nil {
		t.Fatalf("marshal login request: %v", err)
	}
	rec := serveJSON(mux, http.MethodPost, "/api/panel/login", string(raw), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		AccessToken string `json:"access_token"`
		Token       string `json:"token"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode login response: %v body=%s", err, rec.Body.String())
	}
	token := payload.AccessToken
	if token == "" {
		token = payload.Token
	}
	if token == "" || payload.TokenType != "Bearer" {
		t.Fatalf("login response did not return bearer token: %s", rec.Body.String())
	}
	return "Bearer " + token
}

func nodePolicyKey(nodeID string, revision string) string {
	return nodeID + "\x00" + revision
}

func clientSessionKey(nodeID string, sessionID string) string {
	return nodeID + "\x00" + sessionID
}

func clientSessionFromSnapshot(nodeID string, snapshot sessions.Snapshot, now time.Time) ClientSession {
	return ClientSession{
		NodeID:                 nodeID,
		SessionID:              snapshot.ID,
		RemoteAddr:             snapshot.RemoteAddr,
		UserID:                 snapshot.UserID,
		DeviceID:               snapshot.DeviceID,
		ClientID:               snapshot.ClientID,
		ClientVersion:          snapshot.ClientVersion,
		ClientPlatform:         snapshot.ClientPlatform,
		ProtocolVersion:        snapshot.ProtocolVersion,
		Status:                 ClientSessionStatusOnline,
		GameIDs:                nonNilStrings(snapshot.GameIDs),
		PolicyIDs:              nonNilStrings(snapshot.PolicyIDs),
		ConfigRevision:         snapshot.ConfigRevision,
		ConnectedAt:            snapshot.CreatedAt,
		AuthenticatedAt:        snapshot.AuthenticatedAt,
		LastSeenAt:             snapshot.LastSeen,
		LastPingAt:             snapshot.LastPingAt,
		DurationSeconds:        snapshot.ConnectedDurationSeconds,
		MaxConnections:         snapshot.MaxConns,
		RateLimitMbps:          snapshot.RateLimitMbps,
		AllowTCP:               snapshot.AllowTCP,
		AllowUDP:               snapshot.AllowUDP,
		UDPFlows:               snapshot.UDPFlows,
		TCPFlows:               snapshot.TCPFlows,
		UDPClientToTargetBytes: snapshot.UDPClientToTargetBytes,
		UDPTargetToClientBytes: snapshot.UDPTargetToClientBytes,
		TCPClientToTargetBytes: snapshot.TCPClientToTargetBytes,
		TCPTargetToClientBytes: snapshot.TCPTargetToClientBytes,
		CreatedAt:              now,
		UpdatedAt:              now,
	}
}

func clientSessionFromEvent(nodeID string, event sessions.Event, now time.Time) ClientSession {
	status := event.Status
	if status == "" {
		status = ClientSessionStatusOnline
	}
	connectedAt := event.ConnectedAt
	if connectedAt.IsZero() {
		connectedAt = now
	}
	lastSeenAt := event.LastSeenAt
	if lastSeenAt.IsZero() {
		lastSeenAt = now
	}
	return ClientSession{
		NodeID:                 nodeID,
		SessionID:              event.SessionID,
		RemoteAddr:             event.RemoteAddr,
		UserID:                 event.UserID,
		DeviceID:               event.DeviceID,
		ClientID:               event.ClientID,
		ClientVersion:          event.ClientVersion,
		ClientPlatform:         event.ClientPlatform,
		ProtocolVersion:        event.ProtocolVersion,
		Status:                 status,
		CloseReason:            event.CloseReason,
		CloseSource:            event.CloseSource,
		GameIDs:                nonNilStrings(event.GameIDs),
		PolicyIDs:              nonNilStrings(event.PolicyIDs),
		ConfigRevision:         event.ConfigRevision,
		ConnectedAt:            connectedAt,
		AuthenticatedAt:        event.AuthenticatedAt,
		LastSeenAt:             lastSeenAt,
		LastPingAt:             event.LastPingAt,
		EndedAt:                event.EndedAt,
		DurationSeconds:        event.DurationSeconds,
		AllowTCP:               true,
		AllowUDP:               true,
		UDPFlows:               event.UDPFlows,
		TCPFlows:               event.TCPFlows,
		UDPClientToTargetBytes: event.UDPClientToTargetBytes,
		UDPTargetToClientBytes: event.UDPTargetToClientBytes,
		TCPClientToTargetBytes: event.TCPClientToTargetBytes,
		TCPTargetToClientBytes: event.TCPTargetToClientBytes,
		CreatedAt:              now,
		UpdatedAt:              now,
	}
}

type fakeNodeStore struct {
	nodes          map[string]Node
	audit          []AuditLog
	reports        []NodeReport
	tasks          map[string]NodeTask
	logs           []NodeTaskLog
	creds          map[string]NodeCredential
	policies       map[string]PolicyRevision
	nodePolicies   map[string]NodePolicyRevision
	clientSessions map[string]ClientSession
	tokenDefaults  *TokenDefaults
	users          map[uint64]PanelUser
	usersByName    map[string]uint64
}

func newFakeNodeStore() *fakeNodeStore {
	return &fakeNodeStore{
		nodes:          make(map[string]Node),
		tasks:          make(map[string]NodeTask),
		creds:          make(map[string]NodeCredential),
		policies:       make(map[string]PolicyRevision),
		nodePolicies:   make(map[string]NodePolicyRevision),
		clientSessions: make(map[string]ClientSession),
		users:          make(map[uint64]PanelUser),
		usersByName:    make(map[string]uint64),
	}
}

func (s *fakeNodeStore) Ping(_ context.Context) error {
	return nil
}

func (s *fakeNodeStore) CheckRequiredTables(_ context.Context, tables []string) ([]SchemaTableCheck, error) {
	result := make([]SchemaTableCheck, 0, len(tables))
	for _, table := range tables {
		result = append(result, SchemaTableCheck{Name: table, Exists: true})
	}
	return result, nil
}

func (s *fakeNodeStore) GetPanelUserByID(_ context.Context, id uint64) (*PanelUser, error) {
	user, ok := s.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &user, nil
}

func (s *fakeNodeStore) GetPanelUserByUsername(_ context.Context, username string) (*PanelUser, error) {
	id, ok := s.usersByName[strings.TrimSpace(username)]
	if !ok {
		return nil, ErrNotFound
	}
	return s.GetPanelUserByID(context.Background(), id)
}

func (s *fakeNodeStore) ListPanelUsers(_ context.Context) ([]PanelUser, error) {
	users := make([]PanelUser, 0, len(s.users))
	for _, user := range s.users {
		users = append(users, user)
	}
	sort.Slice(users, func(i, j int) bool {
		return users[i].ID < users[j].ID
	})
	return users, nil
}

func (s *fakeNodeStore) CreatePanelUser(_ context.Context, username string, passwordHash string, role string, status string) (*PanelUser, error) {
	username = strings.TrimSpace(username)
	if err := validatePanelUsername(username); err != nil {
		return nil, err
	}
	if strings.TrimSpace(passwordHash) == "" {
		return nil, errors.New("password hash is required")
	}
	normalizedRole, err := normalizePanelRole(role)
	if err != nil {
		return nil, err
	}
	normalizedStatus, err := normalizePanelUserStatus(status)
	if err != nil {
		return nil, err
	}
	if _, ok := s.usersByName[username]; ok {
		return nil, ErrAlreadyExists
	}
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	user := PanelUser{
		ID:           uint64(len(s.users) + 1),
		Username:     username,
		PasswordHash: passwordHash,
		Role:         normalizedRole,
		Status:       normalizedStatus,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	s.users[user.ID] = user
	s.usersByName[user.Username] = user.ID
	return &user, nil
}

func (s *fakeNodeStore) UpdatePanelUser(_ context.Context, id uint64, role string, status string) (*PanelUser, error) {
	user, ok := s.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	normalizedRole, err := normalizePanelRole(role)
	if err != nil {
		return nil, err
	}
	normalizedStatus, err := normalizePanelUserStatus(status)
	if err != nil {
		return nil, err
	}
	user.Role = normalizedRole
	user.Status = normalizedStatus
	user.UpdatedAt = time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	s.users[id] = user
	return &user, nil
}

func (s *fakeNodeStore) UpdatePanelUserPassword(_ context.Context, id uint64, passwordHash string) (*PanelUser, error) {
	user, ok := s.users[id]
	if !ok || user.Status != PanelUserStatusActive {
		return nil, ErrNotFound
	}
	user.PasswordHash = passwordHash
	user.UpdatedAt = time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	s.users[id] = user
	return &user, nil
}

func (s *fakeNodeStore) ListNodes(_ context.Context, filter NodeListFilter) ([]Node, error) {
	nodes := make([]Node, 0, len(s.nodes))
	for _, node := range s.nodes {
		if filter.Query != "" && node.NodeID != filter.Query && node.Region != filter.Query {
			continue
		}
		if filter.Status != "" && node.Status != filter.Status {
			continue
		}
		if filter.Region != "" && node.Region != filter.Region {
			continue
		}
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].NodeID < nodes[j].NodeID
	})
	return nodes, nil
}

func (s *fakeNodeStore) GetNode(_ context.Context, nodeID string) (*Node, error) {
	node, ok := s.nodes[nodeID]
	if !ok {
		return nil, ErrNotFound
	}
	return &node, nil
}

func (s *fakeNodeStore) UpsertNode(_ context.Context, node Node) (*Node, error) {
	if err := ValidateNode(node); err != nil {
		return nil, err
	}
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	if existing, ok := s.nodes[node.NodeID]; ok {
		node.ID = existing.ID
		node.CreatedAt = existing.CreatedAt
		if strings.TrimSpace(node.HMACSecretEncrypted) == "" {
			node.HMACSecretEncrypted = existing.HMACSecretEncrypted
			node.HMACSecretConfigured = existing.HMACSecretConfigured
			node.HMACSecretSource = existing.HMACSecretSource
			node.HMACSecretUpdatedAt = existing.HMACSecretUpdatedAt
		}
	} else {
		node.ID = uint64(len(s.nodes) + 1)
		node.CreatedAt = now
	}
	node.HMACSecretConfigured = strings.TrimSpace(node.HMACSecretEncrypted) != ""
	node.UpdatedAt = now
	s.nodes[node.NodeID] = node
	return &node, nil
}

func (s *fakeNodeStore) SetNodeHMACSecret(_ context.Context, nodeID string, encryptedSecret string, source string, updatedAt time.Time) (*Node, error) {
	node, ok := s.nodes[nodeID]
	if !ok {
		return nil, ErrNotFound
	}
	node.HMACSecretEncrypted = strings.TrimSpace(encryptedSecret)
	node.HMACSecretConfigured = node.HMACSecretEncrypted != ""
	node.HMACSecretSource = strings.TrimSpace(source)
	node.HMACSecretUpdatedAt = &updatedAt
	node.UpdatedAt = updatedAt
	s.nodes[nodeID] = node
	return &node, nil
}

func (s *fakeNodeStore) ClearNodeHMACSecret(_ context.Context, nodeID string) (*Node, error) {
	node, ok := s.nodes[nodeID]
	if !ok {
		return nil, ErrNotFound
	}
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	node.HMACSecretEncrypted = ""
	node.HMACSecretConfigured = false
	node.HMACSecretSource = ""
	node.HMACSecretUpdatedAt = nil
	node.UpdatedAt = now
	s.nodes[nodeID] = node
	return &node, nil
}

func (s *fakeNodeStore) DeleteNode(_ context.Context, nodeID string) error {
	if _, ok := s.nodes[nodeID]; !ok {
		return ErrNotFound
	}
	delete(s.nodes, nodeID)
	return nil
}

func (s *fakeNodeStore) UpsertPolicyRevision(_ context.Context, input PolicyRevisionInput) (*PolicyRevision, error) {
	policy, err := NewPolicyRevisionFromInput(input)
	if err != nil {
		return nil, err
	}
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	if existing, ok := s.policies[policy.Revision]; ok {
		policy.ID = existing.ID
		policy.CreatedAt = existing.CreatedAt
	} else {
		policy.ID = uint64(len(s.policies) + 1)
		policy.CreatedAt = now
	}
	s.policies[policy.Revision] = policy
	return &policy, nil
}

func (s *fakeNodeStore) ListPolicyRevisions(_ context.Context, limit int) ([]PolicyRevision, error) {
	if limit <= 0 {
		limit = 50
	}
	policies := make([]PolicyRevision, 0, len(s.policies))
	for _, policy := range s.policies {
		policies = append(policies, policy)
	}
	sort.Slice(policies, func(i, j int) bool {
		return policies[i].ID > policies[j].ID
	})
	if len(policies) > limit {
		policies = policies[:limit]
	}
	return policies, nil
}

func (s *fakeNodeStore) GetPolicyRevision(_ context.Context, revision string) (*PolicyRevision, error) {
	policy, ok := s.policies[strings.TrimSpace(revision)]
	if !ok {
		return nil, ErrNotFound
	}
	return &policy, nil
}

func (s *fakeNodeStore) SetNodeDesiredPolicy(_ context.Context, nodeID string, revision string, now time.Time) (*NodePolicyRevision, error) {
	node, ok := s.nodes[nodeID]
	if !ok {
		return nil, ErrNotFound
	}
	if _, ok := s.policies[revision]; !ok {
		return nil, ErrNotFound
	}
	if now.IsZero() {
		now = time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	}
	node.DesiredPolicyRevision = revision
	node.UpdatedAt = now
	s.nodes[nodeID] = node
	for key, nodePolicy := range s.nodePolicies {
		if nodePolicy.NodeID == nodeID {
			nodePolicy.Desired = false
			nodePolicy.UpdatedAt = now
			s.nodePolicies[key] = nodePolicy
		}
	}
	key := nodePolicyKey(nodeID, revision)
	nodePolicy := s.nodePolicies[key]
	if nodePolicy.ID == 0 {
		nodePolicy.ID = uint64(len(s.nodePolicies) + 1)
		nodePolicy.CreatedAt = now
	}
	nodePolicy.NodeID = nodeID
	nodePolicy.Revision = revision
	nodePolicy.Desired = true
	nodePolicy.Applied = node.CurrentPolicyRevision == revision
	if nodePolicy.Applied {
		nodePolicy.AppliedAt = &now
	} else {
		nodePolicy.AppliedAt = nil
	}
	nodePolicy.LastError = ""
	nodePolicy.UpdatedAt = now
	s.nodePolicies[key] = nodePolicy
	return &nodePolicy, nil
}

func (s *fakeNodeStore) GetNodePolicyRevision(_ context.Context, nodeID string, revision string) (*NodePolicyRevision, error) {
	nodePolicy, ok := s.nodePolicies[nodePolicyKey(nodeID, revision)]
	if !ok {
		return nil, ErrNotFound
	}
	return &nodePolicy, nil
}

func (s *fakeNodeStore) GetTokenDefaults(_ context.Context) (*TokenDefaults, error) {
	if s.tokenDefaults == nil {
		defaults := DefaultTokenDefaults()
		return &defaults, nil
	}
	defaults := *s.tokenDefaults
	defaults.Plans = append([]TokenPlanDefault(nil), s.tokenDefaults.Plans...)
	return &defaults, nil
}

func (s *fakeNodeStore) SaveTokenDefaults(_ context.Context, input TokenDefaultsInput) (*TokenDefaults, error) {
	normalized, err := NormalizeTokenDefaults(input)
	if err != nil {
		return nil, err
	}
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	plans := append([]TokenPlanDefault(nil), normalized.Plans...)
	for i := range plans {
		plans[i].UpdatedAt = &now
	}
	defaults := BuildTokenDefaults(plans)
	s.tokenDefaults = &defaults
	return &defaults, nil
}

func (s *fakeNodeStore) SaveNodeReport(_ context.Context, input NodeReportInput) (*NodeReport, error) {
	payload := input.Payload
	nodeID := strings.TrimSpace(payload.Node.ID)
	if nodeID == "" {
		return nil, errors.New("node.id is required")
	}
	node, ok := s.nodes[nodeID]
	if !ok {
		return nil, ErrNotFound
	}
	reportedAt := payload.Timestamp
	if reportedAt.IsZero() {
		reportedAt = time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	}
	node.Status = "online"
	if payload.Status != "" && payload.Status != "ok" {
		node.Status = "error"
	}
	node.CurrentVersion = payload.Version
	node.CurrentPolicyRevision = payload.RoutePolicies.Revision
	node.LastReportAt = &reportedAt
	node.LastError = latestCommandError(payload.PanelCommands)
	node.UpdatedAt = reportedAt
	s.nodes[nodeID] = node

	metricsRaw, err := jsonRaw(payload.Metrics)
	if err != nil {
		return nil, err
	}
	commandsRaw, err := jsonRaw(payload.PanelCommands)
	if err != nil {
		return nil, err
	}
	report := NodeReport{
		ID:                    uint64(len(s.reports) + 1),
		NodeID:                nodeID,
		Version:               payload.Version,
		Status:                payload.Status,
		RoutePolicyRevision:   payload.RoutePolicies.Revision,
		RoutePolicyCount:      payload.RoutePolicies.PolicyCount,
		ActiveQUICConnections: payload.Metrics.ActiveQUICConnections,
		ActiveTCPFlows:        payload.Metrics.ActiveTCPFlows,
		ActiveUDPFlows:        payload.Metrics.ActiveUDPFlows,
		Metrics:               metricsRaw,
		PanelCommands:         commandsRaw,
		Raw:                   input.RawJSON,
		ReportedAt:            reportedAt,
		CreatedAt:             reportedAt,
	}
	s.reports = append(s.reports, report)
	for _, result := range payload.PanelCommands {
		task := s.tasks[result.ID]
		if task.TaskID == "" || task.NodeID != nodeID {
			continue
		}
		task.Status = TaskStatusSuccess
		if !result.OK {
			task.Status = TaskStatusFailed
		}
		task.ErrorMessage = result.Error
		resultRaw, err := jsonRaw(result)
		if err != nil {
			return nil, err
		}
		task.ResultJSON = resultRaw
		executedAt := result.ExecutedAt
		if executedAt.IsZero() {
			executedAt = reportedAt
		}
		task.FinishedAt = &executedAt
		task.UpdatedAt = executedAt
		s.tasks[result.ID] = task
	}
	if payload.RoutePolicies.Revision != "" {
		key := nodePolicyKey(nodeID, payload.RoutePolicies.Revision)
		if nodePolicy, ok := s.nodePolicies[key]; ok {
			nodePolicy.Applied = true
			nodePolicy.AppliedAt = &reportedAt
			nodePolicy.LastError = ""
			nodePolicy.UpdatedAt = reportedAt
			s.nodePolicies[key] = nodePolicy
		}
	}
	for _, snapshot := range payload.Sessions {
		if strings.TrimSpace(snapshot.ID) == "" {
			continue
		}
		s.clientSessions[clientSessionKey(nodeID, snapshot.ID)] = clientSessionFromSnapshot(nodeID, snapshot, reportedAt)
	}
	for _, event := range payload.SessionEvents {
		if strings.TrimSpace(event.SessionID) == "" {
			continue
		}
		key := clientSessionKey(nodeID, event.SessionID)
		session := s.clientSessions[key]
		if session.SessionID == "" {
			session = clientSessionFromEvent(nodeID, event, reportedAt)
		}
		session.Status = event.Status
		if session.Status == "" {
			session.Status = ClientSessionStatusOnline
		}
		session.CloseReason = event.CloseReason
		session.CloseSource = event.CloseSource
		session.LastSeenAt = event.LastSeenAt
		if session.LastSeenAt.IsZero() {
			session.LastSeenAt = reportedAt
		}
		session.LastPingAt = event.LastPingAt
		session.EndedAt = event.EndedAt
		session.DurationSeconds = event.DurationSeconds
		session.UDPClientToTargetBytes = event.UDPClientToTargetBytes
		session.UDPTargetToClientBytes = event.UDPTargetToClientBytes
		session.TCPClientToTargetBytes = event.TCPClientToTargetBytes
		session.TCPTargetToClientBytes = event.TCPTargetToClientBytes
		session.UpdatedAt = reportedAt
		s.clientSessions[key] = session
	}
	return &report, nil
}

func (s *fakeNodeStore) ListNodeReports(_ context.Context, nodeID string, limit int) ([]NodeReport, error) {
	if limit <= 0 {
		limit = 20
	}
	reports := make([]NodeReport, 0)
	for i := len(s.reports) - 1; i >= 0 && len(reports) < limit; i-- {
		if s.reports[i].NodeID == nodeID {
			reports = append(reports, s.reports[i])
		}
	}
	return reports, nil
}

func (s *fakeNodeStore) GetTrafficOverview(_ context.Context, filter TrafficOverviewFilter) (*TrafficOverview, error) {
	nodes := make([]Node, 0, len(s.nodes))
	for _, node := range s.nodes {
		nodes = append(nodes, node)
	}
	samples := make([]trafficReportSample, 0, len(s.reports))
	for _, report := range s.reports {
		var snapshot metrics.Snapshot
		if len(report.Metrics) > 0 {
			if err := json.Unmarshal(report.Metrics, &snapshot); err != nil {
				return nil, err
			}
		}
		samples = append(samples, trafficReportSample{
			NodeID:              report.NodeID,
			Version:             report.Version,
			RoutePolicyRevision: report.RoutePolicyRevision,
			Metrics:             snapshot,
			ReportedAt:          report.ReportedAt,
		})
	}
	overview := BuildTrafficOverview(nodes, samples, filter)
	return &overview, nil
}

func (s *fakeNodeStore) ListClientSessions(_ context.Context, filter ClientSessionFilter) (*ClientSessionList, error) {
	filter = normalizeClientSessionFilter(filter)
	items := make([]ClientSession, 0, len(s.clientSessions))
	var overview ClientSessionOverview
	for _, session := range s.clientSessions {
		if filter.NodeID != "" && session.NodeID != filter.NodeID {
			continue
		}
		if filter.UserID != "" && session.UserID != filter.UserID {
			continue
		}
		if filter.DeviceID != "" && session.DeviceID != filter.DeviceID {
			continue
		}
		if filter.Status != "" && session.Status != filter.Status {
			continue
		}
		if filter.CloseReason != "" && session.CloseReason != filter.CloseReason {
			continue
		}
		items = append(items, session)
		overview.TotalSessions++
		if session.Status == ClientSessionStatusOnline {
			overview.OnlineSessions++
		}
		if session.Status == ClientSessionStatusClosed {
			overview.ClosedSessions++
		}
		if session.CloseReason == "heartbeat_timeout" || session.CloseReason == "quic_idle_timeout" {
			overview.TimeoutSessions++
		}
		overview.TotalDurationSeconds += session.DurationSeconds
		overview.UDPClientToTargetBytes += session.UDPClientToTargetBytes
		overview.UDPTargetToClientBytes += session.UDPTargetToClientBytes
		overview.TCPClientToTargetBytes += session.TCPClientToTargetBytes
		overview.TCPTargetToClientBytes += session.TCPTargetToClientBytes
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].LastSeenAt.After(items[j].LastSeenAt)
	})
	if filter.Offset < len(items) {
		items = items[filter.Offset:]
	} else {
		items = []ClientSession{}
	}
	if len(items) > filter.Limit {
		items = items[:filter.Limit]
	}
	return &ClientSessionList{
		Sessions: items,
		Overview: overview,
		Limit:    filter.Limit,
		Offset:   filter.Offset,
	}, nil
}

func (s *fakeNodeStore) CreateNodeTask(_ context.Context, input NodeTaskInput) (*NodeTask, error) {
	input = normalizeTaskInput(input)
	if err := validateTaskInput(input); err != nil {
		return nil, err
	}
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	requestRaw, err := jsonRaw(input.RequestJSON)
	if err != nil {
		return nil, err
	}
	task := NodeTask{
		ID:          uint64(len(s.tasks) + 1),
		TaskID:      input.TaskID,
		NodeID:      input.NodeID,
		Type:        input.Type,
		Status:      input.Status,
		Priority:    input.Priority,
		RequestJSON: requestRaw,
		QueuedAt:    now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	s.tasks[task.TaskID] = task
	return &task, nil
}

func (s *fakeNodeStore) ListNodeTasks(_ context.Context, nodeID string, limit int) ([]NodeTask, error) {
	if limit <= 0 {
		limit = 20
	}
	tasks := make([]NodeTask, 0)
	for _, task := range s.tasks {
		if task.NodeID == nodeID {
			tasks = append(tasks, task)
		}
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].QueuedAt.After(tasks[j].QueuedAt)
	})
	if len(tasks) > limit {
		tasks = tasks[:limit]
	}
	return tasks, nil
}

func (s *fakeNodeStore) GetNodeTask(_ context.Context, taskID string) (*NodeTask, error) {
	task, ok := s.tasks[taskID]
	if !ok {
		return nil, ErrNotFound
	}
	return &task, nil
}

func (s *fakeNodeStore) ClaimPendingNodeTasks(_ context.Context, nodeID string, limit int, now time.Time) ([]NodeTask, error) {
	if limit <= 0 {
		limit = 10
	}
	tasks := make([]NodeTask, 0)
	for _, task := range s.tasks {
		if task.NodeID == nodeID && task.Status == TaskStatusPending {
			tasks = append(tasks, task)
		}
	}
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].Priority != tasks[j].Priority {
			return tasks[i].Priority < tasks[j].Priority
		}
		return tasks[i].QueuedAt.Before(tasks[j].QueuedAt)
	})
	if len(tasks) > limit {
		tasks = tasks[:limit]
	}
	for i := range tasks {
		task := tasks[i]
		task.Status = TaskStatusRunning
		task.StartedAt = &now
		task.UpdatedAt = now
		s.tasks[task.TaskID] = task
		tasks[i] = task
	}
	return tasks, nil
}

func (s *fakeNodeStore) UpdateNodeTask(_ context.Context, update NodeTaskUpdate) (*NodeTask, error) {
	task, ok := s.tasks[update.TaskID]
	if !ok {
		return nil, ErrNotFound
	}
	if !isAllowedTaskStatus(update.Status) {
		return nil, errors.New("invalid task status")
	}
	task.Status = update.Status
	if update.ResultJSON != nil {
		raw, err := jsonRaw(update.ResultJSON)
		if err != nil {
			return nil, err
		}
		task.ResultJSON = raw
	}
	task.ErrorMessage = update.ErrorMessage
	if update.StartedAt != nil {
		task.StartedAt = update.StartedAt
	}
	if update.FinishedAt != nil {
		task.FinishedAt = update.FinishedAt
	}
	task.UpdatedAt = time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	s.tasks[task.TaskID] = task
	return &task, nil
}

func (s *fakeNodeStore) AppendNodeTaskLog(_ context.Context, input NodeTaskLogInput) (*NodeTaskLog, error) {
	input = normalizeTaskLogInput(input)
	if err := validateTaskLogInput(input); err != nil {
		return nil, err
	}
	log := NodeTaskLog{
		ID:        uint64(len(s.logs) + 1),
		TaskID:    input.TaskID,
		NodeID:    input.NodeID,
		Step:      input.Step,
		Stream:    input.Stream,
		Message:   input.Message,
		CreatedAt: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
	}
	s.logs = append(s.logs, log)
	return &log, nil
}

func (s *fakeNodeStore) ListNodeTaskLogs(_ context.Context, taskID string, limit int) ([]NodeTaskLog, error) {
	if limit <= 0 {
		limit = 200
	}
	logs := make([]NodeTaskLog, 0)
	for _, log := range s.logs {
		if log.TaskID == taskID {
			logs = append(logs, log)
		}
	}
	if len(logs) > limit {
		logs = logs[:limit]
	}
	return logs, nil
}

func (s *fakeNodeStore) UpsertNodeCredential(_ context.Context, input NodeCredentialInput) (*NodeCredential, error) {
	if _, ok := s.nodes[input.NodeID]; !ok {
		return nil, ErrNotFound
	}
	if err := validateCredentialInput(input); err != nil {
		return nil, err
	}
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	credential := NodeCredential{
		ID:                            uint64(len(s.creds) + 1),
		NodeID:                        input.NodeID,
		AuthType:                      input.AuthType,
		Username:                      input.Username,
		SudoMode:                      input.SudoMode,
		IsOneTime:                     input.IsOneTime,
		HasPassword:                   input.PasswordEncrypted != "",
		HasPrivateKey:                 input.PrivateKeyEncrypted != "",
		HasPrivatePassphrase:          input.PrivateKeyPassphraseEncrypted != "",
		PasswordEncrypted:             input.PasswordEncrypted,
		PrivateKeyEncrypted:           input.PrivateKeyEncrypted,
		PrivateKeyPassphraseEncrypted: input.PrivateKeyPassphraseEncrypted,
		CreatedAt:                     now,
		UpdatedAt:                     now,
	}
	if existing, ok := s.creds[input.NodeID]; ok {
		credential.ID = existing.ID
		credential.CreatedAt = existing.CreatedAt
	}
	s.creds[input.NodeID] = credential
	return &credential, nil
}

func (s *fakeNodeStore) GetNodeCredential(_ context.Context, nodeID string) (*NodeCredential, error) {
	credential, ok := s.creds[nodeID]
	if !ok {
		return nil, ErrNotFound
	}
	return &credential, nil
}

func (s *fakeNodeStore) DeleteNodeCredential(_ context.Context, nodeID string) error {
	if _, ok := s.creds[nodeID]; !ok {
		return ErrNotFound
	}
	delete(s.creds, nodeID)
	return nil
}

func (s *fakeNodeStore) MarkNodeCredentialUsed(_ context.Context, nodeID string, usedAt time.Time) error {
	credential, ok := s.creds[nodeID]
	if !ok {
		return ErrNotFound
	}
	credential.LastUsedAt = &usedAt
	credential.UpdatedAt = usedAt
	s.creds[nodeID] = credential
	return nil
}

func (s *fakeNodeStore) UpdateNodeOperationalState(_ context.Context, nodeID string, status string, currentVersion string, lastError string) error {
	node, ok := s.nodes[nodeID]
	if !ok {
		return ErrNotFound
	}
	if status != "" {
		node.Status = status
	}
	if currentVersion != "" {
		node.CurrentVersion = currentVersion
	}
	node.LastError = lastError
	node.UpdatedAt = time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	s.nodes[nodeID] = node
	return nil
}

func (s *fakeNodeStore) RecordAudit(_ context.Context, entry AuditLog) error {
	s.audit = append(s.audit, entry)
	return nil
}
