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
			handler = makeSongHandler(id, ui.connection.GetPlayUrl(&entity),
				title, stringOr(entity.Artist, response.Directory.Name),
				entity.Duration, ui.player, ui.queueList, ui.starIdList)
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
		handler = makeSongHandler(id, ui.connection.GetPlayUrl(&entity), title, entity.Artist, entity.Duration, ui.player, ui.queueList, ui.starIdList)

		ui.selectedPlaylist.AddItem(title, "", 0, handler)
	}
}

func (ui *Ui) handleDeleteFromQueue() {
	currentIndex := ui.queueList.GetCurrentItem()
	queue := ui.player.Queue

	if currentIndex == -1 || len(ui.player.Queue) < currentIndex {
		return
	}

	// if the deleted item was the first one, and the player is loaded
	// remove the track. Removing the track auto starts the next one
	if currentIndex == 0 {
		if isSongLoaded, err := ui.player.IsSongLoaded(); err != nil {
			ui.connection.Logger.Printf("handleDeleteFromQueue: IsSongLoaded -- %s", err.Error())
			return
		} else if isSongLoaded {
			ui.player.Stop()
		}
		return
	}

	// remove the item from the queue
	if len(ui.player.Queue) > 1 {
		ui.player.Queue = append(queue[:currentIndex], queue[currentIndex+1:]...)
	} else {
		ui.player.Queue = make([]QueueItem, 0)
	}

	updateQueueList(ui.player, ui.queueList, ui.starIdList)
}

func (ui *Ui) handleAddRandomSongs() {
	ui.addRandomSongsToQueue()
	updateQueueList(ui.player, ui.queueList, ui.starIdList)
}

func (ui *Ui) handleToggleStar() {
	currentIndex := ui.queueList.GetCurrentItem()
	queue := ui.player.Queue

	if currentIndex == -1 || len(ui.player.Queue) < currentIndex {
		return
	}

	var entity = queue[currentIndex]

	// If the song is already in the star list, remove it
	_, remove := ui.starIdList[entity.Id]

	// resp, _ := ui.connection.ToggleStar(entity.Id, remove)
	ui.connection.ToggleStar(entity.Id, ui.starIdList)

	if (remove) {
		delete(ui.starIdList, entity.Id)
	} else {
		ui.starIdList[entity.Id] = struct{}{}
	}

	var text = queueListTextFormat(ui.player.Queue[currentIndex], ui.starIdList )
	updateQueueListItem(ui.queueList, currentIndex, text)
	// Update the entity list to reflect any changes
	ui.connection.Logger.Printf("entity test", ui.currentDirectory)
	if (ui.currentDirectory != nil) {
		ui.handleEntitySelected(ui.currentDirectory.Id) 
	}
}

func (ui *Ui) handleAddEntityToQueue() {
	currentIndex := ui.entityList.GetCurrentItem()
	if currentIndex+1 < ui.entityList.GetItemCount() {
		ui.entityList.SetCurrentItem(currentIndex + 1)
	}

	// if we have a parent directory subtract 1 to account for the [..]
	// which would be index 0 in that case with index 1 being the first entity
	if ui.currentDirectory.Parent != "" {
		currentIndex--
	}

	if currentIndex == -1 || len(ui.currentDirectory.Entities) <= currentIndex {
		return
	}

	entity := ui.currentDirectory.Entities[currentIndex]

	if entity.IsDirectory {
		ui.addDirectoryToQueue(&entity)
	} else {
		ui.addSongToQueue(&entity)
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
	if entityIndex+1 < ui.selectedPlaylist.GetItemCount() {
		ui.selectedPlaylist.SetCurrentItem(entityIndex + 1)
	}

	// TODO add some bounds checking here
	if playlistIndex == -1 || entityIndex == -1 {
		return
	}

	entity := ui.playlists[playlistIndex].Entries[entityIndex]
	ui.addSongToQueue(&entity)

	updateQueueList(ui.player, ui.queueList, ui.starIdList)
}

func (ui *Ui) handleAddPlaylistToQueue() {
	currentIndex := ui.playlistList.GetCurrentItem()
	if currentIndex+1 < ui.playlistList.GetItemCount() {
		ui.playlistList.SetCurrentItem(currentIndex + 1)
	}

	playlist := ui.playlists[currentIndex]

	for _, entity := range playlist.Entries {
		ui.addSongToQueue(&entity)
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

	var artist string
	if ui.currentDirectory == nil {
		artist = entity.Artist
	} else {
		stringOr(entity.Artist, ui.currentDirectory.Name)
	}

	var id = entity.Id


	queueItem := QueueItem{
		id,
		uri,
		entity.getSongTitle(),
		artist,
		entity.Duration,
	}
	ui.player.Queue = append(ui.player.Queue, queueItem)
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

func makeSongHandler(id string, uri string, title string, artist string, duration int, player *Player, queueList *tview.List, starIdList map[string]struct{}) func() {
	return func() {
		player.Play(id, uri, title, artist, duration)
		updateQueueList(player, queueList, starIdList)
	}
}

func (ui *Ui) makeEntityHandler(directoryId string) func() {
	return func() {
		ui.handleEntitySelected(directoryId)
	}
}

func createUi(_ *[]SubsonicIndex, playlists *[]SubsonicPlaylist, connection *SubsonicConnection, player *Player) *Ui {
	app := tview.NewApplication()
	pages := tview.NewPages()
	// list of entities
	entityList := tview.NewList().ShowSecondaryText(false).
		SetSelectedFocusOnly(true)
	// player queue
	queueList := tview.NewList().ShowSecondaryText(false)
	// list of playlists
	playlistList := tview.NewList().ShowSecondaryText(false).
		SetSelectedFocusOnly(true)
	// same as 'playlistList' except for the addToPlaylistModal
	// - we need a specific version of this because we need different keybinds
	addToPlaylistList := tview.NewList().ShowSecondaryText(false)
	// songs in the selected playlist
	selectedPlaylist := tview.NewList().ShowSecondaryText(false)
	// status text at the top
	startStopStatus := tview.NewTextView().SetText("[::b]stmp: [red]stopped").
		SetTextAlign(tview.AlignLeft).
		SetDynamicColors(true)
	currentPage := tview.NewTextView().SetText("Browser").
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true)
	playerStatus := tview.NewTextView().SetText("[::b][100%][0:00/0:00]").
		SetTextAlign(tview.AlignRight).
		SetDynamicColors(true)
	newPlaylistInput := tview.NewInputField().
		SetLabel("Playlist name:").
		SetFieldWidth(50)
	logs := tview.NewList().ShowSecondaryText(false)
	var currentDirectory *SubsonicDirectory
	var artistIdList []string
	// Stores the song IDs
	var starIdList = map[string]struct{}{}

	// create reused timer to scrobble after delay
	scrobbleTimer := time.NewTimer(0)
	if !scrobbleTimer.Stop() {
		<-scrobbleTimer.C
	}

	ui := Ui{
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

	go func() {
		for {
			select {
			case msg := <-connection.Logger.prints:
				ui.app.QueueUpdate(func() {
					ui.logList.AddItem(msg, "", 0, nil)
					// Make sure the log list doesn't grow infinitely
					for ui.logList.GetItemCount() > 200 {
						ui.logList.RemoveItem(0)
					}
				})

			case <-scrobbleTimer.C:
				// scrobble submission delay elapsed
				paused, err := ui.player.IsPaused()
				connection.Logger.Printf("scrobbler event: paused %v, err %v, qlen %d", paused, err, len(ui.player.Queue))
				isPlaying := err == nil && !paused
				if len(ui.player.Queue) > 0 && isPlaying {
					// it's still playing, submit it
					currentSong := ui.player.Queue[0]
					ui.connection.ScrobbleSubmission(currentSong.Id, true)
				}
			}
		}
	}()

	return &ui
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

	// create components shared by pages

	//title row flex
	titleFlex := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(ui.startStopStatus, 0, 1, false).
		AddItem(ui.currentPage, 0, 1, false).
		AddItem(ui.playerStatus, 0, 1, false)

	browserFlex, addToPlaylistModal := ui.createBrowserPage(titleFlex, indexes)
	queueFlex := ui.createQueuePage(titleFlex)
	playlistFlex, deletePlaylistModal := ui.createPlaylistPage(titleFlex)
	logListFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(titleFlex, 1, 0, false).
		AddItem(ui.logList, 0, 1, true)

	// handle
	go ui.handleMpvEvents()

	ui.pages.AddPage("browser", browserFlex, true, true).
		AddPage("queue", queueFlex, true, false).
		AddPage("playlists", playlistFlex, true, false).
		AddPage("addToPlaylist", addToPlaylistModal, true, false).
		AddPage("deletePlaylist", deletePlaylistModal, true, false).
		AddPage("log", logListFlex, true, false)

	ui.pages.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// we don't want any of these firing if we're trying to add a new playlist
		focused := ui.app.GetFocus()
		if focused == ui.newPlaylistInput || focused == ui.searchField {
			return event
		}

		switch keyName(event) {
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
		case keybind("addRandomSongs"):
			ui.handleAddRandomSongs()
		case keybind("clearQueue"):
			ui.player.Queue = make([]QueueItem, 0)
			err := ui.player.Stop()
			if err != nil {
				ui.connection.Logger.Printf("InitGui: Stop -- %s", err.Error())
			}
			updateQueueList(ui.player, ui.queueList, ui.starIdList)
		case keybind("playPause"):
			status, err := ui.player.Pause()
			if err != nil {
				ui.connection.Logger.Printf("InitGui: Pause -- %s", err.Error())
				ui.startStopStatus.SetText("[::b]stmp: [red]error")
				return nil
			}
			if status == PlayerStopped {
				ui.startStopStatus.SetText("[::b]stmp: [red]stopped")
			} else if status == PlayerPlaying {
				ui.startStopStatus.SetText("[::b]stmp: [green]playing " + ui.player.Queue[0].Title)
			} else if status == PlayerPaused {
				ui.startStopStatus.SetText("[::b]stmp: [yellow]paused")
			}
			return nil
		case keybind("volumeDown"):
			if err := ui.player.AdjustVolume(-5); err != nil {
				ui.connection.Logger.Printf("InitGui: AdjustVolume %d -- %s", -5, err.Error())
			}
			return nil
		case keybind("playnexttrack"):
    if err := ui.player.PlayNextTrack(); err != nil {
        ui.connection.Logger.Printf("InitGui: PlayNextTrack -- %s", err.Error())
    } else {
        // get the current track after advancing
        if track := ui.player.CurrentTrack(); track != nil {
            ui.startStopStatus.SetText("[::b]stmp: [green]playing " + track.Title)
        } else {
            ui.startStopStatus.SetText("[::b]stmp: [red]no track")
        }
    }
    return nil
		case keybind("volumeUp"):
			if err := ui.player.AdjustVolume(5); err != nil {
				ui.connection.Logger.Printf("InitGui: AdjustVolume %d -- %s", 5, err.Error())
			}
			updateQueueList(ui.player, ui.queueList, ui.starIdList)
			return nil
		case keybind("seekForward"):
			if err := ui.player.Seek(10); err != nil {
				ui.connection.Logger.Printf("InitGui: Seek %d -- %s", 10, err.Error())
			}
			return nil
		case keybind("seekBack"):
			if err := ui.player.Seek(-10); err != nil {
				ui.connection.Logger.Printf("InitGui: Seek %d -- %s", -10, err.Error())
			}
			return nil
		case keybind("up"):
			return tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
		case keybind("down"):
			return tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
		}

		return event
	})

	if err := ui.app.SetRoot(ui.pages, true).SetFocus(ui.pages).EnableMouse(true).Run(); err != nil {
		panic(err)
	}

	return ui
}


func queueListTextFormat(queueItem QueueItem, starredItems map[string]struct{} ) string {
	min, sec := iSecondsToMinAndSec(queueItem.Duration)
	var star = ""
	_, hasStar := starredItems[queueItem.Id]
	if hasStar {
		star = " [red]♥"
	}
	return fmt.Sprintf("%s - %s - %02d:%02d %s", queueItem.Title, queueItem.Artist, min, sec,star)
}

// Just update the text of a specific row
func updateQueueListItem(queueList *tview.List, id int, text string) {
	queueList.SetItemText(id, text, "")
}

func updateQueueList(player *Player, queueList *tview.List, starredItems map[string]struct{}) {
	queueList.Clear()
	for _, queueItem := range player.Queue {
		queueList.AddItem(queueListTextFormat(queueItem, starredItems), "", 0, nil)
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
			// we don't want to update anything if we're in the process of replacing the current track
		} else if e.Event_Id == mpv.EVENT_END_FILE && !ui.player.ReplaceInProgress {
			ui.startStopStatus.SetText("[::b]stmp: [red]stopped")
			// TODO it's gross that this is here, need better event handling
			if len(ui.player.Queue) > 0 {
				ui.player.Queue = ui.player.Queue[1:]
			}
			updateQueueList(ui.player, ui.queueList, ui.starIdList)
			err := ui.player.PlayNextTrack()
			if err != nil {
				ui.connection.Logger.Printf("handleMoveEvents: PlayNextTrack -- %s", err.Error())
			}
		} else if e.Event_Id == mpv.EVENT_START_FILE {
			ui.player.ReplaceInProgress = false
			updateQueueList(ui.player, ui.queueList, ui.starIdList)

			if len(ui.player.Queue) > 0 {
				currentSong := ui.player.Queue[0]
				ui.startStopStatus.SetText("[::b]stmp: [green]playing " + currentSong.Title)

				if ui.connection.Scrobble {
					// scrobble "now playing" event
					ui.connection.ScrobbleSubmission(currentSong.Id, false)

					// scrobble "submission" after song has been playing a bit
					// see: https://www.last.fm/api/scrobbling
					// A track should only be scrobbled when the following conditions have been met:
					// The track must be longer than 30 seconds. And the track has been played for
					// at least half its duration, or for 4 minutes (whichever occurs earlier.)
					if currentSong.Duration > 30 {
						scrobbleDelay := currentSong.Duration / 2
						if scrobbleDelay > 240 {
							scrobbleDelay = 240
						}
						scrobbleDuration := time.Duration(scrobbleDelay) * time.Second

						ui.scrobbleTimer.Reset(scrobbleDuration)
						ui.connection.Logger.Printf("scrobbler: timer started, %v", scrobbleDuration)
					} else {
						ui.connection.Logger.Printf("scrobbler: track too short")
					}
				}
			}
		} else if e.Event_Id == mpv.EVENT_IDLE || e.Event_Id == mpv.EVENT_NONE {
			continue
		}

		position, err := ui.player.Instance.GetProperty("time-pos", mpv.FORMAT_DOUBLE)
		if err != nil {
			ui.connection.Logger.Printf("handleMoveEvents (%s): GetProperty %s -- %s", e.Event_Id.String(), "time-pos", err.Error())
		}
		// TODO only update these as needed
		duration, err := ui.player.Instance.GetProperty("duration", mpv.FORMAT_DOUBLE)
		if err != nil {
			ui.connection.Logger.Printf("handleMoveEvents (%s): GetProperty %s -- %s", e.Event_Id.String(), "duration", err.Error())
		}
		volume, err := ui.player.Instance.GetProperty("volume", mpv.FORMAT_INT64)
		if err != nil {
			ui.connection.Logger.Printf("handleMoveEvents (%s): GetProperty %s -- %s", e.Event_Id.String(), "volume", err.Error())
		}

		if position == nil {
			position = 0.0
		}

		if duration == nil {
			duration = 0.0
		}

		if volume == nil {
			volume = 0
		}

		ui.playerStatus.SetText(formatPlayerStatus(volume.(int64), position.(float64), duration.(float64)))
		ui.app.Draw()
	}
}

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

func (p *Player) CurrentTrack() *QueueItem {
    if len(p.Queue) == 0 || p.CurrentIndex < 0 || p.CurrentIndex >= len(p.Queue) {
        return nil
    }
    return &p.Queue[p.CurrentIndex]
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
