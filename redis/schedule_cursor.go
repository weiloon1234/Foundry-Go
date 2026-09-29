package redis

import (
	"context"
	"strconv"
	"time"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/schedule"
)

var _ schedule.CursorBackend = (*Client)(nil)

// Schedule occurrences are whole milliseconds, so the cursor stores exact Unix
// milliseconds. The script only ever raises the stored value.
const scheduleCursorScript = `
local current = redis.call('GET', KEYS[1])
if current and (string.len(current) > 16 or not string.match(current, '^%d+$')) then return -2 end
if not current or tonumber(ARGV[1]) > tonumber(current) then
    redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
else
    redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
return 1`

func scheduleCursorKey(key lease.Key) string { return key.String() + ":cursor" }

// ScheduleCursor reads one persisted catch-up cursor.
func (c *Client) ScheduleCursor(ctx context.Context, key lease.Key) (time.Time, bool, error) {
	if ctx == nil {
		return time.Time{}, false, fault.New(fault.Invalid, "schedule cursor requires a context")
	}
	if err := key.Validate(); err != nil {
		return time.Time{}, false, err
	}
	result, err := c.execute(ctx, func(ctx context.Context, raw *driver.Client) (any, error) {
		value, err := raw.Get(ctx, scheduleCursorKey(key)).Result()
		if err == driver.Nil {
			return "", nil
		}
		return value, err
	})
	if err != nil {
		return time.Time{}, false, err
	}
	text, ok := result.(string)
	if !ok {
		return time.Time{}, false, fault.New(fault.Internal, "invalid Redis schedule cursor reply")
	}
	if text == "" {
		return time.Time{}, false, nil
	}
	milliseconds, err := strconv.ParseInt(text, 10, 64)
	if err != nil || milliseconds < 0 || len(text) > 16 {
		return time.Time{}, false, fault.New(fault.Invalid, "stored Redis schedule cursor is invalid")
	}
	return time.UnixMilli(milliseconds).UTC(), true, nil
}

// AdvanceScheduleCursor raises one persisted cursor and refreshes its expiry.
func (c *Client) AdvanceScheduleCursor(ctx context.Context, key lease.Key, at time.Time, ttl time.Duration) error {
	if ctx == nil {
		return fault.New(fault.Invalid, "schedule cursor requires a context")
	}
	if err := key.Validate(); err != nil {
		return err
	}
	if at.Before(time.Unix(0, 0)) || at.Nanosecond()%int(time.Millisecond) != 0 || ttl < time.Millisecond {
		return fault.New(fault.Invalid, "schedule cursor requires a whole-millisecond instant and positive expiry")
	}
	result, err := c.execute(ctx, func(ctx context.Context, raw *driver.Client) (any, error) {
		return evalScript(ctx, raw, scheduleCursorScript, []string{scheduleCursorKey(key)}, at.UnixMilli(), positiveMilliseconds(ttl)).Result()
	})
	if err != nil {
		return err
	}
	status, ok := result.(int64)
	if !ok || status != 1 {
		return fault.New(fault.Invalid, "stored Redis schedule cursor is invalid")
	}
	return nil
}
