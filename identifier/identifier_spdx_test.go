// SPDX-License-Identifier: Apache-2.0

//go:build unit

package identifier

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IBM/license-scanner/configurer"

	"github.com/IBM/license-scanner/licenses"
)

const (
	resources = "../resources"
	spdx      = "default"
)

var testDataDir = path.Join(resources, "spdx", spdx, "testdata")

func Test_identifyLicensesInSPDXTestData(t *testing.T) {
	if _, err := os.Stat(testDataDir); os.IsNotExist(err) {
		// Skip test if this data isn't in place (in repo) yet. Else continue for identify errors.
		t.Skipf("Skipping test with optional resources: %v", resources)
	}

	config, err := configurer.InitConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	config.Set("resources", resources) // override
	config.Set("spdx", spdx)           // override

	licenseLibrary, err := licenses.NewLicenseLibrary(config)
	if err != nil {
		t.Fatalf("NewLicenseLibrary() error = %v", err)
	}
	if err := licenseLibrary.AddAllSPDX(); err != nil {
		t.Fatalf("licenseLibrary.AddAll() error = %v", err)
	}

	options := Options{
		ForceResult: false,
		Enhancements: Enhancements{
			AddNotes:       "",
			AddTextBlocks:  true,
			FlagAcceptable: false,
			FlagCopyrights: true,
			FlagKeywords:   false,
		},
	}

	type tf struct {
		name string
		path string
	}

	var tfs []tf

	// type WalkDirFunc func(path string, d DirEntry, err error) error
	err = filepath.WalkDir(testDataDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			fmt.Printf("prevent panic by handling failure accessing a path %q: %v\n", path, err)
			return err
		}
		if !d.IsDir() {
			tfs = append(tfs, tf{name: d.Name(), path: path})
		}
		return nil
	})

	if err != nil {
		fmt.Printf("error walking the path %v: %v\n", resources, err)
		return
	}

	for _, tc := range tfs {
		tc := tc
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			wantLicenseID := strings.TrimSuffix(tc.name, ".txt")
			wantLicenseID = strings.TrimPrefix(wantLicenseID, "deprecated_")
			got, err := IdentifyLicensesInFile(tc.path, options, licenseLibrary)
			if err != nil {
				t.Errorf("IdentifyLicensesInFile(%v) err = %v", tc.path, err)
			}

			if _, ok := got.Matches[wantLicenseID]; ok {
				t.Logf("GOT %v", wantLicenseID)
			} else {
				t.Errorf("IdentifyLicensesInFile() mismatched. want = %+v, got %v", wantLicenseID, got)
			}
		})
	}
}
