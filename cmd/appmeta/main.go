// Command appmeta prints the metadata of an .apk or .ipa as JSON.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/mobile-next/appmeta"
)

func main() {
	iconPath := flag.String("icon", "", "write the icon PNG to this `file`")
	noIcon := flag.Bool("no-icon", false, "omit the base64 icon from the JSON")
	flag.Usage = func() {
		_, _ = fmt.Fprintf(flag.CommandLine.Output(), "usage: appmeta [--icon out.png] [--no-icon] app.apk|app.ipa\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(os.Stdout, flag.Arg(0), *iconPath, *noIcon); err != nil {
		printJSON(os.Stdout, map[string]string{"error": err.Error()})
		os.Exit(1)
	}
}

func run(out io.Writer, path, iconPath string, noIcon bool) error {
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
	if iconPath != "" {
		if info.Icon == nil {
			return errors.New("the app has no extractable icon")
		}
		if err := os.WriteFile(iconPath, info.Icon.PNG, 0o644); err != nil {
			return err
		}
	}
	if noIcon && info.Icon != nil {
		info.Icon.PNG = nil
	}
	printJSON(out, info)
	return nil
}

func printJSON(out io.Writer, v any) {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
