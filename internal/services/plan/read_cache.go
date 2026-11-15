package plan

import (
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/proto"
)

// maxCachedReads is how many reads are kept, a few per household: enough
// for the month view's weeks, the calendar feed's and a plan's cost, at
// tens of kilobytes each.
const maxCachedReads = 32

// readKind is what was read: a week, or a plan's cost.
type readKind int

const (
	readWeek readKind = iota
	readCost
)

// readKey is what a read depends on besides the household's data: its
// kind, its dates, and today's date for a week, as what's left over and
// what's missing for the cooks to come count from it.
type readKey struct {
	householdID pgtype.UUID
	kind        readKind
	from, to    time.Time
	today       time.Time
}

// cachedRead is a response as read at a version of the household's data.
type cachedRead struct {
	version  string
	response proto.Message
}

// readCache keeps the weeks and plan costs read last, in memory, as the
// app reads the same ones on most pages. Every read checks the household's
// plan version, a few hundred bytes of the database's network transfer, a
// limit of its free tier, where reading a week again is tens of kilobytes.
// The server runs as at most one Cloud Run instance, but even a write on
// another would change the version.
type readCache struct {
	mu    sync.Mutex
	reads map[readKey]cachedRead
}

func newReadCache() *readCache {
	return &readCache{reads: map[readKey]cachedRead{}}
}

// cachedOf returns a copy of the response at the key if it was read at
// the version, or else reads it and keeps a copy. The response returned is
// the caller's to change.
func cachedOf[M proto.Message](c *readCache, key readKey, version string, read func() (M, error)) (M, error) {
	c.mu.Lock()
	cached, ok := c.reads[key]
	c.mu.Unlock()
	if ok && cached.version == version {
		if m, ok := cached.response.(M); ok {
			return proto.CloneOf(m), nil
		}
	}
	m, err := read()
	if err != nil {
		return m, err
	}
	kept := proto.CloneOf(m)
	c.mu.Lock()
	defer c.mu.Unlock()
	// Once full, every read is forgotten, as those of other days or
	// versions are never asked for again, and finding the oldest isn't
	// worth it for so few.
	if len(c.reads) >= maxCachedReads {
		clear(c.reads)
	}
	c.reads[key] = cachedRead{version: version, response: kept}
	return m, nil
}
