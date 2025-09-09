package main

import (
	"github.com/wildeyedskies/go-mpv/mpv"
	"strconv"
)

const (
	PlayerStopped = iota
	PlayerPlaying
	PlayerPaused
	PlayerError
)

type QueueItem struct {
	Id       string
	Uri      string
	Title    string
	Artist   string
	Duration int
}

type Playlist struct {
    ID     string
    Name   string
    Tracks []QueueItem
}


type Player struct {
	Instance          *mpv.Mpv
	EventChannel      chan *mpv.Event
	Playlists         []Playlist
    ActiveListIdx     int // which playlist is active
    CurrentIndex      int // which track in that playlist
    ReplaceInProgress bool
}

func eventListener(m *mpv.Mpv) chan *mpv.Event {
	c := make(chan *mpv.Event)
	go func() {
		for {
			e := m.WaitEvent(1)
			c <- e
		}
	}()
	return c
}

func InitPlayer() (*Player, error) {
	mpvInstance := mpv.Create()

	// TODO figure out what other mpv options we need
	mpvInstance.SetOptionString("audio-display", "no")
	mpvInstance.SetOptionString("video", "no")


	err := mpvInstance.Initialize()
	if err != nil {
		mpvInstance.TerminateDestroy()
		return nil, err
	}

	return &Player{
        Instance:          mpvInstance,
        EventChannel:      eventListener(mpvInstance),
        Playlists:         []Playlist{},
        ActiveListIdx:     -1,
        CurrentIndex:      -1,
        ReplaceInProgress: false,
    }, nil
}

func (p *Player) CurrentPlaylist() *Playlist {
    if p.ActiveListIdx >= 0 && p.ActiveListIdx < len(p.Playlists) {
        return &p.Playlists[p.ActiveListIdx]
    }
    return nil
}

func (p *Player) CurrentTrack() *QueueItem {
    pl := p.CurrentPlaylist()
    if pl == nil {
        return nil
    }
    if p.CurrentIndex >= 0 && p.CurrentIndex < len(pl.Tracks) {
        return &pl.Tracks[p.CurrentIndex]
    }
    return nil
}

func (p *Player) PlayPlaylist(idx int) error {
    if idx < 0 || idx >= len(p.Playlists) {
        return nil
    }

    p.ActiveListIdx = idx
    p.CurrentIndex = 0
    track := p.Playlists[idx].Tracks[p.CurrentIndex]
    p.ReplaceInProgress = true

    return p.Instance.Command([]string{"loadfile", track.Uri})
}

func (p *Player) PlayNextTrack() error {
	pl := p.CurrentPlaylist()
    if pl == nil || len(pl.Tracks) == 0 {
        return nil
    }

    if p.CurrentIndex+1 >= len(pl.Tracks) {
        // loop back to start of active playlist
        p.CurrentIndex = 0
    } else {
        p.CurrentIndex++
    }

    track := pl.Tracks[p.CurrentIndex]
    p.ReplaceInProgress = true
    return p.Instance.Command([]string{"loadfile", track.Uri})
}

func (p *Player) PlayPreviousTrack() error {
    pl := p.CurrentPlaylist()
    if pl == nil || len(pl.Tracks) == 0 {
        return nil
    }

    if p.CurrentIndex-1 < 0 {
        // loop back to end of playlist
        p.CurrentIndex = len(pl.Tracks) - 1
    } else {
        p.CurrentIndex--
    }

    track := pl.Tracks[p.CurrentIndex]
    p.ReplaceInProgress = true
    return p.Instance.Command([]string{"loadfile", track.Uri})
}

func (p *Player) Play(trackIndex int) error {
    pl := p.CurrentPlaylist()
    if pl == nil || len(pl.Tracks) == 0 {
        return nil
    }
    if trackIndex < 0 || trackIndex >= len(pl.Tracks) {
        return nil
    }

    p.CurrentIndex = trackIndex
    track := pl.Tracks[trackIndex]
    p.ReplaceInProgress = true

    // if paused, resume
    if ip, e := p.IsPaused(); ip && e == nil {
        p.Pause()
    }

    return p.Instance.Command([]string{"loadfile", track.Uri})
}

func (p *Player) Stop() error {
	return p.Instance.Command([]string{"stop"})
}

func (p *Player) IsSongLoaded() (bool, error) {
	idle, err := p.Instance.GetProperty("idle-active", mpv.FORMAT_FLAG)
	return !idle.(bool), err
}

func (p *Player) IsPaused() (bool, error) {
	pause, err := p.Instance.GetProperty("pause", mpv.FORMAT_FLAG)
	return pause.(bool), err
}

// Pause toggles playing music
// If a song is playing, it is paused. If a song is paused, playing resumes. The
// state after the toggle is returned, or an error.
func (p *Player) Pause() (int, error) {
	loaded, err := p.IsSongLoaded()
	if err != nil {
		return PlayerError, err
	}
	pause, err := p.IsPaused()
	if err != nil {
		return PlayerError, err
	}

	if loaded {
		err := p.Instance.Command([]string{"cycle", "pause"})
		if err != nil {
			return PlayerError, err
		}
		if pause {
			return PlayerPlaying, nil
		}
		return PlayerPaused, nil
	} else {
		if len(p.Queue) != 0 {
			err := p.Instance.Command([]string{"loadfile", p.Queue[0].Uri})
			return PlayerPlaying, err
		} else {
			return PlayerStopped, nil
		}
	}
}

func (p *Player) AdjustVolume(increment int64) error {
	volume, err := p.Instance.GetProperty("volume", mpv.FORMAT_INT64)
	if err != nil {
		return err
	}

	if volume == nil {
		return nil
	}

	nevVolume := volume.(int64) + increment

	if nevVolume > 100 {
		nevVolume = 100
	} else if nevVolume < 0 {
		nevVolume = 0
	}

	return p.Instance.SetProperty("volume", mpv.FORMAT_INT64, nevVolume)
}

func (p *Player) Volume() (int64, error) {
	volume, err := p.Instance.GetProperty("volume", mpv.FORMAT_INT64)
	if err != nil {
		return -1, err
	}
	return volume.(int64), nil
}

func (p *Player) Seek(increment int) error {
	return p.Instance.Command([]string{"seek", strconv.Itoa(increment)})
}
