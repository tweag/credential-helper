package oci

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

type WWWAuthenticate struct {
	Realm   string
	Service string
}

type AuthConfig struct {
	Username            string
	Password            string
	Auth                string
	IdentityToken       string
	RegistryToken       string
	TokenExchangeMethod string
}

type BasicAuthToken struct {
	Token        string `json:"token,omitempty"`
	AccessToken  string `json:"access_token,omitempty"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	IssuedAt     string `json:"issued_at,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

func (b BasicAuthToken) Convert(now time.Time) *oauth2.Token {
	if b.IssuedAt == "" {
		b.IssuedAt = now.Format(time.RFC3339)
	}
	if b.ExpiresIn == 0 {
		// from the spec:
		//   expires_in: (Optional) The duration in seconds since the token was issued that it will remain valid.
		//   When omitted, this defaults to 60 seconds.
		//   For compatibility with older clients, a token should never be returned with less than 60 seconds to live.
		b.ExpiresIn = 60
	}
	if b.Token == "" && b.AccessToken != "" {
		// compatibility with OAuth 2.0
		// some clients use the AccessToken field instead of Token
		b.Token = b.AccessToken
	}

	issuesAt, err := time.Parse(time.RFC3339, b.IssuedAt)
	if err != nil {
		issuesAt = now
	}

	return &oauth2.Token{
		AccessToken:  b.Token,
		TokenType:    "Bearer",
		RefreshToken: b.RefreshToken,
		Expiry:       issuesAt.Add(time.Duration(b.ExpiresIn) * time.Second).UTC(),
		ExpiresIn:    int64(b.ExpiresIn),
	}
}

type OAuth2Token struct {
	AccessToken  string `json:"access_token"`
	Scope        string `json:"scope"`
	ExpiresIn    int    `json:"expires_in"`
	IssuedAt     string `json:"issued_at,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

func (o OAuth2Token) Convert(now time.Time) *oauth2.Token {
	if o.IssuedAt == "" {
		o.IssuedAt = now.Format(time.RFC3339)
	}
	if o.ExpiresIn == 0 {
		// from the spec:
		//   expires_in: (REQUIRED) The duration in seconds since the token was issued that it will remain valid.
		//   When omitted, this defaults to 60 seconds.
		//   For compatibility with older clients, a token should never be returned with less than 60 seconds to live.
		o.ExpiresIn = 60
	}

	issuesAt, err := time.Parse(time.RFC3339, o.IssuedAt)
	if err != nil {
		issuesAt = now
	}

	return &oauth2.Token{
		AccessToken:  o.AccessToken,
		TokenType:    "Bearer",
		RefreshToken: o.RefreshToken,
		Expiry:       issuesAt.Add(time.Duration(o.ExpiresIn) * time.Second).UTC(),
		ExpiresIn:    int64(o.ExpiresIn),
	}
}

func parseWWWAuthenticate(header string) (map[string]string, error) {
	if !strings.HasPrefix(header, "Bearer ") {
		return nil, errors.New("unexpected auth type")
	}
	header = "," + header[len("Bearer "):]

	var result map[string]string = make(map[string]string)
	for strings.HasPrefix(header, ",") {
		header = header[1:]
		var k, v string
		k, v, header = consumeKV(header)
		result[k] = v
	}

	return result, nil
}

func consumeKey(input string) (string, string) {
	kv := strings.SplitN(input, "=", 2)
	if len(kv) == 0 {
		return "", ""
	}
	if len(kv) == 1 {
		return kv[0], ""
	}
	return strings.TrimSpace(kv[0]), kv[1]
}

func consumeToken(input string) (string, string) {
	for i := 0; i < len(input); i++ {
		if strings.ContainsRune(", \t\r\n", rune(input[i])) {
			return strings.TrimSpace(input[:i]), input[i+1:]
		}
	}
	return strings.TrimSpace(input), ""
}

func consumeQuoted(input string) (string, string) {
	quotedPrefix, err := strconv.QuotedPrefix(input)
	if err != nil {
		return "", input
	}
	unqouted, err := strconv.Unquote(quotedPrefix)
	if err != nil {
		return "", input
	}
	return unqouted, input[len(quotedPrefix):]
}

func consumeTokenOrQuoted(input string) (string, string) {
	if strings.HasPrefix(input, `"`) {
		return consumeQuoted(input)
	}
	return consumeToken(input)
}

func consumeKV(input string) (string, string, string) {
	key, input := consumeKey(input)
	if key == "" {
		return "", "", input
	}
	value, input := consumeTokenOrQuoted(input)
	return key, value, input
}
