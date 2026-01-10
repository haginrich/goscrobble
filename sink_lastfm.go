package main

import (
	"errors"
	"strings"
	"time"

	lastfm "github.com/p-mng/lastfm-go"
	"github.com/rs/zerolog/log"
)

type LastFmSink struct {
	Client     lastfm.Client
	SessionKey string
	Username   string
}

func LastFmSinkFromConfig(c LastFmConfig) (LastFmSink, error) {
	var sink LastFmSink

	if c.SessionKey == "" || c.Username == "" {
		return sink, errors.New("last.fm sink is configured, but not authenticated")
	}

	var baseURL string
	if c.BaseURL != "" {
		baseURL = c.BaseURL
	} else {
		baseURL = lastfm.BaseURL
	}

	client, err := lastfm.NewDesktopClient(baseURL, c.Key, c.Secret)
	if err != nil {
		return sink, err
	}

	return LastFmSink{Client: client, SessionKey: c.SessionKey, Username: c.Username}, nil
}

func (s LastFmSink) Name() string {
	return "last.fm"
}

func (s LastFmSink) NowPlaying(scrobble Scrobble) error {
	_, err := s.Client.TrackUpdateNowPlaying(lastfm.P{
		"artist":   scrobble.JoinArtists(),
		"track":    scrobble.Track,
		"album":    scrobble.Album,
		"duration": max(int(scrobble.Duration.Seconds()), 30),
		"sk":       s.SessionKey,
	})
	return err
}

func (s LastFmSink) Scrobble(scrobble Scrobble) error {
	_, err := s.Client.TrackScrobble(lastfm.P{
		"artist":    scrobble.JoinArtists(),
		"track":     scrobble.Track,
		"album":     scrobble.Album,
		"duration":  max(int(scrobble.Duration.Seconds()), 30),
		"timestamp": scrobble.Timestamp.Unix(),
		"sk":        s.SessionKey,
	})
	return err
}

func (s LastFmSink) GetScrobbles(limit int, from, to time.Time) ([]Scrobble, error) {
	currentPage := 1
	totalPages := int64(1)

	log.Debug().Msg("loading scrobbles from last.fm API")

	noLimit := limit <= 0

	var scrobbles []Scrobble
outer:
	for {
		page, err := s.Client.UserGetRecentTracks(lastfm.P{
			"limit":    min(limit, 200),
			"user":     s.Username,
			"page":     currentPage,
			"from":     from.Unix(),
			"extended": 1,
			"to":       to.Unix(),
		})
		if err != nil {
			return nil, err
		}

		if page.RecentTracks.TotalPages != totalPages {
			totalPages = page.RecentTracks.TotalPages
		}

		for _, track := range page.RecentTracks.Tracks {
			if noLimit || len(scrobbles) < limit {
				scrobbles = append(scrobbles, Scrobble{
					// FIXME: this does not work in some cases (e.g., "Tyler, the Creator")
					Artists:   strings.Split(track.Artist.Name, ", "),
					Track:     track.Name,
					Album:     track.Album.Name,
					Duration:  time.Duration(0),
					Timestamp: time.Unix(track.Date.UTS, 0),
				})
			} else {
				break outer
			}
		}

		if int64(currentPage) >= totalPages {
			break outer
		}
		currentPage++
	}

	return scrobbles, nil
}
