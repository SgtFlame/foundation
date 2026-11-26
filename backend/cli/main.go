/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package main

import (
	"github.com/SgtFlame/foundation/backend/cli/cmd"
	"github.com/SgtFlame/foundation/backend/features/text"
)

func main() {
	// A child started to parse one meeting transcript, isolated from the
	// server (forge 18e): it reads stdin, writes stdout and exits
	if text.TranscriptChildRequested() {
		text.ServeTranscriptChild()
		return
	}
	cmd.Execute()
}
