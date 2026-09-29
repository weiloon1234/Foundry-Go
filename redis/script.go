package redis

import (
	"context"
	"sync"

	driver "github.com/redis/go-redis/v9"
)

// scripts memoizes the framework's constant Lua sources by content. Commands
// use EVALSHA and upload the source only after a NOSCRIPT reply, which proves
// the script did not run, so the fallback never repeats an executed command.
var scripts sync.Map

func evalScript(ctx context.Context, client driver.Scripter, source string, keys []string, args ...any) *driver.Cmd {
	cached, ok := scripts.Load(source)
	if !ok {
		cached, _ = scripts.LoadOrStore(source, driver.NewScript(source))
	}
	return cached.(*driver.Script).Run(ctx, client, keys, args...)
}
