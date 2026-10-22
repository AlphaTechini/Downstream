package shelf

import (
	"crypto/sha256"
)

const hashIterations = 1000

// hashSecret derives a 32-byte key from a password or an API token secret. Both
// kinds of secret go through it, so it runs on every login and on every
// authenticated request.
func hashSecret(secret string, salt []byte) []byte {
	return pbkdf2(sha256.New, []byte(secret), salt, hashIterations, 32)
}
