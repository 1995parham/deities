package config

import (
	"encoding/json"
	"fmt"
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

type Config struct {
	fx.Out `validate:"-"`

	Controller controller.Config `json:"controller" koanf:"controller"`
	K8s        k8s.Config        `json:"k8s"        koanf:"k8s"`
	Logger     logger.Config     `json:"logger"     koanf:"logger"`
}

// Provide loads the configuration from defaults, config.toml and the
// environment (in that order of precedence) and returns it only once it has
// passed validation. Returning the error rather than exiting lets fx report a
// bad configuration as a startup failure, and lets tests assert on it.
func Provide() (Config, error) {
	k := koanf.New(".")

	if err := k.Load(structs.Provider(Default(), "koanf"), nil); err != nil {
		return Config{}, fmt.Errorf("loading defaults: %w", err) //nolint:exhaustruct_v5 // zero value on error.
	}

	if err := k.Load(file.Provider("config.toml"), toml.Parser()); err != nil {
		log.Printf("error loading config.toml: %s", err)
	}

	if err := k.Load(
		env.Provider(".", env.Opt{
			Prefix: prefix,
			TransformFunc: func(source string, value string) (string, any) {
				base := strings.ToLower(strings.TrimPrefix(source, prefix))

				return strings.ReplaceAll(base, "__", "."), value
			},
			EnvironFunc: os.Environ,
		}),
		nil,
	); err != nil {
		log.Printf("error loading environment variables: %s", err)
	}

	var instance Config
	if err := k.Unmarshal("", &instance); err != nil {
		return Config{}, fmt.Errorf("unmarshalling configuration: %w", err) //nolint:exhaustruct_v5 // zero value on error.
	}

	dump(instance)

	if err := instance.Validate(); err != nil {
		return Config{}, err //nolint:exhaustruct_v5 // zero value on error.
	}

	return instance, nil
}

// dump writes the effective configuration so that a failed validation can be
// read against the values that produced it.
func dump(instance Config) {
	indent, err := json.MarshalIndent(instance, "", "\t")
	if err != nil {
		log.Printf("error marshalling config: %s", err)

		return
	}

	indent = pretty.Color(indent, nil)

	log.Printf(`
================ Loaded Configuration ================
%s
======================================================
	`, string(indent))
}
