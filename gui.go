package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/spf13/viper"
	"github.com/wildeyedskies/go-mpv/mpv"
)

// struct contains all the updatable elements of the Ui
type Ui struct {
	app               *tview.Application
	pages             *tview.Pages
	entityList        *tview.List
	queueList         *tview.List
	playlistList      *tview.List
	addToPlaylistList *tview.List
	selectedPlaylist  *tview.List
	newPlaylistInput  *tview.InputField
	startStopStatus   *tview.TextView
	currentPage       *tview.TextView
	playerStatus      *tview.TextView
	logList           *tview.List
	searchField       *tview.InputField
	currentDirectory  *SubsonicDirectory
	artistList        *tview.List
	artistIdList      []string
	starIdList        map[string]struct{}
	playlists         []SubsonicPlaylist
	connection        *SubsonicConnection
	player            *Player
	scrobbleTimer     *time.Timer
}

func (ui *Ui) handleEntitySelected(directoryId string) {
	response, err := ui.connection.GetMusicDirectory(directoryId)
	sort.Sort(response.Directory.Entities)
	if err != nil {
		ui.connection.Logger.Printf("handleEntitySelected: GetMusicDirectory %s -- %s", directoryId, err.Error())
	}

	ui.currentDirectory = &response.Directory
	ui.entityList.Clear()
	if response.Directory.Parent != "" {
		ui.entityList.AddItem(tview.Escape("[..]"), "", 0,
			ui.makeEntityHandler(response.Directory.Parent))
	}

	for _, entity := range response.Directory.Entities {
		var title string
		var id = entity.Id
		var handler func()
		if entity.IsDirectory {
			title = tview.Escape("[" + entity.Title + "]")
			handler = ui.makeEntityHandler(entity.Id)
		} else {
			title = entityListTextFormat(entity, ui.starIdList )
			handler = makeSongHandler(id, ui.player, ui.queueList, ui.starIdList)
		}

		ui.entityList.AddItem(title, "", 0, handler)
	}
}

func (ui *Ui) handlePlaylistSelected(playlist SubsonicPlaylist) {
	ui.selectedPlaylist.Clear()

	for _, entity := range playlist.Entries {
		var title string
		var handler func()

		var id = entity.Id

		title = entity.getSongTitle()
		handler = makeSongHandler(id, ui.player, ui.queueList, ui.starIdList)

		ui.selectedPlaylist.AddItem(title, "", 0, handler)
	}
}

func (ui *Ui) handleDeleteFromQueue() {
	currentIndex := ui.queueList.GetCurrentItem()
	pl := ui.player.CurrentPlaylist()
	if pl == nil || currentIndex < 0 || currentIndex >= len(pl.Tracks) {
		return
	}

	pl.Tracks = append(pl.Tracks[:currentIndex], pl.Tracks[currentIndex+1:]...)
	if currentIndex < ui.player.CurrentIndex {
		ui.player.CurrentIndex--
	} else if currentIndex == ui.player.CurrentIndex {
		ui.player.PlayNextTrack()
	}
	updateQueueList(ui.player, ui.queueList, ui.starIdList)
}

func (ui *Ui) handleAddRandomSongs() {
	ui.addRandomSongsToQueue()
	updateQueueList(ui.player, ui.queueList, ui.starIdList)
}

func (ui *Ui) handleToggleStar() {
	// Get the active playlist and current track index
	pl := ui.player.CurrentPlaylist()
	if pl == nil || len(pl.Tracks) == 0 {
		return
	}

	currentIndex := ui.player.CurrentIndex
	if currentIndex < 0 || currentIndex >= len(pl.Tracks) {
		return
	}

	track := pl.Tracks[currentIndex]

	// Toggle star
	_, remove := ui.starIdList[track.Id]
	ui.connection.ToggleStar(track.Id, ui.starIdList) // updates server and map

	if remove {
		delete(ui.starIdList, track.Id)
	} else {
		ui.starIdList[track.Id] = struct{}{}
	}

	// Update queue list UI
	text := queueListTextFormat(track, ui.starIdList)
	updateQueueListItem(ui.queueList, currentIndex, text)

	// Update entity list if visible
	if ui.currentDirectory != nil {
		ui.handleEntitySelected(ui.currentDirectory.Id)
	}
}

func (ui *Ui) handleAddEntityToQueue() {
	currentIndex := ui.entityList.GetCurrentItem()
	if ui.currentDirectory.Parent != "" {
		currentIndex-- // skip the [..] item
	}
	if currentIndex < 0 || currentIndex >= len(ui.currentDirectory.Entities) {
		return
	}
	entity := ui.currentDirectory.Entities[currentIndex]
	if entity.IsDirectory {
		ui.addDirectoryToQueue(&entity)
	} else {
		ui.addSongToActivePlaylist(&entity)
	}
	updateQueueList(ui.player, ui.queueList, ui.starIdList)
}

func (ui *Ui) handleToggleEntityStar() {
	currentIndex := ui.entityList.GetCurrentItem()

	var entity = ui.currentDirectory.Entities[currentIndex-1]

	// If the song is already in the star list, remove it
	_, remove := ui.starIdList[entity.Id]

	ui.connection.ToggleStar(entity.Id, ui.starIdList)

	if (remove) {
		delete(ui.starIdList, entity.Id)
	} else {
		ui.starIdList[entity.Id] = struct{}{}
	}

	var text = entityListTextFormat(entity, ui.starIdList )
	updateEntityListItem(ui.entityList, currentIndex, text)
	updateQueueList(ui.player, ui.queueList, ui.starIdList)
}

func entityListTextFormat(queueItem SubsonicEntity, starredItems map[string]struct{} ) string {
	var star = ""
	_, hasStar := starredItems[queueItem.Id]
	if hasStar {
		star = " [red]♥"
	}
	return queueItem.Title + star
}

// Just update the text of a specific row
func updateEntityListItem(entityList *tview.List, id int, text string) {
	entityList.SetItemText(id, text, "")
}

func (ui *Ui) handleAddPlaylistSongToQueue() {
	playlistIndex := ui.playlistList.GetCurrentItem()
	entityIndex := ui.selectedPlaylist.GetCurrentItem()
	if playlistIndex < 0 || playlistIndex >= len(ui.playlists) || entityIndex < 0 {
		return
	}
	entity := ui.playlists[playlistIndex].Entries[entityIndex]
	ui.addSongToActivePlaylist(&entity)
	updateQueueList(ui.player, ui.queueList, ui.starIdList)
}

func (ui *Ui) handleAddPlaylistToQueue() {
	currentIndex := ui.playlistList.GetCurrentItem()
	if currentIndex < 0 || currentIndex >= len(ui.playlists) {
		return
	}
	for _, entity := range ui.playlists[currentIndex].Entries {
		ui.addSongToActivePlaylist(&entity)
	}
	updateQueueList(ui.player, ui.queueList, ui.starIdList)
}

func (ui *Ui) handleAddSongToPlaylist(playlist *SubsonicPlaylist) {
	currentIndex := ui.entityList.GetCurrentItem()

	// if we have a parent directory subtract 1 to account for the [..]
	// which would be index 0 in that case with index 1 being the first entity
	if ui.currentDirectory.Parent != "" {
		currentIndex--
	}

	if currentIndex == -1 || len(ui.currentDirectory.Entities) < currentIndex {
		return
	}

	entity := ui.currentDirectory.Entities[currentIndex]

	if !entity.IsDirectory {
		ui.connection.AddSongToPlaylist(string(playlist.Id), entity.Id)
	}
	// update the playlists
	response, err := ui.connection.GetPlaylists()
	if err != nil {
		ui.connection.Logger.Printf("handleAddSongToPlaylist: GetPlaylists -- %s", err.Error())
	}
	ui.playlists = response.Playlists.Playlists

	ui.playlistList.Clear()
	ui.addToPlaylistList.Clear()

	for _, playlist := range ui.playlists {
		ui.playlistList.AddItem(playlist.Name, "", 0, nil)
		ui.addToPlaylistList.AddItem(playlist.Name, "", 0, nil)
	}

	if currentIndex+1 < ui.entityList.GetItemCount() {
		ui.entityList.SetCurrentItem(currentIndex + 1)
	}
}

func (ui *Ui) addRandomSongsToQueue() {
	response, err := ui.connection.GetRandomSongs()
	if (err != nil) {
		ui.connection.Logger.Printf("addRandomSongsToQueue", err.Error())
	}
	for _, e := range response.RandomSongs.Song {
		ui.addSongToQueue(&e)
	}
}

func (ui *Ui) addStarredToList() {
	response, err := ui.connection.GetStarred()
	if (err != nil) {
		ui.connection.Logger.Printf("addStarredToList", err.Error())
	}
	for _, e := range response.Starred.Song {
		// We're storing empty struct as values as we only want the indexes
		// It's faster having direct index access instead of looping through array values
		ui.starIdList[e.Id] = struct{}{}
	}
}

func (ui *Ui) addDirectoryToQueue(entity *SubsonicEntity) {
	response, err := ui.connection.GetMusicDirectory(entity.Id)
	if err != nil {
		ui.connection.Logger.Printf("addDirectoryToQueue: GetMusicDirectory %s -- %s", entity.Id, err.Error())
		return
	}

	sort.Sort(response.Directory.Entities)
	for _, e := range response.Directory.Entities {
		if e.IsDirectory {
			ui.addDirectoryToQueue(&e)
		} else {
			ui.addSongToQueue(&e)
		}
	}
}

func (ui *Ui) search() {
	name, _ := ui.pages.GetFrontPage()
	if name != "browser" {
		return
	}
	ui.searchField.SetText("")
	ui.app.SetFocus(ui.searchField)
}

func (ui *Ui) searchNext() {
	str := ui.searchField.GetText()
	idxs := ui.artistList.FindItems(str, "", false, true)
	if len(idxs) == 0 {
		return
	}
	curIdx := ui.artistList.GetCurrentItem()
	for _, nidx := range idxs {
		if nidx > curIdx {
			ui.artistList.SetCurrentItem(nidx)
			return
		}
	}
	ui.artistList.SetCurrentItem(idxs[0])
}

func (ui *Ui) searchPrev() {
	str := ui.searchField.GetText()
	idxs := ui.artistList.FindItems(str, "", false, true)
	if len(idxs) == 0 {
		return
	}
	curIdx := ui.artistList.GetCurrentItem()
	for nidx := len(idxs) - 1; nidx >= 0; nidx-- {
		if idxs[nidx] < curIdx {
			ui.artistList.SetCurrentItem(idxs[nidx])
			return
		}
	}
	ui.artistList.SetCurrentItem(idxs[len(idxs)-1])
}

func (ui *Ui) addSongToQueue(entity *SubsonicEntity) {
	uri := ui.connection.GetPlayUrl(entity)

	// Determine the artist string
	artist := stringOr(entity.Artist, "")
	if ui.currentDirectory != nil {
		artist = stringOr(entity.Artist, ui.currentDirectory.Name)
	}

	// Create the track object
	track := QueueItem{
		Id:       entity.Id,
		Uri:      uri,
		Title:    entity.getSongTitle(),
		Artist:   artist,
		Duration: entity.Duration,
	}

	// Ensure a playlist exists
	if ui.player.CurrentPlaylist() == nil {
		ui.player.ActivePlaylist = &Playlist{Tracks: []QueueItem{}}
	}

	// Add track to the active playlist
	ui.player.CurrentPlaylist().Tracks = append(ui.player.CurrentPlaylist().Tracks, track)

	// Update the UI
	updateQueueList(ui.player, ui.queueList, ui.starIdList)
}

func (ui *Ui) newPlaylist(name string) {
	response, err := ui.connection.CreatePlaylist(name)
	if err != nil {
		ui.connection.Logger.Printf("newPlaylist: CreatePlaylist %s -- %s", name, err.Error())
		return
	}

	ui.playlists = append(ui.playlists, response.Playlist)

	ui.playlistList.AddItem(response.Playlist.Name, "", 0, nil)
	ui.addToPlaylistList.AddItem(response.Playlist.Name, "", 0, nil)
}

func (ui *Ui) deletePlaylist(index int) {
	if index == -1 || len(ui.playlists) < index {
		return
	}

	playlist := ui.playlists[index]

	if index == 0 {
		ui.playlistList.SetCurrentItem(1)
	}

	// Removes item with specified index
	ui.playlists = append(ui.playlists[:index], ui.playlists[index+1:]...)

	ui.playlistList.RemoveItem(index)
	ui.addToPlaylistList.RemoveItem(index)
	ui.connection.DeletePlaylist(string(playlist.Id))
}

func makeSongHandler(trackID string, player *Player, queueList *tview.List, starIdList map[string]struct{}) func() {
	return func() {
		pl := player.CurrentPlaylist()
		if pl == nil {
			return
		}

		trackIndex := -1
		for i, t := range pl.Tracks {
			if t.Id == trackID {
				trackIndex = i
				break
			}
		}
		if trackIndex == -1 {
			return
		}

		if err := player.Play(trackIndex); err != nil {
			fmt.Printf("Error playing track: %v\n", err)
			return
		}

		updateQueueList(player, queueList, starIdList)
	}
}

func (ui *Ui) makeEntityHandler(directoryId string) func() {
	return func() {
		ui.handleEntitySelected(directoryId)
	}
}

func createUi(indexes *[]SubsonicIndex, playlists *[]SubsonicPlaylist, connection *SubsonicConnection, player *Player) *Ui {
	app := tview.NewApplication()
	pages := tview.NewPages()

	entityList := tview.NewList().ShowSecondaryText(false).SetSelectedFocusOnly(true)
	queueList := tview.NewList().ShowSecondaryText(false)
	playlistList := tview.NewList().ShowSecondaryText(false).SetSelectedFocusOnly(true)
	addToPlaylistList := tview.NewList().ShowSecondaryText(false)
	selectedPlaylist := tview.NewList().ShowSecondaryText(false)
	startStopStatus := tview.NewTextView().SetText("[::b]stmp: [red]stopped").SetTextAlign(tview.AlignLeft).SetDynamicColors(true)
	currentPage := tview.NewTextView().SetText("Browser").SetTextAlign(tview.AlignCenter).SetDynamicColors(true)
	playerStatus := tview.NewTextView().SetText("[::b][100%][0:00/0:00]").SetTextAlign(tview.AlignRight).SetDynamicColors(true)
	newPlaylistInput := tview.NewInputField().SetLabel("Playlist name:").SetFieldWidth(50)
	logs := tview.NewList().ShowSecondaryText(false)
	var currentDirectory *SubsonicDirectory
	var artistIdList []string
	var starIdList = map[string]struct{}{}
	scrobbleTimer := time.NewTimer(0)
	if !scrobbleTimer.Stop() {
		<-scrobbleTimer.C
	}

	ui := &Ui{
		app:               app,
		pages:             pages,
		entityList:        entityList,
		queueList:         queueList,
		playlistList:      playlistList,
		addToPlaylistList: addToPlaylistList,
		selectedPlaylist:  selectedPlaylist,
		newPlaylistInput:  newPlaylistInput,
		startStopStatus:   startStopStatus,
		currentPage:       currentPage,
		playerStatus:      playerStatus,
		logList:           logs,
		currentDirectory:  currentDirectory,
		artistIdList:      artistIdList,
		starIdList:        starIdList,
		playlists:         *playlists,
		connection:        connection,
		player:            player,
		scrobbleTimer:     scrobbleTimer,
	}

	ui.addStarredToList()

	// Handle MPV events in background
	go ui.handleMpvEvents()

	return ui
}


func (ui *Ui) createBrowserPage(titleFlex *tview.Flex, indexes *[]SubsonicIndex) (*tview.Flex, tview.Primitive) {
	// artist list, used to map the index of
	ui.artistList = tview.NewList().ShowSecondaryText(false)
	for _, index := range *indexes {
		for _, artist := range index.Artists {
			ui.artistList.AddItem(artist.Name, "", 0, nil)
			ui.artistIdList = append(ui.artistIdList, artist.Id)
		}
	}

	ui.searchField = tview.NewInputField().
		SetLabel("Search:").
		SetChangedFunc(func(s string) {
			idxs := ui.artistList.FindItems(s, "", false, true)
			if len(idxs) == 0 {
				return
			}
			ui.artistList.SetCurrentItem(idxs[0])
		}).SetDoneFunc(func(key tcell.Key) {
		ui.app.SetFocus(ui.artistList)
	})

	artistFlex := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(ui.artistList, 0, 1, true).
		AddItem(ui.entityList, 0, 1, false)

	browserFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(titleFlex, 1, 0, false).
		AddItem(artistFlex, 0, 1, true).
		AddItem(ui.searchField, 1, 0, false)

	// going right from the artist list should focus the album/song list
	ui.artistList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch keyName(event) {
		case keybind("right"):
			ui.app.SetFocus(ui.entityList)
			return nil
		case keybind("search"):
			ui.search()
			return nil
		case keybind("searchNext"):
			ui.searchNext()
			return nil
		case keybind("searchPrev"):
			ui.searchPrev()
			return nil
		case keybind("refresh"):
			goBackTo := ui.artistList.GetCurrentItem()
			// REFRESH artists
			indexResponse, err := ui.connection.GetIndexes()
			if err != nil {
				ui.connection.Logger.Printf("Error fetching indexes from server: %s\n", err)
				return event
			}
			ui.artistList.Clear()
			ui.connection.directoryCache = make(map[string]SubsonicResponse)
			for _, index := range indexResponse.Indexes.Index {
				for _, artist := range index.Artists {
					ui.artistList.AddItem(artist.Name, "", 0, nil)
					ui.artistIdList = append(ui.artistIdList, artist.Id)
				}
			}
			// Try to put the user to about where they were
			if goBackTo < ui.artistList.GetItemCount() {
				ui.artistList.SetCurrentItem(goBackTo)
			}
		}
		return event
	})

	ui.artistList.SetChangedFunc(func(index int, _ string, _ string, _ rune) {
		ui.handleEntitySelected(ui.artistIdList[index])
	})

	for _, playlist := range ui.playlists {
		ui.addToPlaylistList.AddItem(playlist.Name, "", 0, nil)
	}
	ui.addToPlaylistList.SetBorder(true).
		SetTitle("Add to Playlist")

	addToPlaylistFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(ui.addToPlaylistList, 0, 1, true)

	addToPlaylistModal := makeModal(addToPlaylistFlex, 60, 20)

	ui.addToPlaylistList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			ui.pages.HidePage("addToPlaylist")
			ui.pages.SwitchToPage("browser")
			ui.app.SetFocus(ui.entityList)
		} else if event.Key() == tcell.KeyEnter {
			playlist := ui.playlists[ui.addToPlaylistList.GetCurrentItem()]
			ui.handleAddSongToPlaylist(&playlist)

			ui.pages.HidePage("addToPlaylist")
			ui.pages.SwitchToPage("browser")
			ui.app.SetFocus(ui.entityList)
		}
		return event
	})

	ui.entityList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if keyName(event) == keybind("left") {
			ui.app.SetFocus(ui.artistList)
			return nil
		}
		if keyName(event) == keybind("add") {
			ui.handleAddEntityToQueue()
			return nil
		}
		if keyName(event) == keybind("star") {
			ui.handleToggleEntityStar()
			return nil
		}
		// only makes sense to add to a playlist if there are playlists
		if keyName(event) == keybind("addToPlaylist") && ui.playlistList.GetItemCount() > 0 {
			ui.pages.ShowPage("addToPlaylist")
			ui.app.SetFocus(ui.addToPlaylistList)
			return nil
		}
		// REFRESH only the artist
		if keyName(event) == keybind("refresh") {
			artistIdx := ui.artistList.GetCurrentItem()
			entity := ui.artistIdList[artistIdx]
			//ui.logger.Printf("refreshing artist idx %d, entity %s (%s)", artistIdx, entity, ui.connection.directoryCache[entity].Directory.Name)
			delete(ui.connection.directoryCache, entity)
			ui.handleEntitySelected(ui.artistIdList[artistIdx])
			return nil
		}
		return event
	})

	return browserFlex, addToPlaylistModal
}

func (ui *Ui) addSongToActivePlaylist(entity *SubsonicEntity) {
	pl := ui.player.CurrentPlaylist()
	if pl == nil {
		return
	}

	track := QueueItem{
		Id:       entity.Id,
		Uri:      ui.connection.GetPlayUrl(entity),
		Title:    entity.getSongTitle(),
		Artist:   stringOr(entity.Artist, ui.currentDirectory.Name),
		Duration: entity.Duration,
	}
		pl.Tracks = append(pl.Tracks, track)
}
	

func (ui *Ui) createQueuePage(titleFlex *tview.Flex) *tview.Flex {
	queueFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(titleFlex, 1, 0, false).
		AddItem(ui.queueList, 0, 1, true)
	ui.queueList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyDelete || keyName(event) == keybind("removeFromQueue") {
			ui.handleDeleteFromQueue()
			return nil
		} else if keyName(event) == keybind("star") {
			ui.handleToggleStar()
			return nil
		}

		return event
	})

	return queueFlex
}

func (ui *Ui) createPlaylistPage(titleFlex *tview.Flex) (*tview.Flex, tview.Primitive) {
	//add the playlists
	for _, playlist := range ui.playlists {
		ui.playlistList.AddItem(playlist.Name, "", 0, nil)
	}

	ui.playlistList.SetChangedFunc(func(index int, _ string, _ string, _ rune) {
		ui.handlePlaylistSelected(ui.playlists[index])
	})

	playlistColFlex := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(ui.playlistList, 0, 1, true).
		AddItem(ui.selectedPlaylist, 0, 1, false)

	playlistFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(titleFlex, 1, 0, false).
		AddItem(playlistColFlex, 0, 1, true)

	ui.newPlaylistInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEnter {
			ui.newPlaylist(ui.newPlaylistInput.GetText())
			playlistFlex.Clear()
			playlistFlex.AddItem(titleFlex, 1, 0, false)
			playlistFlex.AddItem(playlistColFlex, 0, 1, true)
			ui.app.SetFocus(ui.playlistList)
			return nil
		}
		if event.Key() == tcell.KeyEscape {
			playlistFlex.Clear()
			playlistFlex.AddItem(titleFlex, 1, 0, false)
			playlistFlex.AddItem(playlistColFlex, 0, 1, true)
			ui.app.SetFocus(ui.playlistList)
			return nil
		}
		return event
	})

	ui.playlistList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if keyName(event) == keybind("right") {
			ui.app.SetFocus(ui.selectedPlaylist)
			return nil
		}
		if keyName(event) == keybind("add") {
			ui.handleAddPlaylistToQueue()
			return nil
		}
		if keyName(event) == keybind("newPlaylist") {
			playlistFlex.AddItem(ui.newPlaylistInput, 0, 1, true)
			ui.app.SetFocus(ui.newPlaylistInput)
		}
		if keyName(event) == keybind("deletePlaylist") {
			ui.pages.ShowPage("deletePlaylist")
		}
		return event
	})

	ui.selectedPlaylist.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if keyName(event) == keybind("left") {
			ui.app.SetFocus(ui.playlistList)
			return nil
		}
		if keyName(event) == keybind("add") {
			ui.handleAddPlaylistSongToQueue()
			return nil
		}
		return event
	})

	deletePlaylistList := tview.NewList().
		ShowSecondaryText(false)

	deletePlaylistList.AddItem("Confirm", "", 0, nil)

	deletePlaylistList.SetBorder(true).
		SetTitle("Confirm deletion")

	deletePlaylistFlex := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(deletePlaylistList, 0, 1, true)

	deletePlaylistList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEnter {
			ui.deletePlaylist(ui.playlistList.GetCurrentItem())
			ui.app.SetFocus(ui.playlistList)
			ui.pages.HidePage("deletePlaylist")
			return nil
		}
		if event.Key() == tcell.KeyEscape {
			ui.app.SetFocus(ui.playlistList)
			ui.pages.HidePage("deletePlaylist")
			return nil
		}
		return event
	})

	deletePlaylistModal := makeModal(deletePlaylistFlex, 20, 3)

	return playlistFlex, deletePlaylistModal
}

func InitGui(indexes *[]SubsonicIndex, playlists *[]SubsonicPlaylist, connection *SubsonicConnection, player *Player) *Ui {
	ui := createUi(indexes, playlists, connection, player)

	// Title bar
	titleFlex := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(ui.startStopStatus, 0, 1, false).
		AddItem(ui.currentPage, 0, 1, false).
		AddItem(ui.playerStatus, 0, 1, false)

	// Browser Page
	ui.artistList = tview.NewList().ShowSecondaryText(false)
	for _, index := range *indexes {
		for _, artist := range index.Artists {
			ui.artistList.AddItem(artist.Name, "", 0, nil)
			ui.artistIdList = append(ui.artistIdList, artist.Id)
		}
	}

	ui.searchField = tview.NewInputField().SetLabel("Search:")
	ui.searchField.SetChangedFunc(func(s string) {
		idxs := ui.artistList.FindItems(s, "", false, true)
		if len(idxs) > 0 {
			ui.artistList.SetCurrentItem(idxs[0])
		}
	}).SetDoneFunc(func(key tcell.Key) {
		ui.app.SetFocus(ui.artistList)
	})

	artistFlex := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(ui.artistList, 0, 1, true).
		AddItem(ui.entityList, 0, 1, false)

	browserFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(titleFlex, 1, 0, false).
		AddItem(artistFlex, 0, 1, true).
		AddItem(ui.searchField, 1, 0, false)

	// Queue Page
	queueFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(titleFlex, 1, 0, false).
		AddItem(ui.queueList, 0, 1, true)

	// Playlist Page
	playlistColFlex := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(ui.playlistList, 0, 1, true).
		AddItem(ui.selectedPlaylist, 0, 1, false)

	playlistFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(titleFlex, 1, 0, false).
		AddItem(playlistColFlex, 0, 1, true)

	// Log Page
	logListFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(titleFlex, 1, 0, false).
		AddItem(ui.logList, 0, 1, true)

	ui.pages.AddPage("browser", browserFlex, true, true).
		AddPage("queue", queueFlex, true, false).
		AddPage("playlists", playlistFlex, true, false).
		AddPage("log", logListFlex, true, false)

	ui.pages.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		key := keyName(event)

		switch key {
		case keybind("pageBrowser"):
			ui.pages.SwitchToPage("browser")
			ui.currentPage.SetText("Browser")
		case keybind("pageQueue"):
			ui.pages.SwitchToPage("queue")
			ui.currentPage.SetText("Queue")
		case keybind("pagePlaylists"):
			ui.pages.SwitchToPage("playlists")
			ui.currentPage.SetText("Playlists")
		case keybind("pageLog"):
			ui.pages.SwitchToPage("log")
			ui.currentPage.SetText("Log")
		case keybind("quit"):
			ui.player.EventChannel <- nil
			ui.player.Instance.TerminateDestroy()
			ui.app.Stop()
		case keybind("playPause"):
			status, err := ui.player.Pause()
			if err != nil {
				ui.startStopStatus.SetText("[::b]stmp: [red]error")
			} else if status == PlayerPlaying {
				pl := ui.player.CurrentPlaylist()
				if pl != nil {
					ui.startStopStatus.SetText("[::b]stmp: [green]playing " + pl.Tracks[ui.player.CurrentIndex].Title)
				}
			} else if status == PlayerPaused {
				ui.startStopStatus.SetText("[::b]stmp: [yellow]paused")
			} else {
				ui.startStopStatus.SetText("[::b]stmp: [red]stopped")
			}
		case keybind("nextplaylist"):
			ui.player.PlayNextTrack()
			updateQueueList(ui.player, ui.queueList, ui.starIdList)
		}
		return event
	})

	if err := ui.app.SetRoot(ui.pages, true).SetFocus(ui.pages).EnableMouse(true).Run(); err != nil {
		panic(err)
	}

	return ui
}



func queueListTextFormat(track QueueItem, starredItems map[string]struct{}) string {
	min, sec := iSecondsToMinAndSec(track.Duration)
	star := ""
	if _, ok := starredItems[track.Id]; ok {
		star = " [red]♥"
	}
	return fmt.Sprintf("%s - %s - %02d:%02d%s", track.Title, track.Artist, min, sec, star)
}

// Just update the text of a specific row
func updateQueueListItem(queueList *tview.List, id int, text string) {
	queueList.SetItemText(id, text, "")
}

func updateQueueList(player *Player, queueList *tview.List, starredItems map[string]struct{}) {
	queueList.Clear()
	pl := player.CurrentPlaylist()
	if pl == nil {
		return
	}
	for i, track := range pl.Tracks {
		text := queueListTextFormat(track, starredItems)
		if i == player.CurrentIndex {
			text = "[green]▶ " + text
		}
		queueList.AddItem(text, "", 0, nil)
	}
}

func (ui *Ui) handleMpvEvents() {
	ui.player.Instance.ObserveProperty(0, "time-pos", mpv.FORMAT_DOUBLE)
	ui.player.Instance.ObserveProperty(0, "duration", mpv.FORMAT_DOUBLE)
	ui.player.Instance.ObserveProperty(0, "volume", mpv.FORMAT_INT64)
	for {
		e := <-ui.player.EventChannel
		if e == nil {
			break
		}

		switch e.Event_Id {
		case mpv.EVENT_END_FILE:
			if !ui.player.ReplaceInProgress {
				ui.player.PlayNextTrack() // loops if at end
				updateQueueList(ui.player, ui.queueList, ui.starIdList)
			}
		case mpv.EVENT_START_FILE:
			ui.player.ReplaceInProgress = false
			updateQueueList(ui.player, ui.queueList, ui.starIdList)
			pl := ui.player.CurrentPlaylist()
			if pl != nil && len(pl.Tracks) > 0 {
				currentTrack := pl.Tracks[ui.player.CurrentIndex]
				ui.startStopStatus.SetText("[::b]stmp: [green]playing " + currentTrack.Title)
			}
		}
	}
}

//func stringOr(firstChoice string, secondChoice string) string {
//	if firstChoice != "" {
	//	return firstChoice
	//}
//	return secondChoice
//}

//func iSecondsToMinAndSec(seconds int) (int, int) {
	//return seconds / 60, seconds % 60
//}


func makeModal(p tview.Primitive, width, height int) tview.Primitive {
	return tview.NewGrid().
		SetColumns(0, width, 0).
		SetRows(0, height, 0).
		AddItem(p, 1, 1, 1, 1, 0, 0, true)
}

func formatPlayerStatus(volume int64, position float64, duration float64) string {
	if position < 0 {
		position = 0.0
	}

	if duration < 0 {
		duration = 0.0
	}

	positionMin, positionSec := secondsToMinAndSec(position)
	durationMin, durationSec := secondsToMinAndSec(duration)

	return fmt.Sprintf("[::b][%d%%][%02d:%02d/%02d:%02d]", volume,
		positionMin, positionSec, durationMin, durationSec)
}

func secondsToMinAndSec(seconds float64) (int, int) {
	minutes := math.Floor(seconds / 60)
	remainingSeconds := int(seconds) % 60
	return int(minutes), remainingSeconds
}

func iSecondsToMinAndSec(seconds int) (int, int) {
	minutes := seconds / 60
	remainingSeconds := seconds % 60
	return minutes, remainingSeconds
}

// if the first argument isn't empty, return it, otherwise return the second
func stringOr(firstChoice string, secondChoice string) string {
	if firstChoice != "" {
		return firstChoice
	}
	return secondChoice
}

// Return the title if present, otherwise fallback to the file path
func (e SubsonicEntity) getSongTitle() string {
	if e.Title != "" {
		return e.Title
	}

	// we get around the weird edge case where a path ends with a '/' by just
	// returning nothing in that instance, which shouldn't happen unless
	// subsonic is being weird
	if e.Path == "" || strings.HasSuffix(e.Path, "/") {
		return ""
	}

	lastSlash := strings.LastIndex(e.Path, "/")

	if lastSlash == -1 {
		return e.Path
	}

	return e.Path[lastSlash+1 : len(e.Path)]
}

func keyName(event *tcell.EventKey) string {
	if (event.Key() == tcell.KeyRune) {
		return string(event.Rune())
	} else {
		return event.Name()
	}
}

func keybind(path string) string {
	return viper.GetString("keys." + path)
}

