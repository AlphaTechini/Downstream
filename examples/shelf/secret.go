package shelf

import (
	"crypto/pbkdf2"
	"crypto/sha256"
)

const hashIterations = 1000

// hashSecret derives a 32-byte key from a password or an API token secret. Both
// kinds of secret go through it, so it runs on every login and on every
// authenticated request.
func hashSecret(secret string, salt []byte) []byte {
	key, err := pbkdf2.Key(sha256.New, secret, salt, hashIterations, 32)
	if err != nil {
		// Only an invalid key length can fail, and the length is a constant.
		panic("shelf: pbkdf2: " + err.Error())
	}
	return key
}
