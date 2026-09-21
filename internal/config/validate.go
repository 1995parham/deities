package config

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/1995parham/deities/internal/controller"
	"github.com/go-playground/validator/v10"
	k8svalidation "k8s.io/apimachinery/pkg/util/validation"
)

// ValidationError reports every constraint that the loaded configuration
// violates. All violations are collected in a single pass so that one restart
// surfaces every mistake instead of only the first one.
type ValidationError struct {
	Violations []string
}

func (err ValidationError) Error() string {
	var out strings.Builder

	out.WriteString("invalid configuration:")

	for _, violation := range err.Violations {
		out.WriteString("\n  - ")
		out.WriteString(violation)
	}

	return out.String()
}

// Validate reports whether the configuration is usable. It checks the shape of
// every section through `validate` struct tags and, on top of that, the
// cross-field rules that tags cannot express (see validateControllerConfig).
func (c Config) Validate() error {
	validate, err := newValidator()
	if err != nil {
		return err
	}

	err = validate.Struct(c)
	if err == nil {
		return nil
	}

	fieldErrors, ok := errors.AsType[validator.ValidationErrors](err)
	if !ok {
		return fmt.Errorf("validating configuration: %w", err)
	}

	violations := make([]string, 0, len(fieldErrors))
	for _, fieldError := range fieldErrors {
		violations = append(violations, describe(fieldError))
	}

	return ValidationError{Violations: violations}
}

func newValidator() (*validator.Validate, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())

	// Report violations using the keys people actually write in config.toml
	// (and, with "__" between segments, in deities_* environment variables)
	// rather than Go field names.
	validate.RegisterTagNameFunc(func(field reflect.StructField) string {
		name, _, _ := strings.Cut(field.Tag.Get("koanf"), ",")
		if name == "" || name == "-" {
			return field.Name
		}

		return name
	})

	// Kubernetes object names have exact rules, and apimachinery is already a
	// dependency, so borrow its checks instead of approximating with a regex.
	for tag, isValid := range map[string]func(string) []string{
		"k8s_label":     k8svalidation.IsDNS1123Label,
		"k8s_subdomain": k8svalidation.IsDNS1123Subdomain,
	} {
		if err := validate.RegisterValidation(tag, k8sName(isValid)); err != nil {
			return nil, fmt.Errorf("registering %q validation: %w", tag, err)
		}
	}

	//nolint:exhaustruct_v5 // only the type is used, as a registration key.
	validate.RegisterStructValidation(validateControllerConfig, controller.Config{})

	return validate, nil
}

func k8sName(isValid func(string) []string) validator.Func {
	return func(field validator.FieldLevel) bool {
		return len(isValid(field.Field().String())) == 0
	}
}

// validateControllerConfig holds the rules that span more than one field:
// every image must point at a configured registry, and neither registries nor
// deployments may be declared twice.
func validateControllerConfig(sl validator.StructLevel) {
	cfg, ok := reflect.TypeAssert[controller.Config](sl.Current())
	if !ok {
		return
	}

	registries := make(map[string]struct{}, len(cfg.Registries))

	for i, reg := range cfg.Registries {
		if _, duplicate := registries[reg.Name]; duplicate {
			sl.ReportError(reg.Name, fmt.Sprintf("registries[%d].name", i), "Name", "unique", reg.Name)
		}

		registries[reg.Name] = struct{}{}
	}

	for i, img := range cfg.Images {
		// An empty registry is already reported by the `required` tag; do not
		// pile a second, more confusing violation on top of it.
		if img.Registry == "" {
			continue
		}

		if _, known := registries[img.Registry]; !known {
			sl.ReportError(
				img.Registry,
				fmt.Sprintf("images[%d].registry", i), "Registry", "registry_ref", img.Registry,
			)
		}
	}

	deployments := make(map[controller.Deployment]struct{}, len(cfg.Deployments))

	for i, deployment := range cfg.Deployments {
		if _, duplicate := deployments[deployment]; duplicate {
			sl.ReportError(
				deployment.Name,
				fmt.Sprintf("deployments[%d].name", i), "Name", "unique", deployment.Name,
			)
		}

		deployments[deployment] = struct{}{}
	}
}

// describe turns one field error into a line a human can act on without
// knowing that go-playground/validator exists.
func describe(fieldError validator.FieldError) string {
	key, _ := strings.CutPrefix(fieldError.Namespace(), "Config.")

	var reason string

	switch fieldError.Tag() {
	case "required":
		reason = "is required but missing or empty"
	case "gt":
		reason = fmt.Sprintf("must be greater than %s, got %v", fieldError.Param(), fieldError.Value())
	case "oneof":
		reason = fmt.Sprintf(
			"must be one of [%s], got %q",
			strings.ReplaceAll(fieldError.Param(), " ", ", "), fieldError.Value(),
		)
	case "k8s_label":
		reason = fmt.Sprintf(
			"must be a valid Kubernetes name: lowercase alphanumeric or '-', "+
				"starting and ending with an alphanumeric, at most 63 characters, got %q",
			fieldError.Value(),
		)
	case "k8s_subdomain":
		reason = fmt.Sprintf(
			"must be a valid Kubernetes name: lowercase alphanumeric, '-' or '.', "+
				"starting and ending with an alphanumeric, at most 253 characters, got %q",
			fieldError.Value(),
		)
	case "registry_ref":
		reason = fmt.Sprintf(
			"refers to registry %q, which is not declared under controller.registries",
			fieldError.Param(),
		)
	case "unique":
		reason = fmt.Sprintf("is declared more than once (%q)", fieldError.Param())
	default:
		reason = fmt.Sprintf("does not satisfy the %q constraint", fieldError.Tag())
	}

	return key + ": " + reason
}
