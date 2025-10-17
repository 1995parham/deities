package config

import (
	"encoding/json"
	"log"
	"os"
	"strings"

	"github.com/1995parham/deities/internal/controller"
	"github.com/1995parham/deities/internal/k8s"
	"github.com/1995parham/deities/internal/logger"
	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
	"github.com/tidwall/pretty"
	"go.uber.org/fx"
)

const prefix = "deities_"

// Config represents all application configurations.
type Config struct {
	fx.Out

	Controller controller.Config `json:"controller" koanf:"controller"`
	K8s        k8s.Config        `json:"k8s"        koanf:"k8s"`
	Logger     logger.Config     `json:"logger"     koanf:"logger"`
}

// Provide loads and provides the configuration.
func Provide() Config {
	k := koanf.New(".")

	// Load default configuration
	if err := k.Load(structs.Provider(Default(), "koanf"), nil); err != nil {
		log.Fatalf("error loading default: %s", err)
	}

	// Load configuration from file
	if err := k.Load(file.Provider("config.toml"), toml.Parser()); err != nil {
		log.Printf("error loading config.toml: %s", err)
	}

	// load environment variables
	if err := k.Load(
		// replace __ with . in environment variables so you can reference field a in struct b
		// as a__b.
		env.Provider(".", env.Opt{
			Prefix: prefix,
			TransformFunc: func(source string, value string) (string, any) {
				base := strings.ToLower(strings.TrimPrefix(source, prefix))

				return strings.ReplaceAll(base, "__", "."), value
			},
			EnvironFunc: os.Environ,
		},
		),
		nil,
	); err != nil {
		log.Printf("error loading environment variables: %s", err)
	}

	var instance Config
	if err := k.Unmarshal("", &instance); err != nil {
		log.Fatalf("error unmarshalling config: %s", err)
	}

	indent, err := json.MarshalIndent(instance, "", "\t")
	if err != nil {
		log.Fatalf("error marshalling config: %s", err)
	}

	indent = pretty.Color(indent, nil)

	log.Printf(`
================ Loaded Configuration ================
%s
======================================================
	`, string(indent))

	return instance
}
