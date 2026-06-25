package panel

import (
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	TokenDefaultNodeHardLimit    = 512
	tokenDefaultMaxPlanCount     = 16
	tokenDefaultMaxRateLimitMbps = 10000
)

var tokenDefaultPlanIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

type TokenPlanDefault struct {
	PlanID         string     `json:"plan_id"`
	Name           string     `json:"name"`
	MaxConnections int        `json:"max_connections"`
	RateLimitMbps  int        `json:"rate_limit_mbps"`
	AllowTCP       bool       `json:"allow_tcp"`
	AllowUDP       bool       `json:"allow_udp"`
	Description    string     `json:"description,omitempty"`
	SortOrder      int        `json:"sort_order"`
	UpdatedAt      *time.Time `json:"updated_at,omitempty"`
}

type TokenDefaults struct {
	NodeHardLimit int                `json:"node_hard_limit"`
	Plans         []TokenPlanDefault `json:"plans"`
	UpdatedAt     *time.Time         `json:"updated_at,omitempty"`
}

type TokenDefaultsInput struct {
	Plans []TokenPlanDefault `json:"plans"`
}

func DefaultTokenDefaults() TokenDefaults {
	return TokenDefaults{
		NodeHardLimit: TokenDefaultNodeHardLimit,
		Plans: []TokenPlanDefault{
			{
				PlanID:         "trial",
				Name:           "免费/测试",
				MaxConnections: 32,
				RateLimitMbps:  50,
				AllowTCP:       true,
				AllowUDP:       true,
				Description:    "短时测试、体验用户和低并发调试。",
				SortOrder:      10,
			},
			{
				PlanID:         "standard",
				Name:           "普通",
				MaxConnections: 64,
				RateLimitMbps:  100,
				AllowTCP:       true,
				AllowUDP:       true,
				Description:    "默认游戏加速档位，适合 Steam 商店、社区和常规在线游戏。",
				SortOrder:      20,
			},
			{
				PlanID:         "advanced",
				Name:           "高级",
				MaxConnections: 128,
				RateLimitMbps:  200,
				AllowTCP:       true,
				AllowUDP:       true,
				Description:    "推荐给 Steam 客户端联调和多连接游戏场景。",
				SortOrder:      30,
			},
			{
				PlanID:         "premium",
				Name:           "旗舰",
				MaxConnections: 256,
				RateLimitMbps:  500,
				AllowTCP:       true,
				AllowUDP:       true,
				Description:    "高并发、多游戏下载和重度游戏加速档位。",
				SortOrder:      40,
			},
		},
	}
}

func NormalizeTokenDefaults(input TokenDefaultsInput) (TokenDefaultsInput, error) {
	if len(input.Plans) == 0 {
		defaults := DefaultTokenDefaults()
		input.Plans = defaults.Plans
	}
	if len(input.Plans) > tokenDefaultMaxPlanCount {
		return TokenDefaultsInput{}, errors.New("plans exceeds the maximum count")
	}
	seen := make(map[string]struct{}, len(input.Plans))
	plans := make([]TokenPlanDefault, 0, len(input.Plans))
	for index, plan := range input.Plans {
		plan.PlanID = strings.ToLower(strings.TrimSpace(plan.PlanID))
		plan.Name = strings.TrimSpace(plan.Name)
		plan.Description = strings.TrimSpace(plan.Description)
		if plan.SortOrder == 0 {
			plan.SortOrder = (index + 1) * 10
		}
		if !tokenDefaultPlanIDPattern.MatchString(plan.PlanID) {
			return TokenDefaultsInput{}, errors.New("plan_id must be 1-64 lowercase letters, numbers, '-' or '_'")
		}
		if _, ok := seen[plan.PlanID]; ok {
			return TokenDefaultsInput{}, errors.New("plan_id must be unique")
		}
		seen[plan.PlanID] = struct{}{}
		if plan.Name == "" || len([]rune(plan.Name)) > 64 {
			return TokenDefaultsInput{}, errors.New("name must be 1-64 chars")
		}
		if plan.MaxConnections < 1 || plan.MaxConnections > TokenDefaultNodeHardLimit {
			return TokenDefaultsInput{}, errors.New("max_connections must be between 1 and 512")
		}
		if plan.RateLimitMbps < 1 || plan.RateLimitMbps > tokenDefaultMaxRateLimitMbps {
			return TokenDefaultsInput{}, errors.New("rate_limit_mbps must be between 1 and 10000")
		}
		if len([]rune(plan.Description)) > 255 {
			return TokenDefaultsInput{}, errors.New("description must be 255 chars or less")
		}
		plans = append(plans, plan)
	}
	sortTokenPlans(plans)
	return TokenDefaultsInput{Plans: plans}, nil
}

func BuildTokenDefaults(plans []TokenPlanDefault) TokenDefaults {
	if len(plans) == 0 {
		return DefaultTokenDefaults()
	}
	sortTokenPlans(plans)
	var updatedAt *time.Time
	for i := range plans {
		if plans[i].UpdatedAt == nil {
			continue
		}
		if updatedAt == nil || plans[i].UpdatedAt.After(*updatedAt) {
			updatedAt = plans[i].UpdatedAt
		}
	}
	return TokenDefaults{
		NodeHardLimit: TokenDefaultNodeHardLimit,
		Plans:         plans,
		UpdatedAt:     updatedAt,
	}
}

func sortTokenPlans(plans []TokenPlanDefault) {
	sort.SliceStable(plans, func(i, j int) bool {
		if plans[i].SortOrder == plans[j].SortOrder {
			return plans[i].PlanID < plans[j].PlanID
		}
		return plans[i].SortOrder < plans[j].SortOrder
	})
}

func (s *Server) handlePanelTokenDefaults(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requirePanelUser(w, r)
	if !ok {
		return
	}
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "node store is not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.writeTokenDefaults(w, r)
	case http.MethodPut:
		if !panelUserHasRole(user, PanelUserRoleAdmin) {
			writeError(w, http.StatusForbidden, "forbidden", "permission denied")
			return
		}
		var input TokenDefaultsInput
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		defaults, err := s.store.SaveTokenDefaults(r.Context(), input)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_token_defaults", err.Error())
			return
		}
		if err := s.store.RecordAudit(r.Context(), AuditLog{
			OperatorID: &user.ID,
			Action:     "panel.token_defaults.update",
			TargetType: "token_defaults",
			TargetID:   "global",
			Request:    defaults,
			IP:         clientIP(r),
			UserAgent:  r.UserAgent(),
		}); err != nil {
			s.logger.Warn("record audit", "action", "panel.token_defaults.update", "error", err)
		}
		writeJSON(w, http.StatusOK, map[string]any{"token_defaults": defaults})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *Server) handleBackendTokenDefaults(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAPIKey(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid API key")
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "node store is not configured")
		return
	}
	s.writeTokenDefaults(w, r)
}

func (s *Server) writeTokenDefaults(w http.ResponseWriter, r *http.Request) {
	defaults, err := s.store.GetTokenDefaults(r.Context())
	if err != nil {
		s.logger.Error("get token defaults", "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "get token defaults failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token_defaults": defaults})
}
