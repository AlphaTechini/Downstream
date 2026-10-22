package shelf

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// The expected keys were produced by the standard library's crypto/pbkdf2 on
// Go 1.24.
func TestPBKDF2Vectors(t *testing.T) {
	cases := []struct {
		password, salt string
		iterations     int
		keyLen         int
		want           string
	}{
		{"password", "salt", 1, 32, "120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b"},
		{"password", "salt", 2, 32, "ae4d0c95af6b46d32d0adff928f06dd02a303f8ef3c251dfd6e2d85a95474c43"},
		{"password", "salt", 4096, 32, "c5e478d59288c841aa530db6845c4c8d962893a001ce4e11a4963873aa98134a"},
		// 40 bytes needs two HMAC blocks.
		{"passwordPASSWORDpassword", "saltSALTsaltSALTsaltSALTsaltSALTsalt", 4096, 40,
			"348c89dbcbd32b2f32d814b8116e84cf2b17347ebc1800181c4e2a1fb8dd53e1c635518c7dac47e9"},
	}
	for _, c := range cases {
		got := hex.EncodeToString(pbkdf2(sha256.New, []byte(c.password), []byte(c.salt), c.iterations, c.keyLen))
		if got != c.want {
			t.Errorf("pbkdf2(%q, %q, %d, %d) = %s, want %s", c.password, c.salt, c.iterations, c.keyLen, got, c.want)
		}
	}
}

// hashSecret must keep returning the same key it returned when it called the
// standard library, or stored password hashes would stop matching.
func TestHashSecretKeepsItsOutput(t *testing.T) {
	got := hex.EncodeToString(hashSecret("secret", []byte{1, 2, 3}))
	want := "6ad3cd2c354b85ea270a4892df84e3a12fa14404709c41f12ff9bdeaccd1f87a"
	if hashIterations != 1000 || got != want {
		t.Errorf("hashSecret = %s with %d iterations, want %s with 1000", got, hashIterations, want)
	}
}
