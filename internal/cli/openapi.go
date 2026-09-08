package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/abn/relay/api/spec"
)

func newOpenAPICommand() *cobra.Command {
	var (
		version string
		asYAML  bool
	)

	cmd := &cobra.Command{
		Use:   "openapi",
		Short: "Print the OpenAPI specification",
		RunE: func(cmd *cobra.Command, args []string) error {
			if asYAML {
				yamlData, err := spec.OpenAPIYAML()
				if err != nil {
					return fmt.Errorf("getting openapi yaml: %w", err)
				}
				_, err = cmd.OutOrStdout().Write(yamlData)
				return err
			}

			var (
				data []byte
				err  error
			)
			switch version {
			case "3.1", "":
				data, err = spec.OpenAPISpec()
			case "3.0":
				data, err = spec.OpenAPI30Spec()
			default:
				return fmt.Errorf("unsupported openapi version %q (supported: 3.0, 3.1)", version)
			}

			if err != nil {
				return fmt.Errorf("reading openapi spec: %w", err)
			}

			_, err = cmd.OutOrStdout().Write(data)
			return err
		},
	}

	cmd.Flags().StringVar(&version, "version", "3.1", "OpenAPI version to output (3.0 or 3.1)")
	cmd.Flags().BoolVar(&asYAML, "yaml", false, "Output specification in YAML format")

	return cmd
}
