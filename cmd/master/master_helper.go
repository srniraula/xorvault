package main

import (
	"math/rand"
)

func RandomID() int64 {
	lower := int64((1 << 32) - 1) // 2^32 - 1
	upper := int64((1 << 63) - 1) // 2^63 - 1

	diff := upper - lower + 1 // range size

	return rand.Int63n(diff) + lower // random range
}
