// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"fmt"
	"os"
	"path"

	"github.com/IBM/license-scanner/identifier"
	"github.com/IBM/license-scanner/licenses"
	"github.com/IBM/license-scanner/normalizer"

	"golang.org/x/exp/slices"
)

func ValidateSPDXTemplateWithLicenseText(id, templateFile, textFile, templateDestDir, preCheckDestDir, textDestDir string) (bool, error) {
	const isValid = false // return false for all skips and errors -- only return true at the very end

	// Skipping known SPDX template/text issues.  Some here. More in later steps below.
	skips := []string{
		"deprecated_Nokia-Qt-exception-1.1", // no test file
	}

	if slices.Contains(skips, id) {
		return isValid, nil
	}

	textBytes, err := os.ReadFile(textFile)
	if err != nil {
		return isValid, err
	}
	normalizedTestData := normalizer.NormalizationData{
		OriginalText: string(textBytes),
	}
	if err := normalizedTestData.NormalizeText(); err != nil {
		return isValid, err
	}

	templateBytes, err := os.ReadFile(templateFile)
	if err != nil {
		return isValid, err
	}

	l := &licenses.License{}
	if err := licenses.AddPrimaryPatternAndSource(string(templateBytes), templateFile, l); err != nil {
		return isValid, err
	}

	if _, err := licenses.GenerateMatchingPatternFromSourceText(l.PrimaryPatterns[0]); err != nil {
		return isValid, err
	}

	// SPDX template/text issues
	skips = []string{
		"Afmparse", // missing space
		"BlueOak-1.0.0",
		"CC-BY-3.0",
		"CC-BY-NC-SA-2.0-FR",
		"CC-BY-SA-3.0",
		"CECILL-2.0",
		"CECILL-2.1",
		"CECILL-B",
		"CECILL-C",
		"COIL-1.0",
		"Community-Spec-1.0",
		"D-FSL-1.0",
		"EPL-1.0",
		"EUDatagrid",
		"EUPL-1.2",
		"Elastic-2.0",
		"ErlPL-1.1",
		"FreeImage",
		"IBM-pibs",
		"LAL-1.2",
		"LAL-1.3",
		"LPL-1.0",
		"LZMA-SDK-9.11-to-9.20",
		"LZMA-SDK-9.22",
		"LiLiQ-Rplus-1.1",
		"MulanPSL-1.0",
		"Multics",
		"NCGL-UK-2.0",
		"NPL-1.1",
		"OGL-UK-1.0",
		"OSET-PL-2.1",
		"Parity-7.0.0",
		"PolyForm-Noncommercial-1.0.0",
		"PolyForm-Small-Business-1.0.0",
		"SHL-2.0",
		"SHL-2.1",
		"SSPL-1.0",
		"W3C-19980720",
		"copyleft-next-0.3.0",
		"copyleft-next-0.3.1",
		"iMatix",
	}

	matches, err := identifier.FindMatchingPatternInNormalizedData(l.PrimaryPatterns[0], normalizedTestData)
	if err != nil {
		return isValid, err
	}

	// There should be exactly ONE match when matching a template against its example license text
	if len(matches) != 1 {
		err = Logger.Errorf("expected 1 match for %v got: %v", id, matches)
		if slices.Contains(skips, id) {
			return isValid, nil // skip with known err
		} else {
			return isValid, err
		}
	}

	// Normalize the input text.
	normalizedTemplate := &normalizer.NormalizationData{
		OriginalText: string(templateBytes),
	}
	err = normalizedTemplate.NormalizeText()
	if err != nil {
		return isValid, err
	}

	staticBlocks := GetStaticBlocks(normalizedTemplate)
	passed := identifier.PassedStaticBlocksChecks(staticBlocks, normalizedTestData)
	if !passed {
		return isValid, Logger.Errorf("%v failed testing against static blocks", id)
	}

	err = WritePreChecksFile(staticBlocks, path.Join(preCheckDestDir, id+".json"))
	if err != nil {
		return isValid, err
	}

	err = os.WriteFile(path.Join(templateDestDir, id+".template.txt"), templateBytes, 0o666) //nolint:gosec
	if err != nil {
		return isValid, fmt.Errorf("error writing template for %v: %w", id, err)
	}

	err = os.WriteFile(path.Join(textDestDir, id+".txt"), textBytes, 0o666) //nolint:gosec
	if err != nil {
		return isValid, fmt.Errorf("error writing testdata for %v: %w", id, err)
	}

	return true, nil
}
