//go:build !darwin && !linux

package filelock

import (
	"os"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func Try(*os.File) (bool, error) { return try(nil, exclusive) }
func try(*os.File, mode) (bool, error) {
	return false, fault.New(fault.Invalid, "filesystem locks require macOS or Linux")
}
