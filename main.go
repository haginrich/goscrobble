package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	lastfm "github.com/p-mng/lastfm-go"
	"github.com/rodaine/table"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"
)

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
				Usage:  "Authenticate last.fm sand save session key and username",
				Action: ActionLastFmAuth,
				Arguments: []cli.Argument{
					&cli.StringArg{Name: "key"},
				},
			},
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Println("Error:", err.Error())
	}
}

func ActionRun(_ context.Context, cmd *cli.Command) error {
	SetupLogger(cmd)

	filename := ConfigFilename(cmd)
	config, err := ReadConfig(filename)
	if err != nil {
		log.Error().
			Err(err).
			Msg("error reading config file")
		return nil
	}

	RunMainLoop(config)

	return nil
}

func ActionScrobbles(_ context.Context, cmd *cli.Command) error {
	SetupLogger(cmd)

	limit := cmd.Int("limit")
	from := cmd.Timestamp("from")
	to := cmd.Timestamp("to")

	sinkName := cmd.StringArg("sink")

	if sinkName == "" {
		fmt.Println("No sink provided. Run `goscrobble list-sinks` to list all configured sinks.")
		return nil
	}
	filename := ConfigFilename(cmd)
	config, err := ReadConfig(filename)
	if err != nil {
		fmt.Println("Error reading config file:", err.Error())
		return nil
	}

	var sink Sink
	for _, s := range config.SetupSinks() {
		if s.Name() == sinkName {
			sink = s
			break
		}
	}

	if sink == nil {
		fmt.Println("Invalid sink name. Run `goscrobble list-sinks` to list all configured sinks.")
		return nil
	}

	scrobbles, err := sink.GetScrobbles(limit, from, to)
	if err != nil {
		fmt.Println("Error fetching scrobbles:", err.Error())
		return nil
	}

	tbl := table.New("ARTISTS", "TRACK", "ALBUM", "DURATION", "TIMESTAMP")
	for _, s := range scrobbles {
		tbl.AddRow(s.JoinArtists(), s.Track, s.Album, s.PrettyDuration(), s.Timestamp.Format(time.RFC1123))
	}
	tbl.Print()

	return nil
}

func ActionCheckConfig(_ context.Context, cmd *cli.Command) error {
	SetupLogger(cmd)

	filename := ConfigFilename(cmd)
	_, err := ReadConfig(filename)
	if err != nil {
		fmt.Println("Error reading config file:", err.Error())
		return nil
	}

	fmt.Println("Configuration is valid")
	return nil
}

func ActionListSources(_ context.Context, cmd *cli.Command) error {
	SetupLogger(cmd)

	filename := ConfigFilename(cmd)
	config, err := ReadConfig(filename)
	if err != nil {
		fmt.Println("Error reading config file:", err.Error())
		return nil
	}

	for _, sink := range config.SetupSources() {
		fmt.Println(sink.Name())
	}

	return nil
}

func ActionListSinks(_ context.Context, cmd *cli.Command) error {
	SetupLogger(cmd)

	filename := ConfigFilename(cmd)
	config, err := ReadConfig(filename)
	if err != nil {
		fmt.Println("Error reading config file:", err.Error())
		return nil
	}

	for _, sink := range config.SetupSinks() {
		fmt.Println(sink.Name())
	}

	return nil
}

func ActionLastFmAuth(_ context.Context, cmd *cli.Command) error {
	SetupLogger(cmd)

	key := cmd.StringArg("key")

	filename := ConfigFilename(cmd)
	config, err := ReadConfig(filename)
	if err != nil {
		fmt.Println("Error reading config file:", err.Error())
		return nil
	}

	if len(config.Sinks.LastFm) == 0 {
		fmt.Println("Error: no last.fm sink is configured")
		return nil
	} else if len(config.Sinks.LastFm) > 1 && key == "" {
		fmt.Println("Error: must specify a key when more than one last.fm sink is configured")
		return nil
	} else if _, ok := config.Sinks.LastFm[key]; !ok {
		fmt.Println("Error: no last.fm sink with this key exists")
		return nil
	}

	lastFmConfig := config.Sinks.LastFm[key]

	if lastFmConfig.SessionKey != "" && lastFmConfig.Username != "" {
		fmt.Println("last.fm is already authenticated")
		return nil
	}

	client, err := lastfm.NewDesktopClient(lastfm.BaseURL, lastFmConfig.Key, lastFmConfig.Secret)
	if err != nil {
		fmt.Println("Error setting up last.fm client:", err.Error())
		return nil
	}

	token, err := client.AuthGetToken()
	if err != nil {
		fmt.Println("Error getting authorization token:", err.Error())
		return nil
	}

	authURL := client.DesktopAuthorizationURL(token.Token)

	//nolint:gosec
	openBrowserCmd := exec.Command("/usr/bin/env", "xdg-open", authURL)
	if err := openBrowserCmd.Run(); err != nil {
		fmt.Println("Error opening URL in default browser:", err.Error())
	}

	fmt.Println("Please open the following URL in your browser and authorize the application:", authURL)
	fmt.Print("Finished authorization? [Y/n] ")

	input := bufio.NewScanner(os.Stdin)
	input.Scan()

	response := strings.ToLower(strings.TrimSpace(input.Text()))
	if response != "y" && response != "" {
		return nil
	}

	session, err := client.AuthGetSession(token.Token)
	if err != nil {
		fmt.Println("Error fetching session key from last.fm API:", err.Error())
		return nil
	}

	fmt.Println("Logged in with user:", session.Session.Name)

	lastFmConfig.SessionKey = session.Session.Key
	lastFmConfig.Username = session.Session.Name
	config.Sinks.LastFm[key] = lastFmConfig

	if err := config.Write(filename); err != nil {
		fmt.Println("Error writing updated config file:", err.Error())
		return nil
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
		return path.Join(ConfigDir(), DefaultConfigFileName)
	}
	return filename
}
