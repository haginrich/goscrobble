package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
)

const DefaultEddyEndpoint = "http://localhost:3665/now-playing"

type EddyAPIResponse struct {
	Item struct {
		ID       int    `json:"id"`
		Title    string `json:"title"`
		Duration int    `json:"duration"`
		Version  string `json:"version"`
		URL      string `json:"url"`
		Artists  []struct {
			ID      int    `json:"id"`
			Name    string `json:"name"`
			Type    string `json:"type"`
			Picture string `json:"picture"`
			Handle  any    `json:"handle"`
			UserID  any    `json:"userId"`
		} `json:"artists"`
		Album struct {
			ID           int    `json:"id"`
			Title        string `json:"title"`
			Cover        string `json:"cover"`
			VibrantColor string `json:"vibrantColor"`
			VideoCover   any    `json:"videoCover"`
			URL          string `json:"url"`
			ReleaseDate  string `json:"releaseDate"`
			AI           bool   `json:"ai"`
		} `json:"album"`
		Explicit               bool     `json:"explicit"`
		VolumeNumber           int      `json:"volumeNumber"`
		TrackNumber            int      `json:"trackNumber"`
		Popularity             int      `json:"popularity"`
		DoublePopularity       float64  `json:"doublePopularity"`
		AllowStreaming         bool     `json:"allowStreaming"`
		StreamReady            bool     `json:"streamReady"`
		StreamStartDate        string   `json:"streamStartDate"`
		AdSupportedStreamReady bool     `json:"adSupportedStreamReady"`
		DJReady                bool     `json:"djReady"`
		StemReady              bool     `json:"stemReady"`
		Editable               bool     `json:"editable"`
		ReplayGain             float64  `json:"replayGain"`
		AudioQuality           string   `json:"audioQuality"`
		AudioModes             []string `json:"audioModes"`
		Mixes                  struct {
			TrackMix string `json:"TRACK_MIX"`
		} `json:"mixes"`
		MediaMetadata struct {
			Tags []string `json:"tags"`
		} `json:"mediaMetadata"`
		Upload      bool   `json:"upload"`
		PayToStream bool   `json:"payToStream"`
		AccessType  string `json:"accessType"`
		Spotlighted bool   `json:"spotlighted"`
		AI          bool   `json:"ai"`
		ContentType string `json:"contentType"`
	} `json:"item"`
	Position    float64 `json:"position"`
	Duration    int     `json:"duration"`
	AlbumArt    string  `json:"albumArt"`
	ArtistArt   any     `json:"artistArt"`
	Paused      bool    `json:"paused"`
	LastUpdate  int64   `json:"lastUpdate"`
	CurrentTime int64   `json:"currentTime"`
}

type EddyAPISource struct {
	Client         http.Client
	Endpoint       string
	IncludeVersion bool
}

func (s EddyAPISource) Name() string {
	return "TidaLuna/EddyAPI"
}

func (s EddyAPISource) GetInfo() (map[string]PlaybackStatus, error) {
	response, err := s.Client.Get(s.Endpoint)
	if err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) {
			log.Debug().Msg("connection to EddyAPI refused; is the plugin installed and loaded?")
			return map[string]PlaybackStatus{}, nil
		}
		return nil, err
	}
	defer CloseLogged(response.Body)

	var body EddyAPIResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return nil, err
	}

	var state PlaybackState
	if body.Paused {
		state = PlaybackPaused
	} else {
		state = PlaybackPlaying
	}

	var track string
	if s.IncludeVersion {
		track = fmt.Sprintf("%s (%s)", body.Item.Title, body.Item.Version)
	} else {
		track = body.Item.Title
	}

	var artists []string
	for _, artist := range body.Item.Artists {
		artists = append(artists, artist.Name)
	}

	info := PlaybackStatus{
		Scrobble: Scrobble{
			Artists:   artists,
			Track:     track,
			Album:     body.Item.Album.Title,
			Duration:  time.Duration(body.Duration * int(time.Second)),
			Timestamp: time.Time{},
		},
		State:    state,
		Position: time.Duration(body.Position * float64(time.Second)),
	}

	return map[string]PlaybackStatus{
		fmt.Sprintf("%s:%s", s.Name(), s.Endpoint): info,
	}, nil
}
