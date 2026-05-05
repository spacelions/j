package tasks

import (
	"crypto/rand"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// ulidEntropy is the process-local monotonic entropy source feeding
// NewTaskID. Wrapping crypto/rand.Reader in ulid.Monotonic guarantees
// strict lexicographic ordering for IDs minted within the same
// millisecond. The reader is not safe for concurrent use, so callers
// must hold ulidMu while invoking ulid.MustNew.
var (
	ulidMu      sync.Mutex
	ulidEntropy = ulid.Monotonic(rand.Reader, 0)
)

// NewTaskID returns a stable, unique, lexicographically time-sortable
// task identifier in canonical ULID form: 26 ASCII characters in
// Crockford base32 (`0-9A-HJKMNP-TV-Z`), where the leading 10 chars
// encode time.Now().UTC() at millisecond precision and the trailing
// 16 chars encode 80 bits of randomness from crypto/rand. IDs minted
// inside the same millisecond are strictly ascending thanks to the
// monotonic entropy source. The function is safe for concurrent use.
func NewTaskID() string {
	ulidMu.Lock()
	defer ulidMu.Unlock()
	return ulid.MustNew(ulid.Timestamp(time.Now()), ulidEntropy).String()
}
