//go:build !unix

package http

import "os"

const downloadOpenFlags = os.O_RDONLY
