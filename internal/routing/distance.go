package routing

import "math/big"

func XorDistance(a, b []byte) *big.Int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	x := make([]byte, n)
	for i := 0; i < n; i++ {
		x[i] = a[i] ^ b[i]
	}
	return new(big.Int).SetBytes(x)
}

func LeadingBitIndex(dist *big.Int) int {
	if dist.Sign() == 0 {
		return -1
	}
	bitLen := dist.BitLen() // 1..256
	return 256 - bitLen
}