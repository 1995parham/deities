package logo

import (
	"github.com/pterm/pterm"
)

// Print displays the colorful Deities logo.
func Print() {
	// Create a big text with gradient colors
	s, _ := pterm.DefaultBigText.WithLetters(
		pterm.NewLettersFromStringWithStyle("D", pterm.NewStyle(pterm.FgCyan)),
		pterm.NewLettersFromStringWithStyle("eities", pterm.NewStyle(pterm.FgLightBlue)),
	).Srender()

	pterm.DefaultCenter.Println(s)

	// Print subtitle
	subtitle := pterm.DefaultCenter.Sprint("Kubernetes Image Digest Monitor")
	pterm.Println()
	pterm.Println(pterm.LightBlue(subtitle))
	pterm.Println()
}
