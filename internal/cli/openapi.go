package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

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
			var filename string
			switch version {
			case "3.1", "":
				filename = "openapi.json"
			case "3.0":
				filename = "openapi-3.0.json"
			default:
				return fmt.Errorf("unsupported openapi version %q (supported: 3.0, 3.1)", version)
			}

			data, err := spec.FS.ReadFile(filename)
			if err != nil {
				return fmt.Errorf("reading openapi spec: %w", err)
			}

			if asYAML {
				var parsed any
				if err := yaml.Unmarshal(data, &parsed); err != nil {
					return fmt.Errorf("parsing openapi spec to yaml: %w", err)
				}
				yamlData, err := yaml.Marshal(parsed)
				if err != nil {
					return fmt.Errorf("marshaling openapi spec to yaml: %w", err)
				}
				_, err = cmd.OutOrStdout().Write(yamlData)
				return err
			}

			_, err = cmd.OutOrStdout().Write(data)
			return err
		},
	}

	cmd.Flags().StringVar(&version, "version", "3.1", "OpenAPI version to output (3.0 or 3.1)")
	cmd.Flags().BoolVar(&asYAML, "yaml", false, "Output specification in YAML format")

	return cmd
}
