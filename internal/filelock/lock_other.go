//go:build !darwin && !linux

package filelock

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"os"
)

func Try(*os.File) (bool, error) {
	return false, fault.New(fault.Invalid, "filesystem locks require macOS or Linux")
}
