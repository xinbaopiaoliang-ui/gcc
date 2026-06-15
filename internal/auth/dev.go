package auth

type DevAuthenticator struct {
	tokens map[string]struct{}
}

func NewDevAuthenticator(tokens []string) *DevAuthenticator {
	index := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		if token == "" {
			continue
		}
		index[token] = struct{}{}
	}
	return &DevAuthenticator{tokens: index}
}

func (a *DevAuthenticator) Authenticate(token string) (*Principal, error) {
	if _, ok := a.tokens[token]; !ok {
		return nil, ErrInvalidToken
	}
	return &Principal{
		UserID:   "dev",
		Token:    token,
		AllowTCP: true,
		AllowUDP: true,
	}, nil
}
