package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	lastfm "github.com/p-mng/lastfm-go"
	"github.com/rodaine/table"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"
)

type ContextKey int

const ContextConfigKey ContextKey = iota

func main() {
	cmd := &cli.Command{
		Name:  "goscrobble",
		Usage: "A simple, cross-platform music scrobbler daemon",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "debug",
				Aliases: []string{"d"},
				Usage:   "print debug log messages",
			},
			&cli.BoolFlag{
				Name:    "json",
				Aliases: []string{"j"},
				Usage:   "print log messages in JSON format",
			},
			&cli.StringFlag{
				Name:    "config",
				Aliases: []string{"c"},
				Usage:   "use a different configuration file",
			},
		},
		Commands: []*cli.Command{
			{
				Name:   "run",
				Usage:  "Watch sources and send scrobbles to configured sinks",
				Action: ActionRun,
			},
			{
				Name:  "scrobbles",
				Usage: "Print scrobbles for the given sink",
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:    "limit",
						Aliases: []string{"l"},
						Value:   10,
						Usage:   "maximum number of scrobbles to display",
					},
					&cli.TimestampFlag{
						Name:        "from",
						Aliases:     []string{"f"},
						Value:       time.Now().Add(-14 * 24 * time.Hour),
						DefaultText: "current datetime minus 14 days",
						Usage:       "only display scrobbles after this time",
					},
					&cli.TimestampFlag{
						Name:        "to",
						Aliases:     []string{"t"},
						Value:       time.Now(),
						DefaultText: "current datetime",
						Usage:       "only display scrobbles before this time",
					},
				},
				Arguments: []cli.Argument{
					&cli.StringArg{Name: "sink"},
				},
				Action: ActionScrobbles,
			},
			{
				Name:   "check-config",
				Usage:  "Check the config file, creating it if needed",
				Action: ActionCheckConfig,
			},
			{
				Name:   "list-sources",
				Usage:  "Print all configured sources",
				Action: ActionListSources,
			},
			{
				Name:   "list-sinks",
				Usage:  "Print all configured sinks",
				Action: ActionListSinks,
			},
			{
				Name:   "lastfm-auth",
				Usage:  "Authenticate last.fm and save session key and username",
				Action: ActionLastFmAuth,
				Arguments: []cli.Argument{
					&cli.StringArg{Name: "key"},
				},
			},
		},
	}

	cmd.Before = func(ctx context.Context, _ *cli.Command) (context.Context, error) {
		SetupLogger(cmd)

		filename := ConfigFilename(cmd)
		config, err := ReadConfig(filename)
		if err != nil {
			return ctx, fmt.Errorf("cannot read config file: %s", err.Error())
		}

		ctx = context.WithValue(ctx, ContextConfigKey, config)

		return ctx, nil
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Println("Error:", err.Error())
	}
}

func ActionRun(ctx context.Context, _ *cli.Command) error {
	config := ctx.Value(ContextConfigKey).(Config)

	RunMainLoop(config)

	return nil
}

func ActionScrobbles(ctx context.Context, cmd *cli.Command) error {
	limit := cmd.Int("limit")
	from := cmd.Timestamp("from")
	to := cmd.Timestamp("to")

	sinkName := cmd.StringArg("sink")

	config := ctx.Value(ContextConfigKey).(Config)

	if sinkName == "" {
		return errors.New("no sink provided (run `goscrobble list-sinks` to list all configured sinks)")
	}

	var sink Sink
	for _, s := range config.SetupSinks() {
		if s.Name() == sinkName {
			sink = s
			break
		}
	}

	if sink == nil {
		return errors.New("invalid sink name (run `goscrobble list-sinks` to list all configured sinks)")
	}

	scrobbles, err := sink.GetScrobbles(limit, from, to)
	if err != nil {
		return fmt.Errorf("error fetching scrobbles: %s", err.Error())
	}

	tbl := table.New("ARTISTS", "TRACK", "ALBUM", "DURATION", "TIMESTAMP")
	for _, s := range scrobbles {
		tbl.AddRow(s.JoinArtists(), s.Track, s.Album, s.PrettyDuration(), s.Timestamp.Format(time.RFC1123))
	}
	tbl.Print()

	return nil
}

func ActionCheckConfig(ctx context.Context, _ *cli.Command) error {
	_ = ctx.Value(ContextConfigKey).(Config)

	fmt.Println("Configuration is valid")
	return nil
}

func ActionListSources(ctx context.Context, _ *cli.Command) error {
	config := ctx.Value(ContextConfigKey).(Config)

	for _, sink := range config.SetupSources() {
		fmt.Println(sink.Name())
	}

	return nil
}

func ActionListSinks(ctx context.Context, _ *cli.Command) error {
	config := ctx.Value(ContextConfigKey).(Config)

	for _, sink := range config.SetupSinks() {
		fmt.Println(sink.Name())
	}

	return nil
}

func ActionLastFmAuth(ctx context.Context, cmd *cli.Command) error {
	key := cmd.StringArg("key")

	config := ctx.Value(ContextConfigKey).(Config)

	if len(config.Sinks.LastFm) == 0 {
		return errors.New("no last.fm sink is configured")
	} else if len(config.Sinks.LastFm) > 1 && key == "" {
		return errors.New("must specify a key when more than one last.fm sink is configured")
	} else if _, ok := config.Sinks.LastFm[key]; !ok {
		return errors.New("no last.fm sink with this key exists")
	}

	lastFmConfig := config.Sinks.LastFm[key]

	if lastFmConfig.SessionKey != "" && lastFmConfig.Username != "" {
		return errors.New("last.fm is already authenticated")
	}

	client, err := lastfm.NewDesktopClient(lastfm.BaseURL, lastFmConfig.Key, lastFmConfig.Secret)
	if err != nil {
		return fmt.Errorf("cannot set up last.fm client: %s", err.Error())
	}

	token, err := client.AuthGetToken()
	if err != nil {
		return fmt.Errorf("cannot get authorization token: %s", err.Error())
	}

	fmt.Println("Warning: authenticating last.fm will rewrite your config file and remove all comments!")

	authURL := client.DesktopAuthorizationURL(token.Token)
	if err := OpenURL(authURL); err != nil {
		fmt.Println("Error opening URL in default browser:", err.Error())
	}

	fmt.Println("Please open the following URL in your browser and authorize the application:", authURL)
	fmt.Print("Finished authorization? [Y/n] ")

	input := bufio.NewScanner(os.Stdin)
	input.Scan()

	response := strings.ToLower(strings.TrimSpace(input.Text()))
	if response != "y" && response != "" {
		return errors.New("invalid input")
	}

	session, err := client.AuthGetSession(token.Token)
	if err != nil {
		return fmt.Errorf("cannot fetch session key from last.fm API: %s", err.Error())
	}

	fmt.Println("Logged in with user:", session.Session.Name)

	lastFmConfig.SessionKey = session.Session.Key
	lastFmConfig.Username = session.Session.Name
	config.Sinks.LastFm[key] = lastFmConfig

	filename := ConfigFilename(cmd)

	if err := config.Write(filename); err != nil {
		return fmt.Errorf("cannnot write updated config file: %s", err.Error())
	}

	return nil
}

func SetupLogger(cmd *cli.Command) {
	debug := cmd.Bool("debug")
	json := cmd.Bool("json")

	log.Logger = log.With().Caller().Logger()

	if debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	if json {
		log.Logger = log.Output(os.Stderr)
	} else {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}
}

func ConfigFilename(cmd *cli.Command) string {
	filename := cmd.String("config")
	if filename == "" {
		return filepath.Join(ConfigDir(), DefaultConfigFileName)
	}
	return filename
}
