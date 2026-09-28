package local

import (
	"github.com/weiloon1234/Foundry-Go/internal/filelock"
	"os"
)

func tryLock(file *os.File) (bool, error) { return filelock.Try(file) }
