package app

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/weiloon1234/Foundry-Go/deployment"
)

// featureCopies runs inside the consuming unit's mount namespace for acceptance.
// The descriptor supplies paths; no private values enter arguments or output.
func featureCopies(ctx context.Context, args []string) error {
	if len(args) < 3 || len(args) > 4 || (args[2] != "granted" && args[2] != "omitted") {
		return fmt.Errorf("copies probe requires binding, count, grant mode and optional public binding")
	}
	count, err := strconv.Atoi(args[1])
	if err != nil || count < 0 || count > 128 {
		return fmt.Errorf("invalid expected copy count")
	}
	bindings, present, err := deployment.LoadBindings()
	if err != nil || !present {
		return fmt.Errorf("copies probe requires a valid descriptor")
	}
	copies, err := bindings.Copies(deployment.BindingName(args[0]))
	if err != nil || len(copies) != count {
		return fmt.Errorf("copies binding has unexpected placement")
	}
	for index, copy := range copies {
		if copy.Ordinal != index+1 || (copy.SecretFile != "") != (args[2] == "granted") {
			return fmt.Errorf("copies binding has unexpected identity or secret grant")
		}
		if copy.SecretFile != "" {
			file, err := os.Open(copy.SecretFile)
			if err != nil {
				return fmt.Errorf("copy secret is unreadable")
			}
			var value [1]byte
			n, readErr := file.Read(value[:])
			closeErr := file.Close()
			if n != 1 || readErr != nil || closeErr != nil {
				return fmt.Errorf("copy secret is empty or unreadable")
			}
			writable, err := os.OpenFile(copy.SecretFile, os.O_WRONLY, 0)
			if err == nil {
				_ = writable.Close()
				return fmt.Errorf("copy secret grant is writable")
			}
		}
		address := copy.Endpoint.Path
		if copy.Endpoint.Scheme != deployment.Unix {
			address = net.JoinHostPort(copy.Endpoint.Address.String(), strconv.Itoa(int(copy.Endpoint.Port)))
		}
		dialer := net.Dialer{Timeout: 3 * time.Second}
		connection, err := dialer.DialContext(ctx, copy.Endpoint.Network(), address)
		if err != nil {
			return fmt.Errorf("copy endpoint is unreachable")
		}
		_ = connection.Close()
	}
	if len(args) == 4 {
		public, err := bindings.PublicListener(deployment.BindingName(args[3]))
		if err != nil || len(public.Ports) != count {
			return fmt.Errorf("public copy ports do not match placement")
		}
		for index, port := range public.Ports {
			if port.Ordinal != copies[index].Ordinal || port.Port == 0 {
				return fmt.Errorf("public copy port has unexpected identity")
			}
		}
	}
	return nil
}
