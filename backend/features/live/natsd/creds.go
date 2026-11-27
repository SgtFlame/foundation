package natsd

import (
	"github.com/nats-io/jwt/v2"
)

func nkeysJWT(creds []byte) (string, error) { return jwt.ParseDecoratedJWT(creds) }

func nkeysSeed(creds []byte) (string, error) {
	kp, err := jwt.ParseDecoratedNKey(creds)
	if err != nil {
		return "", err
	}
	seed, err := kp.Seed()
	return string(seed), err
}
