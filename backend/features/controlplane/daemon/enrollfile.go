package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
)

// EnrollFile is what a host writes into an instance it makes (cloud-init,
// /etc/axiomd/enroll.json; forge 20c-2): the instance's one-time token and
// everything else its agent needs to enroll, with the shared enroll
// user's creds and the CA as text.
type EnrollFile struct {
	Mode        string   `json:"mode"`
	Token       string   `json:"token"`
	NatsURLs    []string `json:"natsUrls"`
	EnrollCreds string   `json:"enrollCreds,omitempty"`
	CA          string   `json:"ca,omitempty"`
}

// EnrollFrom enrolls with an EnrollFile: the creds and CA are written
// into state first, then Enroll runs as usual.
func EnrollFrom(ctx context.Context, path, state, configPath string) (Config, error) {
	var f EnrollFile
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, log.Wrap(err)
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return Config{}, log.Wrap(err)
	}
	if err := os.MkdirAll(state, 0o700); err != nil {
		return Config{}, log.Wrap(err)
	}
	c := Config{Mode: f.Mode, NatsURLs: f.NatsURLs, State: state}
	if c.Mode == "" {
		c.Mode = "agent"
	}
	creds := ""
	if f.EnrollCreds != "" {
		creds = filepath.Join(state, "enroll.in.creds")
		if err := os.WriteFile(creds, []byte(f.EnrollCreds), 0o600); err != nil {
			return c, log.Wrap(err)
		}
		defer os.Remove(creds)
	}
	if f.CA != "" {
		c.CA = filepath.Join(state, "ca.pem")
		if err := os.WriteFile(c.CA, []byte(f.CA), 0o644); err != nil {
			return c, log.Wrap(err)
		}
	}
	return Enroll(ctx, c, creds, f.Token, configPath)
}

// File is the EnrollFile for a token, from this daemon's own config: its
// URLs, its kept enroll creds and its CA.
func (c Config) File(mode, token string) (EnrollFile, error) {
	f := EnrollFile{Mode: mode, Token: token, NatsURLs: c.NatsURLs}
	if c.EnrollCreds != "" {
		raw, err := os.ReadFile(c.EnrollCreds)
		if err != nil {
			return f, log.Wrap(err)
		}
		f.EnrollCreds = string(raw)
	}
	if c.CA != "" {
		raw, err := os.ReadFile(c.CA)
		if err != nil {
			return f, log.Wrap(err)
		}
		f.CA = string(raw)
	}
	return f, nil
}
