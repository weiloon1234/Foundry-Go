package redis

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/internal/cachetest"
)

type lostEntryAcknowledgement struct {
	script  string
	calls   *atomic.Int32
	failure error
}

func (h lostEntryAcknowledgement) DialHook(next driver.DialHook) driver.DialHook { return next }
func (h lostEntryAcknowledgement) ProcessPipelineHook(next driver.ProcessPipelineHook) driver.ProcessPipelineHook {
	return next
}
func (h lostEntryAcknowledgement) ProcessHook(next driver.ProcessHook) driver.ProcessHook {
	return func(ctx context.Context, cmd driver.Cmder) error {
		args := cmd.Args()
		matched := len(args) > 1 && args[0] == "eval" && args[1] == h.script
		err := next(ctx, cmd)
		if matched {
			h.calls.Add(1)
			if err == nil {
				return h.failure
			}
		}
		return err
	}
}
func TestEntryLostAcknowledgementsDoNotReturnSuccessOrRetry(t *testing.T) {
	for _, tagged := range []bool{false, true} {
		mode := "plain"
		if tagged {
			mode = "tagged"
		}
		for _, operation := range []string{"exists", "expire", "batch"} {
			t.Run(mode+"/"+operation, func(t *testing.T) {
				c, key, track := integrationTracked(t, nil)
				base := key("entry")
				snapshot := (cachetest.TaggedFixture{Backend: c, Track: track}).Snapshot(t, base, key("tag"))
				physical := base.String()
				script := cacheScript
				if tagged {
					physical = snapshot.DataKey().String()
					script = taggedCacheScript
				}
				if operation == "batch" {
					script = cacheBatchScript
					if tagged {
						script = taggedBatchScript
					}
				}
				var err error
				if tagged {
					err = c.PutTagged(t.Context(), snapshot, []byte("value"), cache.For(time.Minute))
				} else {
					err = c.Put(t.Context(), base, []byte("value"), cache.For(time.Minute))
				}
				if err != nil {
					t.Fatal(err)
				}
				failure := errors.New("entry acknowledgement lost")
				var calls atomic.Int32
				c.raw.AddHook(lostEntryAcknowledgement{script: script, calls: &calls, failure: failure})
				var result bool
				var count uint64
				switch operation {
				case "exists":
					if tagged {
						result, err = c.ExistsTagged(t.Context(), snapshot)
					} else {
						result, err = c.Exists(t.Context(), base)
					}
				case "expire":
					if tagged {
						result, err = c.ExpireTagged(t.Context(), snapshot, cache.Forever())
					} else {
						result, err = c.Expire(t.Context(), base, cache.Forever())
					}
				case "batch":
					if tagged {
						count, err = c.ForgetManyTagged(t.Context(), []cache.TaggedKey{snapshot})
					} else {
						count, err = c.ForgetMany(t.Context(), []cache.EntryKey{base})
					}
				}
				if result || count != 0 || !errors.Is(err, failure) || calls.Load() != 1 {
					t.Fatal(result, count, err, calls.Load())
				}
				if operation == "expire" && c.raw.PTTL(t.Context(), physical).Val() != -1 {
					t.Fatal("applied expiry was rolled back")
				}
				if operation == "batch" && c.raw.Exists(t.Context(), physical).Val() != 0 {
					t.Fatal("applied batch was rolled back")
				}
			})
		}
	}
}
func TestCacheBatchMalformedTransportCounts(t *testing.T) {
	for _, reply := range []string{"+invalid\r\n", "*0\r\n", "*1\r\n:1\r\n", "*2\r\n:1\r\n:-1\r\n", "*2\r\n:1\r\n:2\r\n", "*2\r\n:1\r\n$1\r\n1\r\n", "*2\r\n:0\r\n:1\r\n"} {
		t.Run(reply, func(t *testing.T) {
			config := transportServer(t, func(conn net.Conn, args []string, _ <-chan struct{}) {
				if strings.EqualFold(args[0], "eval") {
					io.WriteString(conn, reply)
				} else {
					io.WriteString(conn, "+PONG\r\n")
				}
			})
			c := preparedTransport(t, config)
			if err := c.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			key, err := cache.NewEntryKey(cache.Namespace{Application: "test", Environment: "batch-reply"}, "values", "key")
			if err != nil {
				t.Fatal(err)
			}
			if count, err := c.ForgetMany(t.Context(), []cache.EntryKey{key}); err == nil || count != 0 {
				t.Fatal(count, err)
			}
		})
	}
}
