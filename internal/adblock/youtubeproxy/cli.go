package youtubeproxy

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// CLI is a separate mode of the existing binary; it never starts the panel DB.
func CLI(args []string) error {
	flags := flag.NewFlagSet("youtube-proxy", flag.ContinueOnError)
	listen := flags.String("listen", "127.0.0.1:18080", "loopback CONNECT proxy address")
	preset := flags.String("print-routing", "", "print routing fragment for xray or singbox, then exit")
	caDir := flags.String("ca-dir", "/etc/x-ui/youtube-filter", "private CA directory (0600 key)")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if *preset != "" {
		data, err := RoutingPreset(*preset, *listen)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	fmt.Printf("Experimental YouTube HTTPS filter: %s\nTrust ONLY %s/ca-cert.pem on participating clients. Never distribute ca-private.pem.\n", *listen, *caDir)
	return Run(ctx, *listen, *caDir)
}
