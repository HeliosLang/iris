package main

//go:generate go run assets.go
//go:generate go run queries.go

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/acme/autocert"
)

const (
	NetworkFile = "/etc/cardano-node-ctl/network"
)

var (
	useHTTP bool
)

func main() {
	cli := makeCLI()

	cli.Execute()	
}

func makeCLI() *cobra.Command {
	cli := &cobra.Command{
		Use: "cardano-node-ctl",
		Short: "Install and manage Cardano Node services",
		RunE: serve,
	}

	cli.Flags().BoolVar(&useHTTP, "http", false, "host using HTTP instead of HTTPS (more suitable for localhost)")

	return cli
}

func serve(cmd *cobra.Command, args []string) error {
	networkName := readNetworkName()

	if (useHTTP) {
		return serveHTTP(cmd, args, networkName)
	} else {
		return serveHTTPS(cmd, args, networkName)
	}
}

func serveHTTP(cmd *cobra.Command, args []string, networkName string) error {
	handler, err := NewHandler(networkName)
	if err != nil {
		return err
	}
	
	// Start HTTP server
	server := &http.Server{
		Addr:      ":80",
		Handler: handler,
	}

	log.Println("HTTP server listening on port 80")

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("HTTP server error (%v)", err)
	}

	return nil
}

func serveHTTPS(cmd *cobra.Command, args []string, networkName string) error {
	handler, err := NewHandler(networkName)
	if err != nil {
		return err
	}

	// Create autocert manager with a custom HostPolicy
	certManager := &autocert.Manager{
		Prompt: autocert.AcceptTOS,
		Cache:  autocert.DirCache("certs"), // stores certs in ./certs, TODO: use XDG cache directory
		HostPolicy: func(_ context.Context, host string) error {
			if !isAllowedHost(host) {
				return fmt.Errorf("unauthorized host '%s'", host)
			} else {
				return nil
			}
		},
	}
	
	// Start HTTP server for Let's Encrypt challenge response
	go func() {
		httpServer := &http.Server{
			Addr:    ":80",
			Handler: certManager.HTTPHandler(nil),
		}

		log.Println("HTTP server (for ACME certificates) listening on port 80")

		if err := httpServer.ListenAndServe(); err != nil {
			log.Fatalf("HTTP server error (%v)", err)
		}
	}()

	// TLS config
	tlsConfig := &tls.Config{
		GetCertificate: certManager.GetCertificate,
		MinVersion:     tls.VersionTLS12,
	}

	// Start HTTPS server
	httpsServer := &http.Server{
		Addr:      ":443",
		Handler: handler,
		TLSConfig: tlsConfig,
	}

	log.Println("HTTPS server listening on port 443")

	return httpsServer.ListenAndServeTLS("", "") // certs provided dynamically
}

func readNetworkName() string {
	data, err := os.ReadFile(NetworkFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "preprod"
		}

		log.Fatalf("Error reading file %s: %v\n", NetworkFile, err)
	}

	name := strings.TrimSpace(string(data))

	if (name != "preprod" && name != "mainnet") {
		log.Fatalf("Expected preprod or mainnet in %s, got %v\n", NetworkFile, name)
	}

	return name
}

// Only allow domain names (not IPs or localhost)
func isAllowedHost(host string) bool {
	hostname := strings.Split(host, ":")[0] // strip port if present

	// Deny localhost and IP addresses
	if hostname == "localhost" || net.ParseIP(hostname) != nil {
		return false
	}

	// Basic domain format check
	domainRegex := regexp.MustCompile(`^[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	return domainRegex.MatchString(hostname)
}