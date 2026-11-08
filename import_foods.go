package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/bs-tools/mealplann.ing/internal/cnf"
	"github.com/bs-tools/mealplann.ing/internal/fdc"
	"github.com/bs-tools/mealplann.ing/internal/foodimport"
	"github.com/bs-tools/mealplann.ing/internal/foods"
	"github.com/bs-tools/mealplann.ing/internal/store"
)

// importCommand is a subcommand importing reference foods.
type importCommand struct {
	// name is the subcommand, e.g. "import-fdc".
	name string
	// source names what it imports in errors, e.g. "FoodData Central".
	source string
	// usage describes what it imports, after the usage line.
	usage string
	// run imports the downloads at paths.
	run func(ctx context.Context, log *slog.Logger, db foodimport.Upserter, paths ...string) error
}

// importCommands are the subcommands importing reference foods. Importing
// again updates the foods.
var importCommands = []importCommand{
	{
		name:   "import-fdc",
		source: "FoodData Central",
		usage: "Imports the Foundation Foods and SR Legacy CSV or JSON downloads of USDA FoodData Central\n" +
			"(https://fdc.nal.usda.gov/download-datasets), as zip files or extracted directories.\n" +
			"Take Foundation Foods as JSON: its CSV download of October 2026 has no foods or nutrients.",
		run: fdc.Import,
	},
	{
		name:   "import-cnf",
		source: "the Canadian Nutrient File",
		usage: "Imports the CSV download of Health Canada's Canadian Nutrient File\n" +
			"(https://www.canada.ca/en/health-canada/services/food-nutrition/healthy-eating/nutrient-data/canadian-nutrient-file-2015-download-files.html),\n" +
			"as a zip file or an extracted directory.",
		run: cnf.Import,
	},
}

// findImportCommand returns the import subcommand named name, nil if
// there's none.
func findImportCommand(name string) *importCommand {
	for i := range importCommands {
		if importCommands[i].name == name {
			return &importCommands[i]
		}
	}
	return nil
}

// runImport parses the subcommand's flags and imports the downloads given
// as arguments.
func (c *importCommand) runImport(args []string) error {
	fs := flag.NewFlagSet(c.name, flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: %s %s [flags] <download>...\n\n%s\n\n", os.Args[0], c.name, c.usage)
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
		return errors.New("give the path of at least one download")
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
	return c.run(ctx, log, foods.NewRepository(log, db), fs.Args()...)
}
