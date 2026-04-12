package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"github.com/alchemaxinc/terraform-provider-balena/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	opts := providerserver.ServeOpts{
		Address: "registry.terraform.io/alchemaxinc/balena",
		Debug:   debug,
	}

	err := providerserver.Serve(context.Background(), provider.New(version), opts)
	if err != nil {
		slog.With("error", err).Error("failed to start provider server")
		os.Exit(1)
	}
}
