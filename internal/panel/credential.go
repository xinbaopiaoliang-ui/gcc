package panel

import (
	"errors"
	"strings"
	"time"
)

const (
	CredentialAuthPassword   = "password"
	CredentialAuthPrivateKey = "private_key"

	CredentialSudoRoot = "root"
	CredentialSudoSudo = "sudo"
)

type NodeCredentialRequest struct {
	AuthType             string `json:"auth_type"`
	Username             string `json:"username"`
	Password             string `json:"password,omitempty"`
	PrivateKey           string `json:"private_key,omitempty"`
	PrivateKeyPassphrase string `json:"private_key_passphrase,omitempty"`
	SudoMode             string `json:"sudo_mode"`
	IsOneTime            bool   `json:"is_one_time"`
}

type NodeCredentialInput struct {
	NodeID                        string
	AuthType                      string
	Username                      string
	PasswordEncrypted             string
	PrivateKeyEncrypted           string
	PrivateKeyPassphraseEncrypted string
	SudoMode                      string
	IsOneTime                     bool
}

type NodeCredential struct {
	ID                   uint64     `json:"id"`
	NodeID               string     `json:"node_id"`
	AuthType             string     `json:"auth_type"`
	Username             string     `json:"username"`
	SudoMode             string     `json:"sudo_mode"`
	IsOneTime            bool       `json:"is_one_time"`
	HasPassword          bool       `json:"has_password"`
	HasPrivateKey        bool       `json:"has_private_key"`
	HasPrivatePassphrase bool       `json:"has_private_key_passphrase"`
	LastUsedAt           *time.Time `json:"last_used_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`

	PasswordEncrypted             string `json:"-"`
	PrivateKeyEncrypted           string `json:"-"`
	PrivateKeyPassphraseEncrypted string `json:"-"`
}

func NewNodeCredentialInput(nodeID string, req NodeCredentialRequest, box *SecretBox) (NodeCredentialInput, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return NodeCredentialInput{}, errors.New("node_id is required")
	}
	req.AuthType = strings.TrimSpace(req.AuthType)
	if req.AuthType == "" {
		req.AuthType = CredentialAuthPassword
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		req.Username = "root"
	}
	req.SudoMode = strings.TrimSpace(req.SudoMode)
	if req.SudoMode == "" {
		req.SudoMode = CredentialSudoRoot
	}
	if req.AuthType != CredentialAuthPassword && req.AuthType != CredentialAuthPrivateKey {
		return NodeCredentialInput{}, errors.New("auth_type must be password or private_key")
	}
	if req.SudoMode != CredentialSudoRoot && req.SudoMode != CredentialSudoSudo {
		return NodeCredentialInput{}, errors.New("sudo_mode must be root or sudo")
	}
	if req.AuthType == CredentialAuthPassword && req.Password == "" {
		return NodeCredentialInput{}, errors.New("password is required for password auth")
	}
	if req.AuthType == CredentialAuthPrivateKey && strings.TrimSpace(req.PrivateKey) == "" {
		return NodeCredentialInput{}, errors.New("private_key is required for private_key auth")
	}

	passwordEncrypted, err := box.Encrypt(req.Password)
	if err != nil {
		return NodeCredentialInput{}, err
	}
	privateKeyEncrypted, err := box.Encrypt(req.PrivateKey)
	if err != nil {
		return NodeCredentialInput{}, err
	}
	passphraseEncrypted, err := box.Encrypt(req.PrivateKeyPassphrase)
	if err != nil {
		return NodeCredentialInput{}, err
	}

	return NodeCredentialInput{
		NodeID:                        nodeID,
		AuthType:                      req.AuthType,
		Username:                      req.Username,
		PasswordEncrypted:             passwordEncrypted,
		PrivateKeyEncrypted:           privateKeyEncrypted,
		PrivateKeyPassphraseEncrypted: passphraseEncrypted,
		SudoMode:                      req.SudoMode,
		IsOneTime:                     req.IsOneTime,
	}, nil
}
