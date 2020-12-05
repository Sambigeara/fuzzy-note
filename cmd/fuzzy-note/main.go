package main

import (
	"log"
	"os"
	"path"
	"strings"

	"fuzzy-note/pkg/client"
	"fuzzy-note/pkg/service"
	//"github.com/Sambigeara/fuzzy-note/pkg/client"
	//"github.com/Sambigeara/fuzzy-note/pkg/service"
)

const rootFileName = "primary.db"
const eventLogFileName = "event_log.db"

func main() {
	var rootDir, notesSubDir string
	if rootDir = os.Getenv("FZN_ROOT_DIR"); rootDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
			os.Exit(1)
		}
		rootDir = path.Join(home, ".fzn/")
	}
	if notesSubDir = os.Getenv("FZN_NOTES_SUBDIR"); notesSubDir == "" {
		notesSubDir = "notes"
	}

	rootPath := path.Join(rootDir, rootFileName)
	eventLogPath := path.Join(rootDir, eventLogFileName)
	notesDir := path.Join(rootDir, notesSubDir)

	// Create (if not exists) the notes subdirectory
	os.MkdirAll(notesDir, os.ModePerm)

	listDBHandler := service.NewListItemDBHandler(notesDir)
	eventLogHandler := service.NewEventLogDBHandler(eventLogPath)
	listRepo := service.NewDBListRepo(rootPath, listDBHandler, eventLogHandler)

	// Set colourscheme
	fznColour := strings.ToLower(os.Getenv("FZN_COLOUR"))
	if fznColour == "" || (fznColour != "light" && fznColour != "dark") {
		fznColour = "light"
	}

	term := client.NewTerm(listRepo, fznColour)

	err := term.RunClient()
	if err != nil {
		log.Fatal(err)
	}
}
