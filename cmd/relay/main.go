// Command relay is the Relay control plane server.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/abn/relay/internal/cli"
)

func main() {
	if err := cli.Run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "relay: %v\n", err)
		os.Exit(1)
	}
}
