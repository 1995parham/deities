package logo

import (
	"github.com/pterm/pterm"
	"github.com/pterm/pterm/putils"
)

// Print displays the colorful Deities logo.
func Print() {
	s, _ := pterm.DefaultBigText.WithLetters(
		putils.LettersFromStringWithStyle("D", pterm.NewStyle(pterm.FgCyan)),
		putils.LettersFromStringWithStyle("eities", pterm.NewStyle(pterm.FgLightBlue)),
	).Srender()

	pterm.DefaultCenter.Println(s)

	subtitle := pterm.DefaultCenter.Sprint("Kubernetes Image Digest Monitor")

	pterm.Println()
	pterm.Println(pterm.LightBlue(subtitle))
	pterm.Println()
}
