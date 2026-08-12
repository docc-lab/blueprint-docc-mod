package payloadbench

// Cheap, allocation-free payload generation. Content is zero bytes: no
// compression is enabled on either transport (gRPC or Blueprint HTTP), so the
// byte VALUES are irrelevant to the size-vs-throughput measurement, only the
// LENGTH matters. Sizes up to `prealloc` return a read-only sub-slice of a
// shared buffer (no per-request allocation); larger sizes allocate.
//
// Returned slices MUST be treated as read-only by callers (the RPC codegen only
// reads them to marshal, so sharing the backing array across concurrent requests
// is safe).
const prealloc = 16 << 20 // 16 MiB

var zeros = make([]byte, prealloc)

func makePayload(n int64) []byte {
	if n <= 0 {
		return []byte{}
	}
	if n <= int64(len(zeros)) {
		return zeros[:n:n]
	}
	return make([]byte, n)
}

