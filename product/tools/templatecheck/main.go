// Command templatecheck validates every directory of cubeship-templates.
package main

import (
	"cubeship/template"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: templatecheck <repository>")
		os.Exit(2)
	}
	dirs, err := os.ReadDir(os.Args[1])
	if err != nil {
		panic(err)
	}
	count, invalid := 0, 0
	slug := regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	for _, d := range dirs {
		if !d.IsDir() || d.Name() == ".git" || d.Name() == ".github" || d.Name() == "_cubeship" {
			continue
		}
		if !slug.MatchString(d.Name()) {
			fmt.Fprintln(os.Stderr, "invalid template directory:", d.Name())
			invalid++
			continue
		}
		count++
		root := filepath.Join(os.Args[1], d.Name())
		source, err := os.ReadFile(filepath.Join(root, "template.yaml"))
		if err != nil {
			fmt.Fprintln(os.Stderr, d.Name(), err)
			invalid++
			continue
		}
		result := template.Validate(source)
		if !result.OK || result.Manifest.Name == "" {
			fmt.Fprintln(os.Stderr, d.Name(), result.Diagnostics, "name is required")
			invalid++
		}
		for _, name := range []string{"README.md", "icon.png"} {
			info, err := os.Stat(filepath.Join(root, name))
			if err != nil {
				fmt.Fprintln(os.Stderr, d.Name(), err)
				invalid++
				continue
			}
			limit := int64(512 << 10)
			if info.Size() == 0 || info.Size() > limit {
				fmt.Fprintln(os.Stderr, d.Name(), name, "has an invalid size")
				invalid++
			}
		}
		icon, err := os.Open(filepath.Join(root, "icon.png"))
		if err == nil {
			config, e := png.DecodeConfig(icon)
			icon.Close()
			if e != nil || config.Width != config.Height || config.Width < 128 || config.Width > 1024 {
				fmt.Fprintln(os.Stderr, d.Name(), "icon.png must be a square PNG from 128 to 1024 pixels")
				invalid++
			}
		}
	}
	if count == 0 || invalid != 0 {
		fmt.Fprintf(os.Stderr, "%d templates, %d problems\n", count, invalid)
		os.Exit(1)
	}
	fmt.Printf("validated %d templates\n", count)
}
