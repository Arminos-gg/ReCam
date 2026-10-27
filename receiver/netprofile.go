package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// warnPublicNetworks prints a hint for every Windows network marked Public. The
// firewall rule Windows offers on first start covers Private networks only, so the
// phone cannot reach the receiver through a Public one.
func warnPublicNetworks() {
	if runtime.GOOS != "windows" {
		return
	}
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		`Get-NetConnectionProfile | ForEach-Object { "$($_.InterfaceAlias)|$($_.NetworkCategory)" }`).Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		alias, category, ok := strings.Cut(strings.TrimSpace(line), "|")
		if ok && category == "Public" {
			fmt.Printf("  Note: Windows treats the network on %q as Public, so the firewall can block the phone there.\n"+
				"        Set it to Private: Settings > Network & internet > %s > Network profile type.\n\n", alias, alias)
		}
	}
}
