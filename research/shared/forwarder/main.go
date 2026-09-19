// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// This research executable starts the unmodified HTTP forwarder extension.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/open-telemetry/opentelemetry-collector-contrib/extension/httpforwarderextension"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/extension"
	"go.opentelemetry.io/collector/extension/extensiontest"
	"go.yaml.in/yaml/v3"
)

// loadConfig accepts exactly the extension's YAML subtree; no invented route API.
func loadConfig(contents []byte) (*httpforwarderextension.Config, error) {
	var settings map[string]any
	if err := yaml.Unmarshal(contents, &settings); err != nil {
		return nil, err
	}
	cfg := httpforwarderextension.NewFactory().CreateDefaultConfig().(*httpforwarderextension.Config)
	if err := confmap.NewFromStringMap(settings).Unmarshal(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func start(cfg *httpforwarderextension.Config) (extension.Extension, error) {
	factory := httpforwarderextension.NewFactory()
	ext, err := factory.Create(context.Background(), extensiontest.NewNopSettings(factory.Type()), cfg)
	if err != nil {
		return nil, err
	}
	if err := ext.Start(context.Background(), componenttest.NewNopHost()); err != nil {
		return nil, err
	}
	return ext, nil
}

func main() {
	configFile := flag.String("config", "config.yaml", "YAML containing the http_forwarder extension settings")
	flag.Parse()
	contents, err := os.ReadFile(*configFile)
	if err != nil {
		panic(err)
	}
	cfg, err := loadConfig(contents)
	if err != nil {
		panic(err)
	}
	ext, err := start(cfg)
	if err != nil {
		panic(err)
	}
	fmt.Println("READY", cfg.Ingress.NetAddr.Endpoint)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	if err := ext.Shutdown(context.Background()); err != nil {
		panic(err)
	}
}
