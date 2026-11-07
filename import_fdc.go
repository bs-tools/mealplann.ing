package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/bs-tools/foodplann.ing/internal/fdc"
	"github.com/bs-tools/foodplann.ing/internal/foods"
	"github.com/bs-tools/foodplann.ing/internal/store"
)

// importFDCCommand is the subcommand importing USDA FoodData Central.
const importFDCCommand = "import-fdc"

// importFDC imports the FoodData Central datasets given as arguments, the
// Foundation Foods and SR Legacy CSV downloads from
// https://fdc.nal.usda.gov/download-datasets, as zip files or extracted.
// Importing again updates the foods.
func importFDC(args []string) error {
	fs := flag.NewFlagSet(importFDCCommand, flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: %s %s [flags] <dataset>...\n\n"+
			"Imports the Foundation Foods and SR Legacy CSV downloads of USDA FoodData Central\n"+
			"(https://fdc.nal.usda.gov/download-datasets), as zip files or extracted directories.\n\n",
			os.Args[0], importFDCCommand)
		fs.PrintDefaults()
	}
	databaseURL := fs.String("database-url", "", "The URL of the postgres database to import into")
	logLevel := fs.String("log-level", "info", "The log level to use")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if err := setFlagsFromEnv(fs); err != nil {
		return err
	}
	switch {
	case *databaseURL == "":
		return errors.New("database-url is required")
	case fs.NArg() == 0:
		fs.Usage()
		return errors.New("give the path of at least one dataset")
	}
	log, err := newLogger(*logLevel, "text")
	if err != nil {
		return err
	}
	dbURL, err := url.Parse(*databaseURL)
	if err != nil {
		return fmt.Errorf("failed to parse database-url: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := store.New(ctx, log, dbURL)
	if err != nil {
		return fmt.Errorf("failed to create store: %w", err)
	}
	defer db.Close()
	return fdc.Import(ctx, log, foods.NewRepository(log, db), fs.Args()...)
}
