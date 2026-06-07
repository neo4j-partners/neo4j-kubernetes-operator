package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
)

func loadSamples(samplesDir string) ([]map[string]any, error) {
	entries, err := os.ReadDir(samplesDir)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") || name == "kustomization.yaml" {
			continue
		}
		files = append(files, filepath.Join(samplesDir, name))
	}
	sort.Strings(files)

	var samples []map[string]any
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		decoder := yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
		for {
			var doc map[string]any
			if err := decoder.Decode(&doc); err != nil {
				if err == io.EOF {
					break
				}
				return nil, err
			}
			if len(doc) == 0 {
				continue
			}
			samples = append(samples, doc)
		}
	}

	return samples, nil
}

func updateCSV(path string, almValue string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	almUpdated := false
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if !strings.Contains(line, "alm-examples:") {
			continue
		}
		indent := line[:strings.Index(line, "alm-examples:")]
		baseIndent := len(indent)
		lines[i] = fmt.Sprintf("%salm-examples: %s", indent, almValue)
		j := i + 1
		for j < len(lines) && len(lines[j])-len(strings.TrimLeft(lines[j], " ")) > baseIndent {
			j++
		}
		if j > i+1 {
			lines = append(lines[:i+1], lines[j:]...)
		}
		almUpdated = true
		break
	}

	if !almUpdated {
		return false, nil
	}

	lines = setMinKubeVersion(lines, "1.32.0")
	output := strings.Join(lines, "\n") + "\n"
	return true, os.WriteFile(path, []byte(output), 0o644)
}

func setMinKubeVersion(lines []string, version string) []string {
	for i, line := range lines {
		if !strings.Contains(line, "minKubeVersion:") {
			continue
		}
		indent := line[:strings.Index(line, "minKubeVersion:")]
		lines[i] = fmt.Sprintf("%sminKubeVersion: %q", indent, version)
		return lines
	}

	for i, line := range lines {
		if strings.TrimSpace(line) != "spec:" {
			continue
		}
		indent := line[:strings.Index(line, "spec:")]
		newLine := fmt.Sprintf("%s  minKubeVersion: %q", indent, version)
		lines = append(lines[:i+1], append([]string{newLine}, lines[i+1:]...)...)
		return lines
	}

	return lines
}

func main() {
	samplesDir := flag.String("samples-dir", "config/samples", "Directory with sample CRs")
	flag.Parse()
	csvPaths := flag.Args()
	if len(csvPaths) == 0 {
		fmt.Fprintln(os.Stderr, "usage: update-alm-examples [--samples-dir <dir>] <csv path> [<csv path>...]")
		os.Exit(2)
	}

	if _, err := os.Stat(*samplesDir); err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "samples directory not found (%s); skipping alm-examples update\n", *samplesDir)
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "stat samples dir: %v\n", err)
		os.Exit(1)
	}

	samples, err := loadSamples(*samplesDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load samples: %v\n", err)
		os.Exit(1)
	}

	almJSON, err := json.MarshalIndent(samples, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal samples: %v\n", err)
		os.Exit(1)
	}
	almValueBytes, err := json.Marshal(string(almJSON))
	if err != nil {
		fmt.Fprintf(os.Stderr, "encode alm-examples: %v\n", err)
		os.Exit(1)
	}
	almValue := string(almValueBytes)

	updated := false
	for _, csvPath := range csvPaths {
		ok, err := updateCSV(csvPath, almValue)
		if err != nil {
			fmt.Fprintf(os.Stderr, "update %s: %v\n", csvPath, err)
			os.Exit(1)
		}
		updated = updated || ok
	}

	if !updated {
		fmt.Fprintln(os.Stderr, "no alm-examples entry updated")
		os.Exit(1)
	}
}
