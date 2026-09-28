// Package ratewindow owns the shared exact timestamp domain for rate authorities.
package ratewindow

import "github.com/weiloon1234/Foundry-Go/fault"

// MaxTimestamp is the greatest exact integer shared by Go and Redis Lua numbers.
const MaxTimestamp int64 = 1<<53 - 1

func End(now, window int64) (int64, error) {
	if window <= 0 || now < 0 || now > MaxTimestamp-window {
		return 0, fault.New(fault.Invalid, "rate limit clock outside supported epoch range")
	}
	return now - now%window + window, nil
}
