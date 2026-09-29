// Command appmeta prints the metadata of an .apk or .ipa as JSON.
package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/mobile-next/appmeta"
)

type options struct {
	iconPath string
	noIcon   bool
}

func main() {
	cmd := newRootCommand(os.Stdout)
	if err := cmd.Execute(); err != nil {
		// Nothing is left to report to if stdout itself is what failed.
		_ = printJSON(os.Stdout, map[string]string{"error": err.Error()})
		os.Exit(1)
	}
}

func newRootCommand(out io.Writer) *cobra.Command {
	var opts options
	cmd := &cobra.Command{
		Use:           "appmeta [flags] <app.apk|app.ipa>",
		Short:         "Print the metadata of an Android or iOS app as JSON",
		Args:          cobra.ExactArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(out, args[0], opts)
		},
	}
	cmd.Flags().StringVar(&opts.iconPath, "icon", "", "also write the icon PNG to this file")
	cmd.Flags().BoolVar(&opts.noIcon, "no-icon", false, "omit the base64 icon from the JSON")
	return cmd
}

func run(out io.Writer, path string, opts options) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	stat, err := f.Stat()
	if err != nil {
		return err
	}
	info, err := appmeta.Parse(f, stat.Size())
	if err != nil {
		return err
	}
	if opts.iconPath != "" {
		if info.Icon == nil {
			return errors.New("the app has no extractable icon")
		}
		if err := os.WriteFile(opts.iconPath, info.Icon.PNG, 0o644); err != nil {
			return err
		}
	}
	if opts.noIcon && info.Icon != nil {
		info.Icon.PNG = nil
	}
	return printJSON(out, info)
}

func printJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
