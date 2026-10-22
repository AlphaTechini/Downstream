package shelf

import (
	"crypto/hmac"
	"hash"
)

// pbkdf2 derives keyLen bytes from password and salt with PBKDF2 (RFC 8018,
// section 5.2), using HMAC over h as the pseudorandom function. The standard
// library only gained crypto/pbkdf2 in Go 1.24 and this app builds on Go 1.22,
// so the loop is written out here.
func pbkdf2(h func() hash.Hash, password, salt []byte, iterations, keyLen int) []byte {
	prf := hmac.New(h, password)
	size := prf.Size()
	blocks := (keyLen + size - 1) / size

	out := make([]byte, 0, blocks*size)
	u := make([]byte, 0, size)
	for i := 1; i <= blocks; i++ {
		// U1 = PRF(password, salt || INT(i)), with i as four big-endian bytes.
		prf.Reset()
		prf.Write(salt)
		prf.Write([]byte{byte(i >> 24), byte(i >> 16), byte(i >> 8), byte(i)})
		u = prf.Sum(u[:0])

		// The block is U1 xor U2 xor ... xor Uc, where Un = PRF(password, Un-1).
		block := append([]byte(nil), u...)
		for n := 1; n < iterations; n++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(u[:0])
			for j := range block {
				block[j] ^= u[j]
			}
		}
		out = append(out, block...)
	}
	return out[:keyLen]
}
