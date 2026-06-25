package panel

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type SSHCredentialTestResult struct {
	OK        bool   `json:"ok"`
	NodeID    string `json:"node_id"`
	Address   string `json:"address"`
	LatencyMS int64  `json:"latency_ms"`
	Output    string `json:"output,omitempty"`
	Error     string `json:"error,omitempty"`
}

func (s *Server) handleNodeCredential(w http.ResponseWriter, r *http.Request, nodeID string) {
	switch r.Method {
	case http.MethodGet:
		credential, err := s.store.GetNodeCredential(r.Context(), nodeID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				writeJSON(w, http.StatusOK, map[string]any{"credential": nil})
				return
			}
			s.logger.Error("get node credential", "node_id", nodeID, "error", err)
			writeError(w, http.StatusInternalServerError, "store_error", "get credential failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"credential": credential})
	case http.MethodPut:
		if s.secrets == nil {
			writeError(w, http.StatusInternalServerError, "secret_box_unavailable", "secret box is not configured")
			return
		}
		var req NodeCredentialRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		input, err := NewNodeCredentialInput(nodeID, req, s.secrets)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_credential", err.Error())
			return
		}
		credential, err := s.store.UpsertNodeCredential(r.Context(), input)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				writeError(w, http.StatusNotFound, "node_not_found", "node not found")
				return
			}
			s.logger.Error("upsert node credential", "node_id", nodeID, "error", err)
			writeError(w, http.StatusInternalServerError, "store_error", "save credential failed")
			return
		}
		if err := s.store.RecordAudit(r.Context(), AuditLog{
			Action:     "panel.node.credential.upsert",
			TargetType: "node",
			TargetID:   nodeID,
			Request: map[string]any{
				"auth_type":   req.AuthType,
				"username":    req.Username,
				"sudo_mode":   req.SudoMode,
				"is_one_time": req.IsOneTime,
			},
			IP:        clientIP(r),
			UserAgent: r.UserAgent(),
		}); err != nil {
			s.logger.Warn("record audit", "action", "panel.node.credential.upsert", "node_id", nodeID, "error", err)
		}
		writeJSON(w, http.StatusOK, map[string]any{"credential": credential})
	case http.MethodDelete:
		if err := s.store.DeleteNodeCredential(r.Context(), nodeID); err != nil {
			if errors.Is(err, ErrNotFound) {
				writeError(w, http.StatusNotFound, "credential_not_found", "credential not found")
				return
			}
			s.logger.Error("delete node credential", "node_id", nodeID, "error", err)
			writeError(w, http.StatusInternalServerError, "store_error", "delete credential failed")
			return
		}
		if err := s.store.RecordAudit(r.Context(), AuditLog{
			Action:     "panel.node.credential.delete",
			TargetType: "node",
			TargetID:   nodeID,
			IP:         clientIP(r),
			UserAgent:  r.UserAgent(),
		}); err != nil {
			s.logger.Warn("record audit", "action", "panel.node.credential.delete", "node_id", nodeID, "error", err)
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *Server) handleTestNodeCredential(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if s.secrets == nil {
		writeError(w, http.StatusInternalServerError, "secret_box_unavailable", "secret box is not configured")
		return
	}
	node, err := s.store.GetNode(r.Context(), nodeID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "node_not_found", "node not found")
			return
		}
		s.logger.Error("get node", "node_id", nodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "get node failed")
		return
	}
	credential, err := s.store.GetNodeCredential(r.Context(), nodeID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "credential_not_found", "credential not found")
			return
		}
		s.logger.Error("get node credential", "node_id", nodeID, "error", err)
		writeError(w, http.StatusInternalServerError, "store_error", "get credential failed")
		return
	}
	result := s.testSSHCredential(r.Context(), *node, *credential)
	if result.OK {
		if err := s.store.MarkNodeCredentialUsed(r.Context(), nodeID, time.Now().UTC()); err != nil {
			s.logger.Warn("mark credential used", "node_id", nodeID, "error", err)
		}
	}
	if err := s.store.RecordAudit(r.Context(), AuditLog{
		Action:     "panel.node.credential.test",
		TargetType: "node",
		TargetID:   nodeID,
		Request: map[string]any{
			"ok":      result.OK,
			"address": result.Address,
		},
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
	}); err != nil {
		s.logger.Warn("record audit", "action", "panel.node.credential.test", "node_id", nodeID, "error", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": result})
}

func (s *Server) testSSHCredential(ctx context.Context, node Node, credential NodeCredential) SSHCredentialTestResult {
	address := net.JoinHostPort(node.SSHHost, fmt.Sprintf("%d", node.SSHPort))
	start := time.Now()
	result := SSHCredentialTestResult{
		NodeID:  node.NodeID,
		Address: address,
	}
	authMethods, err := s.sshAuthMethods(credential)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	config := &ssh.ClientConfig{
		User:            credential.Username,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         s.cfg.Deploy.SSHTimeout,
	}
	dialer := net.Dialer{Timeout: s.cfg.Deploy.SSHTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		result.Error = err.Error()
		result.LatencyMS = time.Since(start).Milliseconds()
		return result
	}
	defer conn.Close()
	clientConn, chans, reqs, err := ssh.NewClientConn(conn, address, config)
	if err != nil {
		result.Error = err.Error()
		result.LatencyMS = time.Since(start).Milliseconds()
		return result
	}
	client := ssh.NewClient(clientConn, chans, reqs)
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		result.Error = err.Error()
		result.LatencyMS = time.Since(start).Milliseconds()
		return result
	}
	defer session.Close()
	output, err := session.CombinedOutput("printf 'gaccel-ssh-ok '; uname -s")
	result.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.OK = true
	result.Output = strings.TrimSpace(string(output))
	return result
}

func (s *Server) sshAuthMethods(credential NodeCredential) ([]ssh.AuthMethod, error) {
	switch credential.AuthType {
	case CredentialAuthPassword:
		password, err := s.secrets.Decrypt(credential.PasswordEncrypted)
		if err != nil {
			return nil, err
		}
		return []ssh.AuthMethod{ssh.Password(password)}, nil
	case CredentialAuthPrivateKey:
		privateKey, err := s.secrets.Decrypt(credential.PrivateKeyEncrypted)
		if err != nil {
			return nil, err
		}
		passphrase, err := s.secrets.Decrypt(credential.PrivateKeyPassphraseEncrypted)
		if err != nil {
			return nil, err
		}
		var signer ssh.Signer
		if passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(privateKey), []byte(passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(privateKey))
		}
		if err != nil {
			return nil, err
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	default:
		return nil, errors.New("unsupported credential auth_type")
	}
}
