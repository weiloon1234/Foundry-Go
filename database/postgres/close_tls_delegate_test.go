package postgres_test

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/postgres"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestPostgresClosePreservesTLSRandomFailure(t *testing.T) {
	cbc := func(config *tls.Config) {
		config.MinVersion, config.MaxVersion = tls.VersionTLS12, tls.VersionTLS12
		config.CipherSuites = []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA}
	}
	for _, test := range []struct {
		name  string
		cause error
	}{
		{"EOF", io.EOF},
		{"network reset", &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}},
	} {
		t.Run(test.name, func(t *testing.T) {
			bridge, _ := postgresTLSBridge(t, pgtest.Config(t), cbc)
			config := pgtest.Config(t)
			bridge(&config)
			cbc(config.TLSConfig)
			random := &closingRandom{cause: test.cause}
			config.TLSConfig.Rand = random
			db, err := postgres.Open(t.Context(), config)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close(context.Background())
			random.fail.Store(true)
			err = db.Close(t.Context())
			if random.failedReads.Load() == 0 {
				t.Fatal("TLS close did not exercise the failing random source")
			}
			if err == nil || !errors.Is(err, test.cause) {
				t.Fatal("TLS random delegate failure was suppressed or lost")
			}
		})
	}
}

type closingRandom struct {
	cause       error
	fail        atomic.Bool
	failedReads atomic.Int32
}

func (r *closingRandom) Read(p []byte) (int, error) {
	if r.fail.Load() {
		r.failedReads.Add(1)
		return 0, r.cause
	}
	return rand.Read(p)
}
