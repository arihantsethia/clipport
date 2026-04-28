package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"

	"github.com/arihantsethia/clipport/internal/config"
	"github.com/arihantsethia/clipport/internal/daemon"
	"github.com/arihantsethia/clipport/internal/token"
)

func main() {
	configPath := flag.String("config", "", "path to clipport config")
	socketPath := flag.String("socket", daemon.DefaultSocketPath(), "unix socket path")
	httpAddr := flag.String("http", "", "optional loopback HTTP address for SSH RemoteForward clients")
	tokenPath := flag.String("token", token.DefaultPath(), "bearer token path for HTTP clients")
	flag.Parse()

	var cfg *config.Config
	var err error
	if *configPath != "" {
		cfg, err = config.Load(*configPath)
	} else {
		cfg, err = config.LoadDefault()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "clipportd: %v\n", err)
		os.Exit(1)
	}
	server := daemon.NewServer(cfg)
	if *httpAddr != "" {
		if err := requireLoopback(*httpAddr); err != nil {
			fmt.Fprintf(os.Stderr, "clipportd: %v\n", err)
			os.Exit(1)
		}
		bearer, err := token.LoadOrCreate(*tokenPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "clipportd: %v\n", err)
			os.Exit(1)
		}
		go func() {
			log.Printf("clipportd HTTP listening on %s", *httpAddr)
			if err := server.ListenHTTP(*httpAddr, bearer); err != nil {
				log.Fatal(err)
			}
		}()
	}
	log.Printf("clipportd listening on %s", *socketPath)
	if err := server.Listen(*socketPath); err != nil {
		log.Fatal(err)
	}
}

func requireLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("--http must bind to loopback, got %q", addr)
	}
	return nil
}
