/*
Copyright Neo4j.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Command errorcatalog projects the catalog of internal/oracle onto its three consumers: the
// reason tables in the user-facing error reference, the condition table in the status contract,
// and the shell oracle the e2e asserts source. Run it through `make errors`, and `make
// errors-check` in CI to refuse a stale projection.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/neo4j/neo4j-kubernetes-operator/src/internal/oracle"
)

func main() {
	check := flag.Bool("check", false, "report stale projections instead of rewriting them")
	flag.Parse()

	root, err := moduleRoot()
	if err != nil {
		fail(err)
	}

	stale := 0
	if err := syncPage(root, oracle.DocPath, oracle.RenderMarkdown, *check, &stale); err != nil {
		fail(err)
	}
	if err := syncPage(root, oracle.StatusDocPath, oracle.RenderStatusMarkdown, *check, &stale); err != nil {
		fail(err)
	}
	if err := sync(filepath.Join(root, oracle.ShellPath), oracle.ShellPath, []byte(oracle.RenderShell()), *check, &stale); err != nil {
		fail(err)
	}

	if *check && stale > 0 {
		fail(fmt.Errorf("%d projection(s) out of date — run `make errors` and commit the result", stale))
	}
}

// syncPage rewrites the generated blocks of a page in place, leaving the hand-written prose alone.
func syncPage(root, rel string, render func(string) (string, error), check bool, stale *int) error {
	path := filepath.Join(root, rel)
	page, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", rel, err)
	}
	rendered, err := render(string(page))
	if err != nil {
		return err
	}
	return sync(path, rel, []byte(rendered), check, stale)
}

// sync writes body unless check is set, in which case it only counts the files that differ.
func sync(path, rel string, body []byte, check bool, stale *int) error {
	current, err := os.ReadFile(path)
	switch {
	case err == nil && string(current) == string(body):
		return nil
	case err != nil && !os.IsNotExist(err):
		return fmt.Errorf("read %s: %w", rel, err)
	}
	if check {
		*stale++
		fmt.Fprintf(os.Stderr, "stale: %s\n", rel)
		return nil
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", rel, err)
	}
	fmt.Printf("wrote %s\n", rel)
	return nil
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "errorcatalog: %v\n", err)
	os.Exit(1)
}
