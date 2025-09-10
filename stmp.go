package main

import (
	"flag"
	"fmt"
	"os"
 	"log"
    "time"

    "periph.io/x/conn/v3/gpio"
    "periph.io/x/host/v3"
    "periph.io/x/host/v3/rpi"
	"github.com/spf13/viper"
)

func readConfig() {
	required_properties := []string{"auth.username", "auth.password", "server.host"}

	viper.SetConfigName("stmp")
	viper.SetConfigType("toml")
	viper.AddConfigPath("$HOME/.config/stmp")
	viper.AddConfigPath(".")

	// Keybinds
	viper.SetDefault("keys.search", "/")
	viper.SetDefault("keys.searchNext", "n")
	viper.SetDefault("keys.searchPrev", "N")
	viper.SetDefault("keys.refresh", "r")
	viper.SetDefault("keys.add", "a")
	viper.SetDefault("keys.star", "y")
	viper.SetDefault("keys.newPlaylist", "a")
	viper.SetDefault("keys.addToPlaylist", "A")
	viper.SetDefault("keys.deletePlaylist", "d")
	viper.SetDefault("keys.removeFromQueue", "d")
	viper.SetDefault("keys.pageBrowser", "1")
	viper.SetDefault("keys.pageQueue", "2")
	viper.SetDefault("keys.pagePlaylists", "3")
	viper.SetDefault("keys.pageLog", "7")
	viper.SetDefault("keys.playnexttrack", "8")
	viper.SetDefault("keys.quit", "q")
	viper.SetDefault("keys.addRandomSongs", "s")
	viper.SetDefault("keys.clearQueue", "D")
	viper.SetDefault("keys.playPause", "p")
	viper.SetDefault("keys.volumeDown", "-")
	viper.SetDefault("keys.volumeUp", "=")
	viper.SetDefault("keys.seekForward", ".")
	viper.SetDefault("keys.seekBack", ",")
	viper.SetDefault("keys.up", "Up")
	viper.SetDefault("keys.down", "Down")
	viper.SetDefault("keys.left", "Left")
	viper.SetDefault("keys.right", "Right")

	err := viper.ReadInConfig()

	if err != nil {
		fmt.Printf("Config file error: %s \n", err)
		os.Exit(1)
	}

	for _, prop := range required_properties {
		if !viper.IsSet(prop) {
			fmt.Printf("Config property %s is required\n", prop)
		}
	}
}

type Logger struct {
	prints chan string
}

func (l Logger) Printf(s string, as ...interface{}) {
	l.prints <- fmt.Sprintf(s, as...)
}

func ListenForButton(player *Player) {
    if _, err := host.Init(); err != nil {
        log.Fatal(err)
    }

    pin := rpi.P1_37
    if err := pin.In(gpio.PullUp, gpio.BothEdges); err != nil {
        log.Fatal(err)
    }

    go func() {
        for {
            pin.WaitForEdge(-1)
            if pin.Read() == gpio.Low {
                // Call the method on the Player instance
                err := player.PlayNextTrack()
                if err != nil {
                    log.Printf("Error playing next track: %v", err)
                }
            }
            time.Sleep(200 * time.Millisecond) // debounce
        }
    }()
}

func main() {
	help := flag.Bool("help", false, "Print usage")
	enableMpris := flag.Bool("mpris", false, "Enable MPRIS2")
	flag.Parse()
	if *help {
		fmt.Printf("USAGE: %s <args>\n", os.Args[0])
		flag.Usage()
		os.Exit(0)
	}

	readConfig()

	logger := Logger{make(chan string, 100)}

	connection := &SubsonicConnection{
		Username:       viper.GetString("auth.username"),
		Password:       viper.GetString("auth.password"),
		Host:           viper.GetString("server.host"),
		PlaintextAuth:  viper.GetBool("auth.plaintext"),
		Scrobble:       viper.GetBool("server.scrobble"),
		Logger:         logger,
		directoryCache: make(map[string]SubsonicResponse),
	}

	indexResponse, err := connection.GetIndexes()
	if err != nil {
		fmt.Printf("Error fetching indexes from server: %s\n", err)
		os.Exit(1)
	}
	playlistResponse, err := connection.GetPlaylists()
	if err != nil {
		fmt.Printf("Error fetching playlists from server: %s\n", err)
		os.Exit(1)
	}

	player, err := InitPlayer()
	if err != nil {
		fmt.Println("Unable to initialize mpv. Is mpv installed?")
		os.Exit(1)
	}

	 ListenForButton(player)

	// ✅ Load the first playlist into the queue
if len(playlistResponse.Playlists.Playlists) > 0 {
    firstPlaylist := playlistResponse.Playlists.Playlists[0]

			// Fetch full playlist entries (GetPlaylists often returns only metadata)
		fullPlaylist, err := connection.GetPlaylist(firstPlaylist.Id)
		if err != nil {
			fmt.Printf("Error fetching playlist %s: %s\n", firstPlaylist.Name, err)
		} else {
			firstPlaylist.Entries = fullPlaylist.Entries

     for _, entry := range firstPlaylist.Entries {
        player.Queue = append(player.Queue, QueueItem{
            entry.Id,
            connection.GetPlayUrl(&entry),
            entry.getSongTitle(),
            entry.Artist,
            entry.Duration,
        })
    }

}

	if *enableMpris {
		mpris, err := RegisterPlayer(player, logger)
		if err != nil {
			fmt.Printf("Unable to register MPRIS with DBUS: %s\n", err)
			fmt.Println("Try running without MPRIS")
			os.Exit(1)
		}
		defer mpris.Close()
	}

	InitGui(&indexResponse.Indexes.Index, &playlistResponse.Playlists.Playlists, connection, player)
}
