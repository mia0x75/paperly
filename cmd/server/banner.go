package main

import (
	"fmt"

	"log/slog"
)

// printBanner writes the boxed ASCII banner through the structured logger (not fmt.Println),
// matching the access-log format, with the version and build time centered in the box.
//
// Paperly's ASCII is provided by humans.
func printBanner() {
	const line = "_/_/_/_/_/_/_/_/_/_/_/_/_/_/_/_/_/_/_/_/_/_/_/_/_/_/_/_/"

	slog.Info(line)
	slog.Info("_/                                                    _/")
	slog.Info("_/  ██▀██ ██▀██ ██▀██ ██▀██ ██▀██ ██    ██ ██         _/")
	slog.Info("_/  ██▄█▀ ██▄██ ██▄█▀ ██▄   ██▄█▀ ██    ██▄██         _/")
	slog.Info("_/  ██    ██ ██ ██    ██ ▄▄ ██ ██ ██ ▄▄ ▄▄ ██         _/")
	slog.Info("_/  ▀▀    ▀▀ ▀▀ ▀▀    ▀▀▀▀▀ ▀▀ ▀▀ ▀▀▀▀▀ ▀▀▀▀▀         _/")
	slog.Info("_/                                                    _/")
	slog.Info(fmt.Sprintf("_/  Version: %-21s                    _/", version))
	slog.Info(fmt.Sprintf("_/  Built at: %-21s                   _/", buildTime))
	slog.Info("_/                                                    _/")
	slog.Info(line)
}
