//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode"
	"unsafe"

	"github.com/Scharkenberg/PurpleDisc/v4/internal/audio"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/bridge"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/config"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/deezer"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/engine"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/i18n"
	pdlog "github.com/Scharkenberg/PurpleDisc/v4/internal/log"
	"github.com/Scharkenberg/PurpleDisc/v4/internal/update"
	version "github.com/Scharkenberg/PurpleDisc/v4/internal/version"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	className         = "PurpleDiscWin32"
	titleText         = "Purple Disc for Deezer"
	wmApp             = 0x8000
	wmUpdates         = wmApp + 1
	wmShutdownDone    = wmApp + 2
	wmCommand         = 0x0111
	wmKeyDown         = 0x0100
	wmContextMenu     = 0x007b
	wmInitMenuPopup   = 0x0117
	wmAppCommand      = 0x0319
	emSetPasswordChar = 0x00cc
	tbThumbTrack      = 5
	tbEndTrack        = 8
	wsClipChildren    = 0x02000000
	wsExLayoutRTL     = 0x00400000
	spiSetNCM         = 0x002a
	vkReturn          = 0x0d
	vkBack            = 0x08
	vkSpace           = 0x20
	vkLeft            = 0x25
	vkRight           = 0x27
	wmSize            = 0x0005
	wmClose           = 0x0010
	wmDestroy         = 0x0002
	wmEraseBkgnd      = 0x0014
	wmCtlColorEdit    = 0x0133
	wmCtlColorList    = 0x0134
	wmCtlColorStatic  = 0x0138
	emSetCueBanner    = 0x1501
	wmSetFont         = 0x0030
	wmTimer           = 0x0113
	wmHScroll         = 0x0114
	wmSettingChange   = 0x001a
	wmThemeChanged    = 0x031a
	wmDPIChanged      = 0x02e0
	wsVisible         = 0x10000000
	wsChild           = 0x40000000
	wsExControlParent = 0x00010000
	wsBorder          = 0x00800000
	wsTabStop         = 0x00010000
	wsOverlapped      = 0x00CF0000
	bsPushButton      = 0
	esAutoHScroll     = 0x0080
	esPassword        = 0x0020
	// The list is a virtual list view (LVS_OWNERDATA): it asks for the text of the rows
	// it draws, so a page of any length is one LVM_SETITEMCOUNT.
	lvsList            = 0x1 | 0x4 | 0x8 | 0x40 | 0x1000 | 0x4000 // REPORT, SINGLESEL, SHOWSELALWAYS, SHAREIMAGELISTS, OWNERDATA, NOCOLUMNHEADER
	lvmSetBkColor      = 0x1001
	lvmGetNextItem     = 0x100c
	lvmGetItemRect     = 0x100e
	lvmHitTest         = 0x1012
	lvmEnsureVisible   = 0x1013
	lvmScroll          = 0x1014
	lvmSetColumnWidth  = 0x101e
	lvmSetTextColor    = 0x1024
	lvmSetTextBkColor  = 0x1026
	lvmGetTopIndex     = 0x1027
	lvmGetCountPerPage = 0x1028
	lvmSetItemState    = 0x102b
	lvmSetItemCount    = 0x102f
	lvmSetExStyle      = 0x1036
	lvmInsertColumn    = 0x1061
	lvnGetDispInfo     = -177
	nmDblClk           = -3
	ttnGetDispInfo     = -530
	wmNotify           = 0x004e
	wmDrawItem         = 0x002b
	tbmGetPos          = 0x0400
	tbmSetPos          = 0x0405
	tbmSetRange        = 0x0406
	tbsNoTicks         = 0x0010
	spiGetNCM          = 0x0029
	swpNoZOrder        = 0x0004
	swpNoActivate      = 0x0010
	minWindowWidth     = 480
	mfGrayed           = 0x0001
	mfChecked          = 0x0008
	mfPopup            = 0x0010
	mfSeparator        = 0x0800
	mfByPosition       = 0x0400
	tpmReturnCommand   = 0x0100
	idSearch           = 100
	idSearchGo         = 101
	idHome             = 102
	idLiked            = 103
	idPlaylists        = 104
	idFlow             = 105
	idCharts           = 106
	idPodcasts         = 107
	idRecent           = 108
	idNewPlaylist      = 109
	idLibrary          = 110
	idQueue            = 111
	idLike             = 112
	idPrev             = 113
	idPlayPause        = 114
	idStop             = 115
	idNext             = 116
	idLyrics           = 119
	idDownload         = 122
	idPin              = 123
	idConnect          = 124
	idRemote           = 125
	idList             = 126
	idStatus           = 127
	idTrack            = 128
	idSignOut          = 130
	idAddQueue         = 131
	idOfflineMode      = 132
	idSeek             = 135
	idVolume           = 136
	idVolumeIcon       = 137
	idCover            = 138
	// Menu-only commands; 1 and 2 are IDOK and IDCANCEL, sent by Enter and Escape.
	idUpdate     = 201
	idGapless    = 202
	idReplayGain = 203
	idQuality    = 210 // + 0..2
	idShuffle    = 220
	idRepeat     = 230 // + 0..2
	idCrossfade  = 240 // + index in fades
	idSpeed      = 250 // + index in speeds
	idMono       = 260
	idEqualizer  = 261
	idPreset     = 270 // + index in audio.EQPresetNames
	idSleep      = 290 // + index in presets
	idBack       = 300
	idFind       = 301
	idPlay       = 302
	idPlayNext   = 303
	idOpen       = 304
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	comctl32             = syscall.NewLazyDLL("comctl32.dll")
	gdi32                = syscall.NewLazyDLL("gdi32.dll")
	dwmapi               = syscall.NewLazyDLL("dwmapi.dll")
	procRegisterClass    = user32.NewProc("RegisterClassExW")
	procCreateWindow     = user32.NewProc("CreateWindowExW")
	procDefWindowProc    = user32.NewProc("DefWindowProcW")
	procShowWindow       = user32.NewProc("ShowWindow")
	procUpdateWindow     = user32.NewProc("UpdateWindow")
	procGetMessage       = user32.NewProc("GetMessageW")
	procIsDialogMessage  = user32.NewProc("IsDialogMessageW")
	procTranslate        = user32.NewProc("TranslateMessage")
	procTranslateAccel   = user32.NewProc("TranslateAcceleratorW")
	procCreateAccel      = user32.NewProc("CreateAcceleratorTableW")
	procDispatch         = user32.NewProc("DispatchMessageW")
	procPostMessage      = user32.NewProc("PostMessageW")
	procSendMessage      = user32.NewProc("SendMessageW")
	procSetWindowText    = user32.NewProc("SetWindowTextW")
	procGetWindowRect    = user32.NewProc("GetWindowRect")
	procGetWindowText    = user32.NewProc("GetWindowTextW")
	procGetTextLength    = user32.NewProc("GetWindowTextLengthW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procMessageBox       = user32.NewProc("MessageBoxW")
	procSetTimer         = user32.NewProc("SetTimer")
	procKillTimer        = user32.NewProc("KillTimer")
	procGetModule        = kernel32.NewProc("GetModuleHandleW")
	procLoadLibrary      = kernel32.NewProc("LoadLibraryW")
	procGetProcAddress   = kernel32.NewProc("GetProcAddress")
	procFindResource     = kernel32.NewProc("FindResourceW")
	procSizeofResource   = kernel32.NewProc("SizeofResource")
	procLoadResource     = kernel32.NewProc("LoadResource")
	procLockResource     = kernel32.NewProc("LockResource")
	procInitCommon       = comctl32.NewProc("InitCommonControls")
	procCreateFont       = gdi32.NewProc("CreateFontIndirectW")
	procDeleteObject     = gdi32.NewProc("DeleteObject")
	procCreateDIBSection = gdi32.NewProc("CreateDIBSection")
	procImageListCreate  = comctl32.NewProc("ImageList_Create")
	procImageListDestroy = comctl32.NewProc("ImageList_Destroy")
	procImageListAdd     = comctl32.NewProc("ImageList_Add")
	procImageListReplace = comctl32.NewProc("ImageList_Replace")
	procImageListDraw    = comctl32.NewProc("ImageList_Draw")
	procImageListCount   = comctl32.NewProc("ImageList_GetImageCount")
	procSelectObject     = gdi32.NewProc("SelectObject")
	procGetTextMetrics   = gdi32.NewProc("GetTextMetricsW")
	procAddFontMem       = gdi32.NewProc("AddFontMemResourceEx")
	procGetDC            = user32.NewProc("GetDC")
	procReleaseDC        = user32.NewProc("ReleaseDC")
	procSetWindowPos     = user32.NewProc("SetWindowPos")
	procGetPlacement     = user32.NewProc("GetWindowPlacement")
	procSetPlacement     = user32.NewProc("SetWindowPlacement")
	procBeginDefer       = user32.NewProc("BeginDeferWindowPos")
	procDeferWindowPos   = user32.NewProc("DeferWindowPos")
	procEndDefer         = user32.NewProc("EndDeferWindowPos")
	procGetClientRect    = user32.NewProc("GetClientRect")
	procClientToScreen   = user32.NewProc("ClientToScreen")
	procScreenToClient   = user32.NewProc("ScreenToClient")
	procFillRect         = user32.NewProc("FillRect")
	procCreatePen        = gdi32.NewProc("CreatePen")
	procGetStockObject   = gdi32.NewProc("GetStockObject")
	procRoundRect        = gdi32.NewProc("RoundRect")
	procExcludeClipRect  = gdi32.NewProc("ExcludeClipRect")
	procInvalidateRect   = user32.NewProc("InvalidateRect")
	procGetSysColorBrush = user32.NewProc("GetSysColorBrush")
	procGetSysColor      = user32.NewProc("GetSysColor")
	procGetDpiForWindow  = user32.NewProc("GetDpiForWindow")
	procGetDlgItem       = user32.NewProc("GetDlgItem")
	procSetFocus         = user32.NewProc("SetFocus")
	procGetFocus         = user32.NewProc("GetFocus")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procNotifyWinEvent   = user32.NewProc("NotifyWinEvent")
	procCoInitializeEx   = syscall.NewLazyDLL("ole32.dll").NewProc("CoInitializeEx")
	procCoCreateInstance = syscall.NewLazyDLL("ole32.dll").NewProc("CoCreateInstance")
	procLoadImage        = user32.NewProc("LoadImageW")
	procFindWindow       = user32.NewProc("FindWindowW")
	procIsIconic         = user32.NewProc("IsIconic")
	procSetForeground    = user32.NewProc("SetForegroundWindow")
	procSystemInfoDpi    = user32.NewProc("SystemParametersInfoForDpi")
	procSystemInfo       = user32.NewProc("SystemParametersInfoW")
	procSetTextColor     = gdi32.NewProc("SetTextColor")
	procSetBkColor       = gdi32.NewProc("SetBkColor")
	procSetBkMode        = gdi32.NewProc("SetBkMode")
	procCreateBrush      = gdi32.NewProc("CreateSolidBrush")
	procDwmSetWindow     = dwmapi.NewProc("DwmSetWindowAttribute")
	procSetWindowTheme   = syscall.NewLazyDLL("uxtheme.dll").NewProc("SetWindowTheme")
	procSetSubclass      = comctl32.NewProc("SetWindowSubclass")
	procDefSubclass      = comctl32.NewProc("DefSubclassProc")
	procShellExecute     = syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW")
	procCreateMenu       = user32.NewProc("CreateMenu")
	procCreatePopupMenu  = user32.NewProc("CreatePopupMenu")
	procAppendMenu       = user32.NewProc("AppendMenuW")
	procDeleteMenu       = user32.NewProc("DeleteMenu")
	procGetMenuCount     = user32.NewProc("GetMenuItemCount")
	procSetMenu          = user32.NewProc("SetMenu")
	procTrackPopupMenu   = user32.NewProc("TrackPopupMenu")
	procDestroyMenu      = user32.NewProc("DestroyMenu")
	procModifyMenu       = user32.NewProc("ModifyMenuW")
	procSetMenuItemInfo  = user32.NewProc("SetMenuItemInfoW")
	procSetMenuInfo      = user32.NewProc("SetMenuInfo")
	procDrawMenuBar      = user32.NewProc("DrawMenuBar")
	procDrawText         = user32.NewProc("DrawTextW")
	procGetWindowDC      = user32.NewProc("GetWindowDC")
	procMapWindowPoints  = user32.NewProc("MapWindowPoints")
	uxthemeModule        uintptr
	setPreferredAppMode  uintptr
	flushMenuThemes      uintptr
)

type point struct{ X, Y int32 }
type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}
type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSmall  uintptr
}
type rect struct{ Left, Top, Right, Bottom int32 }
type minMaxInfo struct{ Reserved, MaxSize, MaxPosition, MinTrackSize, MaxTrackSize point }
type placement struct {
	Length, Flags, ShowCmd uint32
	Min, Max               point
	Normal                 rect
}
type measureItem struct {
	CtlType, CtlID, ItemID, ItemWidth, ItemHeight uint32
	ItemData                                      uintptr
}
type drawItem struct {
	CtlType, CtlID, ItemID, ItemAction, ItemState uint32
	Item, DC                                      uintptr
	Rect                                          rect
	ItemData                                      uintptr
}
type menuInfo struct {
	Size, Mask, Style, YMax uint32
	Back                    uintptr
	ContextHelpID           uint32
	MenuData                uintptr
}
type nmhdr struct {
	From, ID uintptr
	Code     int32
}
type lvItem struct {
	Mask             uint32
	Item, SubItem    int32
	State, StateMask uint32
	Text             *uint16
	TextMax, Image   int32
	Param            uintptr
	Indent, GroupID  int32
	Columns          uint32
	PColumns, PFmt   uintptr
	Group            int32
}
type lvDispInfo struct {
	Hdr  nmhdr
	Item lvItem
}
type lvColumn struct {
	Mask                      uint32
	Fmt, Cx                   int32
	Text                      uintptr
	TextMax, SubItem          int32
	Image, Order              int32
	CxMin, CxDefault, CxIdeal int32
}
type lvHitTest struct {
	Pt                   point
	Flags                uint32
	Item, SubItem, Group int32
}
type toolInfo struct {
	Size, Flags uint32
	Hwnd, ID    uintptr
	Rect        rect
	Inst, Text  uintptr
	Param, Res  uintptr
}
type tipDispInfo struct {
	Hdr   nmhdr
	Text  *uint16
	Buf   [80]uint16
	Inst  uintptr
	Flags uint32
	Param uintptr
}
type menuItemInfo struct {
	Size, Mask, Type, State, ID uint32
	SubMenu, Checked, Unchecked uintptr
	ItemData, TypeData          uintptr
	Chars                       uint32
	Item                        uintptr
}
type accel struct {
	Virt uint8
	_    uint8
	Key  uint16
	Cmd  uint16
}
type logFont struct {
	Height, Width, Escapement, Orientation, Weight int32
	Italic, Underline, StrikeOut, CharSet          uint8
	OutPrecision, ClipPrecision, Quality           uint8
	PitchAndFamily                                 uint8
	FaceName                                       [32]uint16
}
type nonClientMetrics struct {
	Size                                                 uint32
	BorderWidth, ScrollWidth, ScrollHeight, CaptionWidth int32
	CaptionHeight                                        int32
	CaptionFont                                          logFont
	SmallCaptionWidth, SmallCaptionHeight                int32
	SmallCaptionFont                                     logFont
	MenuWidth, MenuHeight                                int32
	MenuFont, StatusFont, MessageFont                    logFont
	PaddedBorderWidth                                    int32
}
type highContrast struct {
	Size, Flags uint32
	Scheme      uintptr
}

type row struct {
	kind  string
	track deezer.Track
	id    string
	title string
	meta  string
	art   string // the cover of an album, playlist, artist or podcast; a track's is its own
}
type customDraw struct {
	Hdr                 nmhdr
	Stage               uint32
	DC                  uintptr
	Rect                rect
	ItemSpec            uintptr
	ItemState           uint32
	ItemParam           uintptr
	TextColor, TextBack uint32
	SubItem             int32
}

// page is a list shown before, kept for Back.
type page struct {
	rows       []row
	title      string
	top, sel   int
	lyricsFor  string
	lyricTimes []int64
}

// box is a control's place in logical (96 DPI) pixels.
type box struct{ x, y, w, h int }

// saved is what the window remembers between runs (win32.json in the config folder).
type saved struct {
	X, Y, W, H int32
	Maximized  bool
	Page       int
}

// buttons are the push buttons in creation (tab) order with their label keys; a
// button whose text changes lists every text it shows.
var buttons = []struct {
	id   int
	keys []string
}{
	{idHome, []string{"Home"}}, {idLiked, []string{"Liked"}}, {idPlaylists, []string{"Playlists"}},
	{idFlow, []string{"Flow"}}, {idCharts, []string{"Charts"}}, {idPodcasts, []string{"Podcasts"}},
	{idRecent, []string{"Recent"}}, {idNewPlaylist, []string{"New playlist"}}, {idSearchGo, []string{"Search"}},
	{idPrev, []string{"Previous"}}, {idPlayPause, []string{"Play", "Pause"}}, {idNext, []string{"Next track"}},
	{idQueue, []string{"Queue"}}, {idLyrics, []string{"Lyrics"}},
}

// menuKeys are the menu bar's titles; their mnemonics come first.
var menuKeys = []string{"Playback", "Go", "App"}

var (
	navIDs    = []int{idHome, idLiked, idPlaylists, idFlow, idCharts, idPodcasts, idRecent, idNewPlaylist}
	bottomIDs = []int{idPrev, idPlayPause, idNext, idQueue, idLyrics}
	presets   = []int{0, 5, 10, 20, 30, 45, 60, -1} // minutes; 0 off, -1 end of track
	fades     = []int{0, 3000, 6000, 12000}
	speeds    = []float64{0.5, 0.75, 1, 1.25, 1.5, 2}
)

type app struct {
	hwnd, edit, list, status, trackLabel, seek, volume uintptr
	font                                               uintptr
	backgroundBrush                                    uintptr
	menuBar, menuPlayback, menuGo, menuApp             uintptr
	accels                                             uintptr
	scale                                              float64
	textH                                              int // the UI font's line height in physical pixels
	editProc                                           uintptr
	tip, iconFont, boldFont                            uintptr
	thumbs, bigThumb                                   uintptr // image lists: row thumbnails (0 the placeholder) and the now-playing cover
	thumbPx, bigPx, nextSlot, thumbGap                 int
	coverIdx                                           map[string]int // cover URL -> thumbnail
	coverSlotURL                                       map[int]string
	coverWant, coverFail                               map[string]bool
	coverMu                                            sync.Mutex
	coverQueue                                         []coverRequest
	coverCond                                          *sync.Cond // on coverMu: a request for an idle worker
	nowArt                                             string
	nowReady                                           bool
	menuItems                                          []string // the open menu's items, by item data - menuItemBase
	menuSubs                                           []bool   // which of them open a submenu
	menuBack                                           uintptr
	twoColumns                                         bool // a row on the page has a second column
	dark, highContrast, inter                          bool
	current, playingID                                 string
	nowTrack                                           deezer.Track
	rows                                               []row
	history                                            []page
	liked                                              map[string]bool
	queue                                              []deezer.Track // episodes only; tracks play from the engine queue
	queueIndex                                         int
	episodeMode                                        bool
	finishedSeen                                       int
	pendingTrack                                       *deezer.Track
	page                                               string
	lastNav                                            int
	updates                                            chan func()
	closed                                             atomic.Bool
	destroyed                                          bool
	labels                                             map[int][]string
	menuTitles                                         []string
	playbackTitle, playbackTrackID                     string
	playbackState                                      audio.State
	playbackStatusActive                               bool
	generation                                         int
	lyricsFor                                          string  // the track whose lyrics the list shows, "" on other pages
	lyricTimes                                         []int64 // each synced line's start, nil for plain lyrics
	lyricLine                                          int     // the line selected as sung, -1 for none
	pauseShown                                         bool    // the Play button reads Pause
	seeking                                            bool
	trackText                                          string
	tick                                               int // timer period in ms, 0 when stopped
	minimized                                          bool
	idleTicks                                          int
	release                                            update.Info
}

func main() {
	mutex, _ := syscall.UTF16PtrFromString("PurpleDisc.Win32")
	if _, err := windows.CreateMutex(nil, false, mutex); err == windows.ERROR_ALREADY_EXISTS {
		name, _ := syscall.UTF16PtrFromString(className)
		if h, _, _ := procFindWindow.Call(uintptr(unsafe.Pointer(name)), 0); h != 0 {
			if iconic, _, _ := procIsIconic.Call(h); iconic != 0 {
				procShowWindow.Call(h, 9) // SW_RESTORE
			}
			procSetForeground.Call(h)
		}
		return
	}
	if dir, err := config.Dir(); err == nil {
		_ = pdlog.OpenFile(dir)
	}
	pdlog.Info("Purple Disc Win32 %s starting", version.Number)
	if lang := config.LoadLanguage(); lang != "" {
		i18n.SetLocale(lang)
	} else {
		i18n.SetLocale(i18n.DetectLocale())
	}
	app := &app{coverWant: map[string]bool{}, coverFail: map[string]bool{}, updates: make(chan func(), 32), queueIndex: -1, scale: 1, lastNav: idHome, liked: map[string]bool{}}
	app.coverCond = sync.NewCond(&app.coverMu)
	app.run()
}

func (a *app) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	procCoInitializeEx.Call(0, 2) // COINIT_APARTMENTTHREADED, for liveStatus
	a.applyTheme()
	a.inter = loadInter()
	engine.SetHostAdvances(false)
	procInitCommon.Call()
	name, _ := syscall.UTF16PtrFromString(className)
	title, _ := syscall.UTF16PtrFromString(titleText)
	inst, _, _ := procGetModule.Call(0)
	icon, _, _ := procLoadImage.Call(inst, 1, 1, 0, 0, 0x8040) // resource 1, IMAGE_ICON, LR_DEFAULTSIZE | LR_SHARED
	wc := wndClassEx{Size: uint32(unsafe.Sizeof(wndClassEx{})), WndProc: windows.NewCallback(func(hwnd, message, wp, lp uintptr) uintptr { return a.wndProc(hwnd, uint32(message), wp, lp) }), Instance: inst, Icon: icon, ClassName: name}
	if atom, _, err := procRegisterClass.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
		startFailed("RegisterClassExW", err)
		return
	}
	exStyle := uintptr(wsExControlParent)
	if i18n.Locale() == "ar" {
		exStyle |= wsExLayoutRTL
	}
	const cwUseDefault = 0x80000000
	var err error
	a.hwnd, _, err = procCreateWindow.Call(exStyle, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(title)), wsOverlapped|wsClipChildren,
		cwUseDefault, cwUseDefault, 920, 660, 0, 0, inst, 0)
	if a.hwnd == 0 {
		startFailed("CreateWindowExW", err)
		return
	}
	if !a.restorePlacement() {
		procShowWindow.Call(a.hwnd, 5)
	}
	procUpdateWindow.Call(a.hwnd)
	for range 8 { // one HTTP/2 connection carries them all
		go a.coverWorker()
	}
	keys := []accel{
		{Virt: 0x09, Key: 'F', Cmd: idFind},     // Ctrl+F
		{Virt: 0x09, Key: vkLeft, Cmd: idPrev},  // Ctrl+Left
		{Virt: 0x09, Key: vkRight, Cmd: idNext}, // Ctrl+Right
		{Virt: 0x11, Key: vkLeft, Cmd: idBack},  // Alt+Left
	}
	a.accels, _, _ = procCreateAccel.Call(uintptr(unsafe.Pointer(&keys[0])), uintptr(len(keys)))
	go func() {
		if info, err := update.Check(version.Number); err == nil && info.HasUpdate {
			a.dispatch(func() {
				a.release = info
				a.setStatus(i18n.Tf("Update available: v%s", info.Latest))
			})
		}
	}()
	var m msg
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		if m.Message == wmKeyDown && !a.closed.Load() && a.key(m.Hwnd, m.WParam, m.LParam) {
			continue
		}
		// Ctrl+Left and Ctrl+Right stay word moves in the field.
		if m.Hwnd != a.edit || m.WParam == 'F' {
			if done, _, _ := procTranslateAccel.Call(a.hwnd, a.accels, uintptr(unsafe.Pointer(&m))); done != 0 {
				continue
			}
		}
		if handled, _, _ := procIsDialogMessage.Call(a.hwnd, uintptr(unsafe.Pointer(&m))); handled != 0 {
			continue
		}
		procTranslate.Call(uintptr(unsafe.Pointer(&m)))
		procDispatch.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// key handles Enter in the field and Enter, Space and Backspace on the list.
func (a *app) key(hwnd, vk, lp uintptr) bool {
	if hwnd != a.edit && hwnd != a.list || lp&(1<<30) != 0 && vk != vkBack {
		return (hwnd == a.edit || hwnd == a.list) && vk == vkReturn // a held Enter repeats nothing
	}
	switch {
	case hwnd == a.edit && vk == vkReturn:
		a.search()
	case hwnd == a.list && vk == vkReturn:
		a.activateSelected()
	case hwnd == a.list && vk == vkSpace:
		a.command(idPlayPause)
	case hwnd == a.list && vk == vkBack:
		a.back()
	default:
		return false
	}
	return true
}

func startFailed(call string, err error) {
	pdlog.Warn("%s: %v", call, err)
	text, _ := syscall.UTF16PtrFromString(i18n.T("Purple Disc couldn't start.") + "\n\n" + call + ": " + err.Error())
	title, _ := syscall.UTF16PtrFromString(titleText)
	procMessageBox.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x10) // MB_ICONERROR
}

// loadInter makes Inter (resource 2, RCDATA) available to this process only.
func loadInter() bool {
	res, _, _ := procFindResource.Call(0, 2, 10)
	if res == 0 {
		return false
	}
	size, _, _ := procSizeofResource.Call(0, res)
	data, _, _ := procLoadResource.Call(0, res)
	p, _, _ := procLockResource.Call(data)
	var fonts uint32
	h, _, _ := procAddFontMem.Call(p, size, 0, uintptr(unsafe.Pointer(&fonts)))
	return h != 0
}

func savedPath() string { return filepath.Join(configDir(), "win32.json") }

// restorePlacement shows the window where it was last closed; false on a first run.
func (a *app) restorePlacement() bool {
	b, err := os.ReadFile(savedPath())
	var s saved
	if err != nil || json.Unmarshal(b, &s) != nil || s.W <= 0 || s.H <= 0 {
		return false
	}
	if s.Page >= idHome && s.Page <= idRecent {
		a.lastNav = s.Page
	}
	p := placement{Length: uint32(unsafe.Sizeof(placement{})), ShowCmd: 1, Normal: rect{s.X, s.Y, s.X + s.W, s.Y + s.H}}
	if s.Maximized {
		p.ShowCmd = 3
	}
	ok, _, _ := procSetPlacement.Call(a.hwnd, uintptr(unsafe.Pointer(&p)))
	return ok != 0
}

func (a *app) savePlacement() {
	p := placement{Length: uint32(unsafe.Sizeof(placement{}))}
	if ok, _, _ := procGetPlacement.Call(a.hwnd, uintptr(unsafe.Pointer(&p))); ok == 0 {
		return
	}
	n := p.Normal
	b, _ := json.Marshal(saved{X: n.Left, Y: n.Top, W: n.Right - n.Left, H: n.Bottom - n.Top, Maximized: p.ShowCmd == 3, Page: a.lastNav})
	_ = config.WriteFile("win32.json", string(b))
}

func (a *app) applyTheme() {
	mode := uintptr(3) // ForceLight
	hc := highContrast{Size: uint32(unsafe.Sizeof(highContrast{}))}
	highContrastOn := false
	if ok, _, _ := procSystemInfo.Call(0x0042, uintptr(hc.Size), uintptr(unsafe.Pointer(&hc)), 0); ok != 0 {
		highContrastOn = hc.Flags&1 != 0
	}
	if highContrastOn {
		mode = 0 // Default
	} else {
		theme := ""
		if dir, err := config.Dir(); err == nil {
			if b, err := os.ReadFile(filepath.Join(dir, "settings.json")); err == nil {
				var settings struct {
					Theme string `json:"theme"`
				}
				_ = json.Unmarshal(b, &settings)
				theme = strings.ToLower(strings.TrimSpace(settings.Theme))
			}
		}
		switch theme {
		case "dark":
			mode = 2 // ForceDark
		case "light":
			mode = 3 // ForceLight
		default:
			key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
			if err == nil {
				light, _, err := key.GetIntegerValue("AppsUseLightTheme")
				key.Close()
				if err == nil && light == 0 {
					mode = 2
				}
			}
		}
	}
	oldDark := a.dark
	a.highContrast = highContrastOn
	a.dark = mode == 2
	if oldDark && a.backgroundBrush != 0 {
		procDeleteObject.Call(a.backgroundBrush)
	}
	if a.dark {
		brush, _, _ := procCreateBrush.Call(0x202020)
		if brush != 0 {
			a.backgroundBrush = brush
		}
	} else {
		a.backgroundBrush, _, _ = procGetSysColorBrush.Call(5) // COLOR_WINDOW
	}
	if uxthemeModule == 0 {
		dll, _ := syscall.UTF16PtrFromString("uxtheme.dll")
		uxthemeModule, _, _ = procLoadLibrary.Call(uintptr(unsafe.Pointer(dll)))
		if uxthemeModule != 0 {
			setPreferredAppMode, _, _ = procGetProcAddress.Call(uxthemeModule, 135)
			flushMenuThemes, _, _ = procGetProcAddress.Call(uxthemeModule, 136)
		}
	}
	if setPreferredAppMode != 0 {
		_, _, _ = syscall.SyscallN(setPreferredAppMode, mode)
	}
	if flushMenuThemes != 0 {
		_, _, _ = syscall.SyscallN(flushMenuThemes)
	}
	if a.hwnd != 0 && !a.highContrast {
		dark := uint32(0)
		if a.dark {
			dark = 1
		}
		result, _, _ := procDwmSetWindow.Call(a.hwnd, 20, uintptr(unsafe.Pointer(&dark)), unsafe.Sizeof(dark))
		if result != 0 {
			procDwmSetWindow.Call(a.hwnd, 19, uintptr(unsafe.Pointer(&dark)), unsafe.Sizeof(dark))
		}
	}
}

func (a *app) wndProc(hwnd uintptr, message uint32, wp, lp uintptr) uintptr {
	switch message {
	case 0x0001: // WM_CREATE
		a.hwnd = hwnd
		if dpi, _, _ := procGetDpiForWindow.Call(hwnd); dpi != 0 {
			a.scale = float64(dpi) / 96
		}
		procSetWindowPos.Call(hwnd, 0, 0, 0, uintptr(920*a.scale), uintptr(660*a.scale), 0x0001|swpNoZOrder|swpNoActivate) // SWP_NOMOVE
		a.applyTheme()
		arl := loadARL()
		a.createControls(arl)
		if arl != "" {
			a.login(arl)
		} else {
			a.showHint()
		}
		return 0
	case wmSize:
		if wp == 1 { // SIZE_MINIMIZED
			a.minimized = true
			a.setTick(a.tick)
			return 0
		}
		a.layout(int(float64(uint32(lp)&0xffff)/a.scale), int(float64((uint32(lp)>>16)&0xffff)/a.scale))
		if a.minimized {
			a.minimized = false
			a.setTick(500)
		}
		return 0
	case wmSettingChange, wmThemeChanged:
		theme := message == wmThemeChanged || lp != 0 && windows.UTF16PtrToString(*(**uint16)(unsafe.Pointer(&lp))) == "ImmersiveColorSet"
		font := message == wmSettingChange && wp == spiSetNCM
		if !theme && !font {
			break
		}
		if theme {
			a.applyTheme()
			a.applyControlTheme()
		}
		if font {
			a.applySystemFont()
		}
		procInvalidateRect.Call(hwnd, 0, 1)
		for id := idSearch; id <= idVolumeIcon; id++ {
			if h := a.child(id); h != 0 {
				procInvalidateRect.Call(h, 0, 1)
			}
		}
	case wmDPIChanged:
		dpi := uint32(wp & 0xffff)
		if dpi != 0 {
			a.scale = float64(dpi) / 96
		}
		a.applySystemFont()
		bounds := *(**rect)(unsafe.Pointer(&lp))
		procSetWindowPos.Call(hwnd, 0, uintptr(bounds.Left), uintptr(bounds.Top),
			uintptr(bounds.Right-bounds.Left), uintptr(bounds.Bottom-bounds.Top), swpNoZOrder|swpNoActivate)
		return 0
	case wmEraseBkgnd:
		bounds := rect{}
		procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&bounds)))
		procFillRect.Call(wp, uintptr(unsafe.Pointer(&bounds)), a.backgroundBrush)
		return 1
	case 0x0135: // WM_CTLCOLORBTN: the corners around a themed button's rounded frame are the window's
		if !a.highContrast {
			procSetBkMode.Call(wp, 1) // TRANSPARENT
			return a.backgroundBrush
		}
	case wmCtlColorEdit, wmCtlColorList, wmCtlColorStatic:
		if a.dark && !a.highContrast {
			if message == wmCtlColorStatic {
				procSetBkMode.Call(wp, 1) // TRANSPARENT
			} else {
				procSetBkColor.Call(wp, 0x202020)
			}
			procSetTextColor.Call(wp, 0xf0f0f0)
			return a.backgroundBrush
		}
		if message == wmCtlColorStatic && !a.highContrast {
			procSetBkMode.Call(wp, 1) // TRANSPARENT
			return a.backgroundBrush
		}
	case 0x0024: // WM_GETMINMAXINFO
		m := *(**minMaxInfo)(unsafe.Pointer(&lp))
		m.MinTrackSize = point{X: int32(minWindowWidth * a.scale), Y: int32(float64(minHeight(a.line())) * a.scale)}
		return 0
	case wmNotify:
		h := *(**nmhdr)(unsafe.Pointer(&lp))
		switch {
		case h.From == a.list && h.Code == lvnGetDispInfo:
			a.cellText(&(*(**lvDispInfo)(unsafe.Pointer(&lp))).Item)
		case h.From == a.list && h.Code == -12: // NM_CUSTOMDRAW
			return a.customDraw(*(**customDraw)(unsafe.Pointer(&lp)))
		case h.From == a.list && h.Code == nmDblClk:
			a.activateSelected()
		case h.From == a.tip && h.Code == ttnGetDispInfo:
			a.tipText(*(**tipDispInfo)(unsafe.Pointer(&lp)))
		}
		return 0
	case wmInitMenuPopup:
		if !a.highContrast {
			info := menuInfo{Size: uint32(unsafe.Sizeof(menuInfo{})), Mask: 0x2, Back: a.menuBack} // MIM_BACKGROUND
			procSetMenuInfo.Call(wp, uintptr(unsafe.Pointer(&info)))
		}
		a.fillMenu(wp)
		return 0
	case 0x002c: // WM_MEASUREITEM: a menu bar title or a menu item drawn here
		m := *(**measureItem)(unsafe.Pointer(&lp))
		switch {
		case m.CtlType != 1: // ODT_MENU only
		case m.ItemData >= menuItemBase:
			a.measureMenuItem(m)
			return 1
		case int(m.ItemData) < len(a.menuTitles):
			r := a.menuText(0, int(m.ItemData), rect{}, 0x400) // DT_CALCRECT
			m.ItemWidth, m.ItemHeight = uint32(r.Right)+uint32(12*a.scale), uint32(r.Bottom)
			return 1
		}
	case wmDrawItem:
		d := *(**drawItem)(unsafe.Pointer(&lp))
		switch {
		case d.CtlType == 5 && d.CtlID == idCover: // ODT_STATIC
			a.drawCover(d)
			return 1
		case d.CtlType == 5 && d.CtlID == idVolumeIcon: // the speaker
			procFillRect.Call(d.DC, uintptr(unsafe.Pointer(&d.Rect)), a.backgroundBrush)
			text, _, _ := procGetSysColor.Call(8) // COLOR_WINDOWTEXT
			if a.dark && !a.highContrast {
				text = 0xf0f0f0
			}
			old, _, _ := procSelectObject.Call(d.DC, a.iconFont)
			procSetBkMode.Call(d.DC, 1)
			procSetTextColor.Call(d.DC, text)
			glyph, _ := syscall.UTF16PtrFromString("")                                                        // Volume
			procDrawText.Call(d.DC, uintptr(unsafe.Pointer(glyph)), 1, uintptr(unsafe.Pointer(&d.Rect)), 0x25) // DT_CENTER | DT_VCENTER | DT_SINGLELINE
			procSelectObject.Call(d.DC, old)
			return 1
		case d.CtlType == 1 && d.ItemData >= menuItemBase: // ODT_MENU
			a.drawMenuItem(d)
			return 1
		case d.CtlType == 1 && int(d.ItemData) < len(a.menuTitles):
			back, _, _ := procGetSysColor.Call(5) // the bar is the window's color
			if a.dark {
				back = 0x202020
			}
			if d.ItemState&(0x1|0x40) != 0 { // ODS_SELECTED, ODS_HOTLIGHT
				back = a.menuColors()[1]
			}
			brush, _, _ := procCreateBrush.Call(back)
			procFillRect.Call(d.DC, uintptr(unsafe.Pointer(&d.Rect)), brush)
			procDeleteObject.Call(brush)
			flags := uintptr(0x25)      // DT_CENTER | DT_VCENTER | DT_SINGLELINE
			if d.ItemState&0x100 != 0 { // ODS_NOACCEL
				flags |= 0x100000 // DT_HIDEPREFIX
			}
			a.menuText(d.DC, int(d.ItemData), d.Rect, flags)
			return 1
		}
	case 0x0085, 0x0086: // WM_NCPAINT, WM_NCACTIVATE: the line under a dark menu bar
		r, _, _ := procDefWindowProc.Call(hwnd, uintptr(message), wp, lp)
		if a.dark && !a.highContrast {
			var c, w rect
			procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&c)))
			procMapWindowPoints.Call(hwnd, 0, uintptr(unsafe.Pointer(&c)), 2)
			procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&w)))
			line := rect{c.Left - w.Left, c.Top - w.Top - 1, c.Right - w.Left, c.Top - w.Top}
			dc, _, _ := procGetWindowDC.Call(hwnd)
			procFillRect.Call(dc, uintptr(unsafe.Pointer(&line)), a.backgroundBrush)
			procReleaseDC.Call(hwnd, dc)
		}
		return r
	case wmContextMenu:
		if wp == a.list {
			a.contextMenu(lp)
			return 0
		}
	case wmAppCommand:
		switch (lp >> 16) & 0x0fff {
		case 11: // APPCOMMAND_MEDIA_NEXTTRACK
			a.command(idNext)
		case 12: // PREVIOUSTRACK
			a.command(idPrev)
		case 13: // STOP
			a.command(idStop)
		case 14, 46, 47: // PLAY_PAUSE, PLAY, PAUSE
			a.command(idPlayPause)
		default:
			r, _, _ := procDefWindowProc.Call(hwnd, uintptr(message), wp, lp)
			return r
		}
		return 1
	case wmCommand:
		if a.closed.Load() {
			return 0
		}
		id := int(wp & 0xffff)
		notify := int((wp >> 16) & 0xffff)
		if lp != 0 && notify != 0 { // a control's notification (the field's EN_*), not a click
			return 0
		}
		a.command(id)
		return 0
	case 0x0120: // WM_MENUCHAR: Alt+letter for the menu bar's titles, which have no text when drawn dark
		if lp == a.menuBar {
			if i := menuIndex(a.menuTitles, rune(wp&0xffff)); i >= 0 {
				return 2<<16 | uintptr(i) // MNC_EXECUTE
			}
		}
	case wmHScroll:
		switch lp {
		case a.seek:
			switch wp & 0xffff {
			case tbThumbTrack:
				a.seeking = true
			case tbEndTrack:
				a.seeking = false
				pos, _, _ := procSendMessage.Call(a.seek, tbmGetPos, 0, 0)
				engine.Seek(seekMS(int64(pos), engine.DurationMS()))
				a.setTick(500)
			}
		case a.volume:
			pos, _, _ := procSendMessage.Call(a.volume, tbmGetPos, 0, 0)
			engine.SetVolume(float64(pos) / 100)
		}
		return 0
	case wmTimer:
		if !a.closed.Load() {
			a.refreshPlayer()
		}
		return 0
	case wmUpdates:
		for !a.closed.Load() {
			select {
			case f := <-a.updates:
				f()
			default:
				return 0
			}
		}
		return 0
	case wmClose:
		if !a.closed.CompareAndSwap(false, true) {
			return 0
		}
		a.setTick(0)
		a.savePlacement()
		go func() {
			engine.Stop()
			if p := engine.Player(); p != nil {
				p.Close()
			}
			procPostMessage.Call(hwnd, wmShutdownDone, 0, 0)
		}()
		time.AfterFunc(3*time.Second, func() { procPostMessage.Call(hwnd, wmShutdownDone, 0, 0) })
		return 0
	case wmShutdownDone:
		if !a.destroyed {
			a.destroyed = true
			procDestroyWindow.Call(hwnd)
		}
		return 0
	case wmDestroy:
		a.setTick(0)
		if a.font != 0 {
			procDeleteObject.Call(a.font)
		}
		if a.dark && a.backgroundBrush != 0 {
			procDeleteObject.Call(a.backgroundBrush)
		}
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProc.Call(hwnd, uintptr(message), wp, lp)
	return r
}

// setTick runs the refresh timer every ms milliseconds, or stops it at 0. Minimized, the
// playing tick slows to 1 s (the episode list's advance and the title still need it) and
// the idle one stops.
func (a *app) setTick(ms int) {
	switch {
	case a.hwnd == 0:
		ms = 0
	case a.minimized && ms == 500:
		ms = 1000
	case a.minimized && ms != 1000:
		ms = 0
	}
	if ms == a.tick {
		return
	}
	if ms == 0 {
		procKillTimer.Call(a.hwnd, 1)
	} else {
		procSetTimer.Call(a.hwnd, 1, uintptr(ms), 0)
		a.idleTicks = 0
	}
	a.tick = ms
}

// translated returns the menu titles' and each button's texts in the current language.
func translated() [][]string {
	out := make([][]string, 0, len(menuKeys)+len(buttons))
	for _, k := range menuKeys {
		out = append(out, []string{i18n.T(k)})
	}
	for _, b := range buttons {
		var texts []string
		for _, k := range b.keys {
			texts = append(texts, i18n.T(k))
		}
		out = append(out, texts)
	}
	return out
}

// mnemonics marks in each entry a letter that every text of the entry contains and no
// other entry took (never Han, which is typed through an IME). Entries with the fewest
// such letters choose first, each its earliest free one; an entry left without one keeps
// its texts unmarked.
func mnemonics(entries [][]string) [][]string {
	letters := func(texts []string) []rune {
		var cs []rune
		for _, r := range texts[0] {
			c := unicode.ToLower(r)
			inAll := unicode.IsLetter(r) && !unicode.Is(unicode.Han, r) && !slices.Contains(cs, c)
			for _, t := range texts[1:] {
				inAll = inAll && strings.ContainsRune(strings.ToLower(t), c)
			}
			if inAll {
				cs = append(cs, c)
			}
		}
		return cs
	}
	order := make([]int, len(entries))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(i, j int) int { return len(letters(entries[i])) - len(letters(entries[j])) })
	used := map[rune]bool{}
	out := slices.Clone(entries)
	for _, i := range order {
		texts := entries[i]
		for _, c := range letters(texts) {
			if used[c] {
				continue
			}
			used[c] = true
			out[i] = make([]string, len(texts))
			for j, t := range texts {
				k := strings.IndexFunc(t, func(x rune) bool { return unicode.ToLower(x) == c })
				out[i][j] = t[:k] + "&" + t[k:]
			}
			break
		}
	}
	return out
}

// menuIndex is the menu title whose mnemonic is ch, -1 for none.
func menuIndex(titles []string, ch rune) int {
	for i, t := range titles {
		if k := strings.IndexRune(t, '&'); k >= 0 {
			if r := []rune(t[k+1:]); len(r) > 0 && unicode.ToLower(r[0]) == unicode.ToLower(ch) {
				return i
			}
		}
	}
	return -1
}

func (a *app) createControls(arl string) {
	labels := mnemonics(translated())
	a.menuTitles = nil
	for _, l := range labels[:len(menuKeys)] {
		a.menuTitles = append(a.menuTitles, l[0])
	}
	a.labels = map[int][]string{}
	for i, b := range buttons {
		a.labels[b.id] = labels[len(menuKeys)+i]
	}
	a.menuBar, _, _ = procCreateMenu.Call()
	for i, m := range []*uintptr{&a.menuPlayback, &a.menuGo, &a.menuApp} {
		*m, _, _ = procCreatePopupMenu.Call()
		a.appendItem(a.menuBar, mfPopup, *m, a.menuTitles[i])
	}
	procSetMenu.Call(a.hwnd, a.menuBar)
	for _, id := range navIDs {
		a.button(id)
	}
	editStyle := uintptr(wsTabStop | esAutoHScroll) // its frame is drawn in editSubclass
	if arl == "" {
		editStyle |= esPassword
	}
	a.edit = a.control("EDIT", "", editStyle, idSearch)
	a.editProc = windows.NewCallback(func(h, m, wp, lp, id, ref uintptr) uintptr { return a.editSubclass(h, uint32(m), wp, lp) })
	procSetSubclass.Call(a.edit, a.editProc, 1, 0)
	if arl == "" {
		a.setEditCue(i18n.T("Paste your Deezer ARL"))
	} else {
		a.setEditCue(i18n.T("Search Deezer…"))
	}
	a.button(idSearchGo)
	a.list = a.control("SysListView32", "", wsBorder|wsTabStop|lvsList, idList)
	procSendMessage.Call(a.list, lvmSetExStyle, 0x14020, 0x14020) // FULLROWSELECT, LABELTIP (full text of a cut cell), DOUBLEBUFFER
	for i := range 2 {
		col := lvColumn{Mask: 0x8, SubItem: int32(i)} // LVCF_SUBITEM
		procSendMessage.Call(a.list, lvmInsertColumn, uintptr(i), uintptr(unsafe.Pointer(&col)))
	}
	a.control("STATIC", "", 0xd, idCover) // SS_OWNERDRAW: the now-playing cover, decorative
	a.status = a.control("STATIC", "", 0, idStatus)
	a.liveStatus()
	a.trackLabel = a.control("STATIC", "", 0, idTrack)
	a.setTrackText(i18n.T("Nothing playing."))
	a.seek = a.control("msctls_trackbar32", "", wsTabStop|tbsNoTicks, idSeek)
	procSendMessage.Call(a.seek, tbmSetRange, 1, uintptr(1000<<16))
	for _, id := range bottomIDs {
		a.button(id)
	}
	// A speaker drawn from the icon font; its text is the slider's name for screen readers,
	// which take a slider's name from the label before it.
	a.control("STATIC", i18n.T("Volume"), 0x10d, idVolumeIcon) // SS_OWNERDRAW | SS_NOTIFY (for its tooltip)
	a.volume = a.control("msctls_trackbar32", "", wsTabStop|tbsNoTicks, idVolume)
	procSendMessage.Call(a.volume, tbmSetRange, 1, uintptr(100<<16))
	procSendMessage.Call(a.volume, tbmSetPos, 1, 100)
	tc, _ := syscall.UTF16PtrFromString("tooltips_class32")
	a.tip, _, _ = procCreateWindow.Call(0x8, uintptr(unsafe.Pointer(tc)), 0, 0x80000003, 0, 0, 0, 0, a.hwnd, 0, 0, 0) // WS_EX_TOPMOST; WS_POPUP | TTS_ALWAYSTIP | TTS_NOPREFIX
	procSendMessage.Call(a.tip, 0x0418, 0, uintptr(360*a.scale))                                                      // TTM_SETMAXTIPWIDTH
	for id, text := range tipTexts() {
		a.addTip(a.child(id), text)
	}
	a.applyControlTheme()
	a.applySystemFont()
	a.layout(920, 660)
}

// tipTexts are the buttons' and the field's tooltips; the sliders' come with their value
// (tipText). Most reuse the terminal app's help lines, which are lower case.
func tipTexts() map[int]string {
	t, up := i18n.T, func(key string) string { return capFirst(i18n.T(key)) }
	return map[int]string{
		idHome: t("Top tracks") + " · " + up("your playlists"), idLiked: t("Liked Songs"), idPlaylists: up("your playlists"),
		idFlow: up("non-stop mix"), idCharts: up("top tracks, albums & artists"), idPodcasts: up("search shows & episodes"),
		idRecent: t("Recently played"), idNewPlaylist: t("Create a playlist with the name typed in the field"),
		idSearch: up("search Deezer") + " (Ctrl+F)", idSearchGo: up("tracks, albums, artists, playlists"),
		idPrev: t("Previous") + " (Ctrl+←)", idPlayPause: up("play / pause"), idNext: t("Next track") + " (Ctrl+→)",
		idQueue: up("queue view"), idLyrics: up("lyrics (synced when available)"),
		idVolumeIcon: "", idVolume: "", idSeek: "",
	}
}

func capFirst(s string) string {
	r := []rune(s)
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return string(r)
}

// addTip gives control h a tooltip: text, or "" for one asked for as it shows (tipText).
func (a *app) addTip(h uintptr, text string) {
	ti := toolInfo{Size: uint32(unsafe.Sizeof(toolInfo{})), Flags: 0x11, Hwnd: a.hwnd, ID: h, Text: ^uintptr(0)} // TTF_IDISHWND | TTF_SUBCLASS; LPSTR_TEXTCALLBACK
	if text != "" {
		p, _ := syscall.UTF16PtrFromString(text)
		ti.Text = uintptr(unsafe.Pointer(p))
	}
	procSendMessage.Call(a.tip, 0x0432, 0, uintptr(unsafe.Pointer(&ti))) // TTM_ADDTOOLW
}

// tipText answers TTN_GETDISPINFO for the sliders: the volume or the position.
func (a *app) tipText(d *tipDispInfo) {
	var s string
	switch d.Hdr.ID {
	case a.volume, a.child(idVolumeIcon):
		v, _, _ := procSendMessage.Call(a.volume, tbmGetPos, 0, 0)
		s = i18n.Tf("Volume %d%%", int(v))
	case a.seek:
		s = timeLabel("", engine.PositionMS(), engine.DurationMS())[4:]
	default:
		return
	}
	copy(d.Buf[:len(d.Buf)-1], windows.StringToUTF16(s))
	d.Text = &d.Buf[0]
}

func (a *app) applyControlTheme() {
	var theme *uint16
	dark := a.dark && !a.highContrast
	if dark {
		theme, _ = syscall.UTF16PtrFromString("DarkMode_Explorer")
	}
	for id := idSearch; id <= idVolumeIcon; id++ {
		if h := a.child(id); h != 0 {
			procSetWindowTheme.Call(h, uintptr(unsafe.Pointer(theme)), 0)
		}
	}
	procSetWindowTheme.Call(a.tip, uintptr(unsafe.Pointer(theme)), 0)
	back, text := uintptr(0x202020), uintptr(0xf0f0f0)
	if !dark {
		back, _, _ = procGetSysColor.Call(5) // COLOR_WINDOW
		text, _, _ = procGetSysColor.Call(8) // COLOR_WINDOWTEXT
	}
	procSendMessage.Call(a.list, lvmSetBkColor, 0, back)
	procSendMessage.Call(a.list, lvmSetTextBkColor, 0, back)
	procSendMessage.Call(a.list, lvmSetTextColor, 0, text)
	if a.thumbs != 0 {
		a.placeholder()
		procInvalidateRect.Call(a.child(idCover), 0, 0)
	}
	// The menu bar's titles are drawn here in the UI font, and in dark mode on a dark bar,
	// which Windows never draws; the menus' items too (appendItem), on menuBack.
	if a.menuBack != 0 {
		procDeleteObject.Call(a.menuBack)
	}
	a.menuBack, _, _ = procCreateBrush.Call(a.menuColors()[0])
	for i, sub := range []uintptr{a.menuPlayback, a.menuGo, a.menuApp} {
		flags, data := uintptr(mfByPosition|mfPopup), uintptr(i)
		if !a.highContrast {
			flags |= 0x100 // MF_OWNERDRAW
		} else {
			p, _ := syscall.UTF16PtrFromString(a.menuTitles[i])
			data = uintptr(unsafe.Pointer(p))
		}
		procModifyMenu.Call(a.menuBar, uintptr(i), flags, sub, data)
	}
	info := menuInfo{Size: uint32(unsafe.Sizeof(menuInfo{})), Mask: 0x2} // MIM_BACKGROUND
	if !a.highContrast {
		info.Back = a.backgroundBrush
	}
	procSetMenuInfo.Call(a.menuBar, uintptr(unsafe.Pointer(&info)))
	procDrawMenuBar.Call(a.hwnd)
}

// menuText draws (or with DT_CALCRECT measures) menu bar title i on dc, or on the
// window's when dc is 0.
func (a *app) menuText(dc uintptr, i int, r rect, flags uintptr) rect {
	own := dc == 0
	if own {
		dc, _, _ = procGetDC.Call(a.hwnd)
		defer procReleaseDC.Call(a.hwnd, dc)
	}
	old, _, _ := procSelectObject.Call(dc, a.font)
	procSetBkMode.Call(dc, 1) // TRANSPARENT
	procSetTextColor.Call(dc, a.menuColors()[2])
	p, _ := syscall.UTF16PtrFromString(a.menuTitles[i])
	procDrawText.Call(dc, uintptr(unsafe.Pointer(p)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), flags)
	procSelectObject.Call(dc, old)
	return r
}

func (a *app) setEditCue(text string) {
	p, _ := syscall.UTF16PtrFromString(text)
	procSendMessage.Call(a.edit, emSetCueBanner, 1, uintptr(unsafe.Pointer(p)))
}

func (a *app) control(class, text string, style uintptr, id int) uintptr {
	cp, _ := syscall.UTF16PtrFromString(class)
	tp, _ := syscall.UTF16PtrFromString(text)
	h, _, _ := procCreateWindow.Call(0, uintptr(unsafe.Pointer(cp)), uintptr(unsafe.Pointer(tp)), wsChild|wsVisible|style,
		0, 0, 10, 10, a.hwnd, uintptr(id), 0, 0)
	return h
}

// applySystemFont builds the UI font from the system message font at the window's DPI,
// in Inter where it is loaded, and measures it for the field's height and the list's column.
func (a *app) applySystemFont() {
	if a.hwnd == 0 || a.scale == 0 {
		return
	}
	metrics := nonClientMetrics{Size: uint32(unsafe.Sizeof(nonClientMetrics{}))}
	dpi := uintptr(96 * a.scale)
	if ok, _, _ := procSystemInfoDpi.Call(spiGetNCM, uintptr(metrics.Size), uintptr(unsafe.Pointer(&metrics)), 0, dpi); ok == 0 {
		return
	}
	// Windows' text size setting (Accessibility › Text size) scales the menu font, not the
	// message font: the larger of the two is the base.
	if metrics.MenuFont.Height < metrics.MessageFont.Height { // negative: character heights
		metrics.MessageFont.Height = metrics.MenuFont.Height
	}
	metrics.MessageFont.Height = metrics.MessageFont.Height * 11 / 10 // a little larger than the system's
	if a.inter {
		metrics.MessageFont.FaceName = [32]uint16{}
		copy(metrics.MessageFont.FaceName[:], windows.StringToUTF16("Inter"))
	}
	font, _, _ := procCreateFont.Call(uintptr(unsafe.Pointer(&metrics.MessageFont)))
	if font == 0 {
		return
	}
	var tm [16]int32 // TEXTMETRICW: tmHeight first, tmAveCharWidth sixth
	dc, _, _ := procGetDC.Call(a.hwnd)
	old, _, _ := procSelectObject.Call(dc, font)
	procGetTextMetrics.Call(dc, uintptr(unsafe.Pointer(&tm[0])))
	procSelectObject.Call(dc, old)
	procReleaseDC.Call(a.hwnd, dc)
	a.textH = int(tm[0])
	prev := a.font
	a.font = font
	for id := idSearch; id <= idVolumeIcon; id++ {
		if h := a.child(id); h != 0 {
			procSendMessage.Call(h, wmSetFont, font, 1)
		}
	}
	procSendMessage.Call(a.tip, wmSetFont, font, 0)
	procSetWindowPos.Call(a.edit, 0, 0, 0, 0, 0, 0x27) // NOSIZE | NOMOVE | NOZORDER | FRAMECHANGED: recenter for the new font
	bold := metrics.MessageFont
	bold.Weight = 700
	if f, _, _ := procCreateFont.Call(uintptr(unsafe.Pointer(&bold))); f != 0 {
		if a.boldFont != 0 {
			procDeleteObject.Call(a.boldFont)
		}
		a.boldFont = f
	}
	a.makeImageLists()
	if prev != 0 {
		procDeleteObject.Call(prev)
	}
	icon := metrics.MessageFont
	icon.Height = icon.Height * 4 / 3
	icon.FaceName = [32]uint16{}
	copy(icon.FaceName[:], windows.StringToUTF16("Segoe MDL2 Assets"))
	if f, _, _ := procCreateFont.Call(uintptr(unsafe.Pointer(&icon))); f != 0 {
		if a.iconFont != 0 {
			procDeleteObject.Call(a.iconFont)
		}
		a.iconFont = f
	}
}

func (a *app) button(id int) uintptr {
	return a.control("BUTTON", a.labels[id][0], wsTabStop|bsPushButton, id)
}

// layoutBoxes places every control in a client area of w x h logical pixels. Below 640
// the navigation takes two rows.
func layoutBoxes(w, h, line int) map[int]box {
	b := map[int]box{}
	margin, gap := 6, 4
	button, status, row := line+8, line+1, line+6 // 24, 17 and 22 at the default line of 16
	cols, navRows := len(navIDs), 1
	if w < 640 {
		cols, navRows = 4, 2
	}
	navW := (w - 2*margin - gap*(cols-1)) / cols
	for i, id := range navIDs {
		b[id] = box{margin + i%cols*(navW+gap), margin + i/cols*(button+gap), navW, button}
	}
	searchY := margin + navRows*(button+gap)
	buttonW := 72
	editW := w - 2*margin - buttonW - gap
	b[idSearch] = box{margin, searchY, editW, button}
	b[idSearchGo] = box{margin + editW + gap, searchY, buttonW, button}
	listY := searchY + button + gap
	statusY := h - bottomHeight(line)
	b[idList] = box{margin, listY, w - 2*margin, statusY - listY - gap}
	cover := 2*status + 2
	b[idCover] = box{margin, statusY, cover, cover}
	b[idStatus] = box{margin + cover + 6, statusY, w - 2*margin - cover - 6, status}
	b[idTrack] = box{margin + cover + 6, statusY + status + 2, w - 2*margin - cover - 6, status}
	seekY := statusY + cover
	b[idSeek] = box{margin, seekY, w - 2*margin, 14}
	rowY := seekY + 17
	cell := min(96, (w-2*margin-gap*len(bottomIDs)-22)/(len(bottomIDs)+1)) // the speaker takes 22 of the slider's share
	for i, id := range bottomIDs {
		b[id] = box{margin + i*(cell+gap), rowY, cell, row}
	}
	x := margin + len(bottomIDs)*(cell+gap)
	b[idVolumeIcon] = box{x, rowY, 20, row}
	b[idVolume] = box{x + 22, rowY, w - margin - x - 22, row}
	return b
}

// line is the UI font's line height in logical pixels, 16 by default.
func (a *app) line() int {
	if a.textH == 0 {
		return 16
	}
	return int(float64(a.textH)/a.scale + 0.5)
}

// bottomHeight is the height from the status line to the window's bottom edge: two
// status lines, the seek bar, the bottom row and the margins (80 at the default line).
func bottomHeight(line int) int { return 2*(line+1) + 2 + 17 + line + 6 + 5 }

// minHeight is the window's smallest height: 400 at the default line, and higher by what
// the rows above and below the list grow with a larger font.
func minHeight(line int) int {
	return 6 + 2*(line+8+4) + (line + 8) + 4 + 4 + 226 + bottomHeight(line)
}
func (a *app) layout(w, h int) {
	if w <= 0 || h <= 0 || a.list == 0 {
		return
	}
	boxes := layoutBoxes(w, h, a.line())
	d, _, _ := procBeginDefer.Call(uintptr(len(boxes)))
	for id, b := range boxes {
		if d == 0 {
			return
		}
		x, y, cw, ch := int(float64(b.x)*a.scale), int(float64(b.y)*a.scale), int(float64(b.w)*a.scale), int(float64(b.h)*a.scale)
		flags := uintptr(swpNoZOrder | swpNoActivate)
		if id == idSearch {
			flags |= 0x20 // SWP_FRAMECHANGED: the field recenters its line (editSubclass)
		}
		d, _, _ = procDeferWindowPos.Call(d, a.child(id), 0, uintptr(x), uintptr(y), uintptr(cw), uintptr(ch), flags)
	}
	if d != 0 {
		procEndDefer.Call(d)
	}
	a.setColumns()
}

func (a *app) child(id int) uintptr {
	h, _, _ := procGetDlgItem.Call(a.hwnd, uintptr(id))
	return h
}

// appendItem adds an item to a menu. Outside high contrast it is drawn here in the UI font
// (drawMenuItem); its text is set on the item as well, where screen readers read it.
func (a *app) appendItem(menu, flags, id uintptr, label string) {
	p, _ := syscall.UTF16PtrFromString(label)
	if a.highContrast || menu == a.menuBar {
		procAppendMenu.Call(menu, flags, id, uintptr(unsafe.Pointer(p)))
		return
	}
	a.menuItems = append(a.menuItems, label)
	a.menuSubs = append(a.menuSubs, flags&mfPopup != 0)
	procAppendMenu.Call(menu, flags|0x100, id, uintptr(menuItemBase+len(a.menuItems)-1)) // MF_OWNERDRAW
	n, _, _ := procGetMenuCount.Call(menu)
	mi := menuItemInfo{Size: uint32(unsafe.Sizeof(menuItemInfo{})), Mask: 0x40, TypeData: uintptr(unsafe.Pointer(p))} // MIIM_STRING
	procSetMenuItemInfo.Call(menu, n-1, 1, uintptr(unsafe.Pointer(&mi)))
}

func check(on bool) uintptr {
	if on {
		return mfChecked
	}
	return 0
}

// fillMenu fills a menu bar menu as it opens, so its checks show the engine's state.
func (a *app) fillMenu(menu uintptr) {
	if menu != a.menuPlayback && menu != a.menuGo && menu != a.menuApp {
		return
	}
	for n, _, _ := procGetMenuCount.Call(menu); int32(n) > 0; n-- {
		procDeleteMenu.Call(menu, 0, mfByPosition) // also destroys a submenu
	}
	a.menuItems, a.menuSubs = nil, nil // one menu is open at a time
	sep := func() { procAppendMenu.Call(menu, mfSeparator, 0, 0) }
	sub := func(label string) uintptr {
		m, _, _ := procCreatePopupMenu.Call()
		a.appendItem(menu, mfPopup, m, label)
		return m
	}
	signedIn := engine.Client() != nil
	needs := func(on bool) uintptr {
		if on {
			return 0
		}
		return mfGrayed
	}
	switch menu {
	case a.menuPlayback:
		play := "Play"
		if p := engine.Player(); p != nil && p.State() == audio.Playing {
			play = "Pause"
		}
		a.appendItem(menu, needs(signedIn), idPlayPause, i18n.T(play))
		a.appendItem(menu, needs(signedIn), idStop, i18n.T("Stop"))
		a.appendItem(menu, needs(signedIn), idPrev, i18n.T("Previous")+"\tCtrl+←")
		a.appendItem(menu, needs(signedIn), idNext, i18n.T("Next track")+"\tCtrl+→")
		sep()
		s := sub(i18n.T("Sleep timer"))
		for i, mins := range presets {
			label := i18n.Tf("%d min", mins)
			switch mins {
			case 0:
				label = i18n.T("off")
			case -1:
				label = i18n.T("End of track")
			}
			a.appendItem(s, 0, uintptr(idSleep+i), label)
		}
		sep()
		a.appendItem(menu, check(engine.Shuffle()), idShuffle, i18n.T("Shuffle"))
		r := sub(i18n.T("Repeat"))
		for i, mode := range []string{"off", "all", "one"} {
			a.appendItem(r, check(engine.Repeat() == mode), uintptr(idRepeat+i), i18n.T(mode))
		}
		cf := sub(i18n.T("Crossfade"))
		for i, ms := range fades {
			label := i18n.T("off")
			if ms > 0 {
				label = i18n.Tf("%d s", ms/1000)
			}
			a.appendItem(cf, check(engine.CrossfadeMS() == ms), uintptr(idCrossfade+i), label)
		}
		sp := sub(i18n.T("Episode speed"))
		for i, s := range speeds {
			a.appendItem(sp, check(engine.PlaybackSpeed() == s), uintptr(idSpeed+i), fmt.Sprintf("%g×", s))
		}
		sep()
		q := sub(i18n.T("Audio quality"))
		for i, label := range qualities {
			a.appendItem(q, check(engine.Quality() == i), uintptr(idQuality+i), i18n.T(label))
		}
		a.appendItem(menu, check(engine.Gapless()), idGapless, i18n.T("Gapless"))
		a.appendItem(menu, check(engine.ReplayGain()), idReplayGain, i18n.T("ReplayGain"))
		eq, ok := engine.EQ()
		a.appendItem(menu, check(eq.Mono)|needs(ok), idMono, i18n.T("Mono downmix"))
		a.appendItem(menu, check(eq.Enabled)|needs(ok), idEqualizer, i18n.T("Equalizer"))
		ps := sub(i18n.T("EQ preset"))
		for i, name := range audio.EQPresetNames {
			a.appendItem(ps, check(eq.Enabled && eq.Preset == name)|needs(ok), uintptr(idPreset+i), name)
		}
	case a.menuGo:
		a.appendItem(menu, needs(len(a.history) > 0), idBack, i18n.T("Back")+"\tAlt+←")
		sep()
		for _, id := range append(navIDs[:len(navIDs)-1:len(navIDs)-1], idQueue, idLyrics) { // New playlist is an action: App
			a.appendItem(menu, needs(signedIn), uintptr(id), strings.ReplaceAll(a.labels[id][0], "&", ""))
		}
		sep()
		a.appendItem(menu, 0, idFind, i18n.T("Search")+"\tCtrl+F")
	case a.menuApp:
		if a.release.HasUpdate {
			a.appendItem(menu, 0, idUpdate, i18n.Tf("Update available: v%s", a.release.Latest))
			sep()
		}
		a.appendItem(menu, needs(signedIn), idNewPlaylist, strings.ReplaceAll(a.labels[idNewPlaylist][0], "&", ""))
		a.appendItem(menu, needs(signedIn), idConnect, i18n.T("Connect to a device"))
		enabled, _ := engine.WebRemoteInfo()["enabled"].(bool)
		a.appendItem(menu, check(enabled)|needs(signedIn), idRemote, i18n.T("Web Remote"))
		a.appendItem(menu, check(engine.OfflineMode()), idOfflineMode, i18n.T("Offline"))
		sep()
		label := "Sign out"
		if !signedIn {
			label = "Sign in"
		}
		a.appendItem(menu, 0, idSignOut, i18n.T(label))
	}
}

var qualities = []string{"Normal (MP3 128)", "High (MP3 320)", "HiFi (FLAC)"}

// contextMenu offers the selected row's actions, at the pointer or, from the keyboard, at the row.
func (a *app) contextMenu(lp uintptr) {
	pt := point{int32(int16(lp & 0xffff)), int32(int16(lp >> 16))}
	if uint32(lp) == 0xffffffff {
		r := rect{} // Left 0: LVIR_BOUNDS
		procSendMessage.Call(a.list, lvmGetItemRect, uintptr(max(a.selected(), 0)), uintptr(unsafe.Pointer(&r)))
		pt = point{r.Left + 16, r.Bottom}
		procClientToScreen.Call(a.list, uintptr(unsafe.Pointer(&pt)))
	} else {
		c := pt
		procScreenToClient.Call(a.list, uintptr(unsafe.Pointer(&c)))
		hit := lvHitTest{Pt: c}
		if i, _, _ := procSendMessage.Call(a.list, lvmHitTest, 0, uintptr(unsafe.Pointer(&hit))); int32(i) >= 0 {
			a.selectRow(int(int32(i)))
		}
	}
	i := a.selected()
	if i < 0 || i >= len(a.rows) || engine.Client() == nil {
		return
	}
	r := a.rows[i]
	a.menuItems, a.menuSubs = nil, nil
	menu, _, _ := procCreatePopupMenu.Call()
	sep := func() { procAppendMenu.Call(menu, mfSeparator, 0, 0) }
	switch r.kind {
	case "track", "queued":
		a.appendItem(menu, 0, idPlay, i18n.T("Play"))
		a.appendItem(menu, 0, idPlayNext, i18n.T("Play next"))
		a.appendItem(menu, 0, idAddQueue, i18n.T("Enqueue"))
		a.appendItem(menu, 0, idLibrary, i18n.T("Add to playlist"))
		sep()
		like := "Like"
		if a.liked[r.track.ID] {
			like = "Unlike"
		}
		a.appendItem(menu, 0, idLike, i18n.T(like))
		sep()
		a.appendItem(menu, 0, idDownload, i18n.T("Download"))
		a.appendItem(menu, 0, idPin, i18n.T("Keep offline"))
	case "album", "playlist":
		a.appendItem(menu, 0, idOpen, i18n.T("Open"))
		sep()
		a.appendItem(menu, 0, idDownload, i18n.T("Download"))
		a.appendItem(menu, 0, idPin, i18n.T("Keep offline"))
	case "episode":
		a.appendItem(menu, 0, idPlay, i18n.T("Play"))
	case "header", "lyric", "retryLyrics", "hint":
		procDestroyMenu.Call(menu)
		return
	default:
		a.appendItem(menu, 0, idOpen, i18n.T("Open"))
	}
	picked, _, _ := procTrackPopupMenu.Call(menu, tpmReturnCommand, uintptr(pt.X), uintptr(pt.Y), 0, a.hwnd, 0)
	procDestroyMenu.Call(menu)
	if picked != 0 {
		a.command(int(picked))
	}
}

func (a *app) signedOut() bool {
	if engine.Client() != nil {
		return false
	}
	a.announce(i18n.T("Paste your ARL and sign in."))
	a.focusSearch()
	return true
}

func (a *app) command(id int) {
	switch {
	case id >= idQuality && id < idQuality+len(qualities):
		engine.SetQuality(id - idQuality)
		a.announce(i18n.T("Audio quality") + ": " + i18n.T(qualities[engine.Quality()]))
		return
	case id >= idRepeat && id < idRepeat+3:
		engine.SetRepeat(id - idRepeat)
		a.announce(i18n.Tf("Repeat: %s", i18n.T(engine.Repeat())))
		return
	case id >= idCrossfade && id < idCrossfade+len(fades):
		engine.SetCrossfadeMS(fades[id-idCrossfade])
		return
	case id >= idSpeed && id < idSpeed+len(speeds):
		engine.SetPlaybackSpeed(speeds[id-idSpeed])
		return
	case id >= idPreset && id < idPreset+len(audio.EQPresetNames):
		b, _ := json.Marshal(map[string]any{"enabled": true, "preset": audio.EQPresetNames[id-idPreset]})
		engine.SetEQJSON(string(b))
		return
	case id >= idSleep && id < idSleep+len(presets):
		a.sleep(presets[id-idSleep])
		return
	}
	switch id {
	case idSearchGo:
		a.search()
		return
	case idFind:
		a.focusSearch()
		procSendMessage.Call(a.edit, 0x00b1, 0, ^uintptr(0)) // EM_SETSEL: all
		return
	case idBack:
		a.back()
		return
	case idUpdate:
		verb, _ := syscall.UTF16PtrFromString("open")
		url, _ := syscall.UTF16PtrFromString(a.release.URL)
		procShellExecute.Call(a.hwnd, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(url)), 0, 0, 1)
		return
	case idGapless:
		engine.SetGapless(!engine.Gapless())
		return
	case idReplayGain:
		engine.SetReplayGain(!engine.ReplayGain())
		return
	case idShuffle:
		engine.SetShuffle(!engine.Shuffle())
		a.announce(i18n.T(map[bool]string{true: "Shuffle on", false: "Shuffle off"}[engine.Shuffle()]))
		return
	case idMono, idEqualizer:
		eq, _ := engine.EQ()
		if id == idMono {
			engine.SetEQJSON(fmt.Sprintf(`{"mono":%t}`, !eq.Mono))
		} else {
			engine.SetEQJSON(fmt.Sprintf(`{"enabled":%t}`, !eq.Enabled))
		}
		return
	case idOfflineMode:
		engine.SetOfflineMode(!engine.OfflineMode())
		a.announce(i18n.Tf("Offline mode: %s", onOff(engine.OfflineMode())))
		return
	case idSignOut:
		a.signInOrOut()
		return
	}
	if a.signedOut() {
		return
	}
	switch id {
	case idHome, idLiked, idPlaylists, idFlow, idCharts, idPodcasts, idRecent:
		a.lastNav = id
		switch id {
		case idHome:
			a.home()
		case idLiked:
			a.likedPage()
		case idPlaylists:
			a.playlists()
		case idFlow:
			a.flow("")
		case idCharts:
			a.charts()
		case idPodcasts:
			a.podcasts()
		default:
			a.recent()
		}
	case idNewPlaylist:
		a.newPlaylist()
	case idLibrary:
		a.choosePlaylistForTrack()
	case idQueue:
		a.showQueue()
	case idAddQueue, idPlayNext:
		a.enqueueSelected(id == idPlayNext)
	case idLike:
		a.likeSelected()
	case idPlay, idOpen:
		a.activateSelected()
	case idPrev, idNext:
		if a.episodeMode {
			if p := engine.Player(); id == idPrev && p != nil && p.PositionMS() >= 5000 {
				engine.Seek(0)
			} else if id == idPrev {
				a.skip(-1)
			} else {
				a.skip(1)
			}
			return
		}
		a.background(false, func(*deezer.Client) func() {
			if id == idNext {
				engine.Next()
			} else {
				engine.Previous()
			}
			return nil
		})
		a.setTick(500)
	case idPlayPause:
		p := engine.Player()
		if p == nil {
			return
		}
		switch p.State() {
		case audio.Playing, audio.Paused, audio.Loading:
			p.TogglePause()
			a.setTick(500)
		default: // nothing loaded: the selected row when it plays, else the queue's current track
			if i := a.selected(); i >= 0 && i < len(a.rows) && slices.Contains([]string{"track", "queued", "episode"}, a.rows[i].kind) {
				a.activateSelected()
			} else if ts, i, _ := engine.QueueSnapshot(); i >= 0 && i < len(ts) {
				a.playTrack(ts[i], false)
			}
		}
	case idStop:
		engine.Stop()
		a.announce(i18n.T("Stopped"))
		a.refreshPlayer()
	case idLyrics:
		a.lyrics()
	case idDownload:
		a.download()
	case idPin:
		a.pin()
	case idConnect:
		a.connect()
	case idRemote:
		a.remote()
	}
}

func (a *app) signInOrOut() {
	if engine.Client() == nil {
		if arl := a.query(); arl != "" {
			a.login(arl)
		} else {
			a.signedOut()
		}
		return
	}
	engine.Logout()
	a.generation++
	a.history, a.liked = nil, map[string]bool{}
	a.nowTrack, a.current, a.playingID = deezer.Track{}, "", ""
	a.playbackStatusActive = false
	a.setTrackText(i18n.T("Nothing playing."))
	a.setTitle()
	a.setNowCover("")
	_ = os.Remove(filepath.Join(configDir(), "arl.txt"))
	a.setSignedIn(false)
	a.showHint()
	a.announce(i18n.T("Signed out."))
}

// background runs work off the UI thread with the signed-in client, then the func it
// returns on the UI thread. Every command reaches it through command or activateSelected,
// whose guard keeps a signed-out click from getting here. A page load (latest) drops its
// reply when a newer page was asked for meanwhile.
func (a *app) background(latest bool, work func(c *deezer.Client) func()) {
	c := engine.Client()
	if c == nil {
		return
	}
	gen := a.generation
	if latest {
		a.generation++
		gen = a.generation
		a.setStatus(i18n.T("Loading…"))
	}
	go func() {
		done := work(c)
		a.dispatch(func() {
			if done != nil && (!latest || gen == a.generation) {
				done()
			}
		})
	}()
}

// async loads a page of rows.
func (a *app) async(work func(c *deezer.Client) ([]row, string, error)) {
	a.background(true, func(c *deezer.Client) func() {
		rows, title, err := work(c)
		return func() {
			if err != nil {
				a.announce(err.Error())
				return
			}
			a.showRows(rows, title)
		}
	})
}

func (a *app) dispatch(f func()) {
	if a.closed.Load() {
		return
	}
	select {
	case a.updates <- f:
		procPostMessage.Call(a.hwnd, wmUpdates, 0, 0)
	default:
		go func() { a.updates <- f; procPostMessage.Call(a.hwnd, wmUpdates, 0, 0) }()
	}
}

// rowCells are a row's two columns: a section header in capitals; a track marked when it
// plays (▶) or is liked (♥); then the artist, album or owner.
func rowCells(r row, playing, liked bool) (string, string) {
	if r.kind == "header" {
		return strings.ToUpper(r.title), ""
	}
	mark := ""
	if playing {
		mark = "▶ "
	}
	if liked {
		mark += "♥ "
	}
	return mark + r.title, r.meta
}

// cellText answers the list's LVN_GETDISPINFO: the text of one cell it is about to draw.
func (a *app) cellText(it *lvItem) {
	if it.Item < 0 || int(it.Item) >= len(a.rows) {
		return
	}
	r := a.rows[it.Item]
	if it.Mask&2 != 0 && it.SubItem == 0 { // LVIF_IMAGE
		it.Image = int32(a.coverIndex(rowArt(r)))
	}
	if it.Mask&1 == 0 || it.TextMax <= 0 {
		return
	}
	s, meta := rowCells(r, r.track.ID != "" && r.track.ID == a.playingID, a.liked[r.track.ID])
	if it.SubItem == 1 {
		s = meta
	}
	buf := unsafe.Slice(it.Text, it.TextMax)
	copy(buf, windows.StringToUTF16(s)) // with its NUL when it fits
	buf[len(buf)-1] = 0                 // and cut when it does not
}

// showRows shows a new page; the one it replaces is kept for Back.
func (a *app) showRows(rows []row, title string) { a.showPage(rows, title, true) }

// showPage shows rows; push keeps the page it replaces for Back.
func (a *app) showPage(rows []row, title string, push bool) {
	if push && len(a.rows) > 0 && a.page != "" {
		a.history = append(a.history, page{a.rows, a.page, a.topRow(), a.selected(), a.lyricsFor, a.lyricTimes})
		if len(a.history) > 20 {
			a.history = a.history[1:]
		}
	}
	a.rows, a.lyricsFor, a.lyricTimes, a.lyricLine = rows, "", nil, -1
	if title != "" {
		a.page = title
	}
	a.fill(0, -1)
	a.setStatus(fmt.Sprintf("%s · %d", a.page, len(rows)))
}

func (a *app) back() {
	if len(a.history) == 0 {
		return
	}
	p := a.history[len(a.history)-1]
	a.history = a.history[:len(a.history)-1]
	a.generation++ // a page still loading must not replace this one
	a.rows, a.page, a.lyricsFor, a.lyricTimes, a.lyricLine = p.rows, p.title, p.lyricsFor, p.lyricTimes, -1
	a.fill(p.top, p.sel)
	a.setStatus(fmt.Sprintf("%s · %d", a.page, len(a.rows)))
	if a.lyricsFor != "" && a.playingID != "" && a.lyricsFor != a.playingID {
		a.loadLyrics(false) // lyrics of a track that has stopped playing
	}
}

// fill shows a.rows, scrolled to top with sel selected: the list only learns the count.
func (a *app) fill(top, sel int) {
	a.twoColumns = slices.ContainsFunc(a.rows, func(r row) bool { return r.meta != "" })
	a.dropCoverRequests()
	a.showImages()
	procSendMessage.Call(a.list, lvmSetItemCount, uintptr(len(a.rows)), 0)
	a.setColumns()
	a.selectRow(sel)
	a.scrollTo(top)
	procInvalidateRect.Call(a.list, 0, 0)
}

// redrawRows repaints the visible rows, for a changed ▶ or ♥.
func (a *app) redrawRows() { procInvalidateRect.Call(a.list, 0, 0) }

// setColumns splits the list 55/45 when the page has a second column, else gives the
// first all of it (lyrics, moods).
func (a *app) setColumns() {
	var r rect
	procGetClientRect.Call(a.list, uintptr(unsafe.Pointer(&r)))
	first := r.Right
	if a.twoColumns {
		first = r.Right * 55 / 100
	}
	procSendMessage.Call(a.list, lvmSetColumnWidth, 0, uintptr(first))
	procSendMessage.Call(a.list, lvmSetColumnWidth, 1, uintptr(r.Right-first))
}

// selectRow selects and focuses row i, or clears the selection for -1.
func (a *app) selectRow(i int) {
	it := lvItem{StateMask: 3} // LVIS_FOCUSED | LVIS_SELECTED
	procSendMessage.Call(a.list, lvmSetItemState, ^uintptr(0), uintptr(unsafe.Pointer(&it)))
	if i >= 0 && i < len(a.rows) {
		it.State = 3
		procSendMessage.Call(a.list, lvmSetItemState, uintptr(i), uintptr(unsafe.Pointer(&it)))
	}
}

func (a *app) topRow() int {
	n, _, _ := procSendMessage.Call(a.list, lvmGetTopIndex, 0, 0)
	return int(int32(n))
}

// scrollTo scrolls row top to the top of the list.
func (a *app) scrollTo(top int) {
	r := rect{} // Left 0: LVIR_BOUNDS
	if ok, _, _ := procSendMessage.Call(a.list, lvmGetItemRect, 0, uintptr(unsafe.Pointer(&r))); ok != 0 {
		procSendMessage.Call(a.list, lvmScroll, 0, uintptr((top-a.topRow())*int(r.Bottom-r.Top)))
	}
}

// showHint puts the sign-in hint in the empty list.
func (a *app) showHint() {
	a.rows, a.page = []row{{kind: "hint", title: i18n.T("Paste your ARL and sign in.")}}, ""
	a.lyricsFor, a.lyricTimes, a.pendingTrack, a.queue, a.episodeMode = "", nil, nil, nil, false
	a.fill(0, -1)
}

func setText(hwnd uintptr, s string) {
	p, _ := syscall.UTF16PtrFromString(s)
	procSetWindowText.Call(hwnd, uintptr(unsafe.Pointer(p)))
}

func (a *app) setStatus(s string) { setText(a.status, s) }

// announce shows the result of something the user did, and has a screen reader read it:
// the status line is a live region (liveStatus).
func (a *app) announce(s string) {
	a.setStatus(s)
	procNotifyWinEvent.Call(0x8019, a.status, objidClient, 0) // EVENT_OBJECT_LIVEREGIONCHANGED
}

// objidClient is OBJID_CLIENT (-4) as a DWORD argument.
const objidClient = ^uintptr(3)

// liveStatus makes the status line a polite live region (UI Automation's LiveSetting,
// set through IAccPropServices::SetHwndProp as in Microsoft's Win32 sample), so a
// screen reader reads it when announce raises EVENT_OBJECT_LIVEREGIONCHANGED.
func (a *app) liveStatus() {
	clsid := windows.GUID{Data1: 0xb5f8350b, Data2: 0x0548, Data3: 0x48b1, Data4: [8]byte{0xa6, 0xee, 0x88, 0xbd, 0x00, 0xb4, 0xa5, 0xe7}}
	iid := windows.GUID{Data1: 0x6e26e776, Data2: 0x04f0, Data3: 0x495d, Data4: [8]byte{0x80, 0xe4, 0x33, 0x30, 0x35, 0x2e, 0x31, 0x69}}
	live := windows.GUID{Data1: 0xc12bcd8e, Data2: 0x2a8e, Data3: 0x4950, Data4: [8]byte{0x8a, 0xe7, 0x36, 0x25, 0x11, 0x1d, 0x58, 0xeb}}
	var svc uintptr
	if hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsid)), 0, 1, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&svc))); hr != 0 {
		return
	}
	object := *(**[1]*[7]uintptr)(unsafe.Pointer(&svc)) // a COM object starts with its method table
	setHwndProp := object[0][6]                         // after IUnknown, SetPropValue, SetPropServer, ClearProps
	if unsafe.Sizeof(uintptr(0)) == 4 {                 // 386: the GUID and the VARIANT go by value, word by word
		g := (*[4]uint32)(unsafe.Pointer(&live))
		syscall.SyscallN(setHwndProp, svc, a.status, objidClient, 0, uintptr(g[0]), uintptr(g[1]), uintptr(g[2]), uintptr(g[3]), 3, 0, 1, 0) // VARIANT{VT_I4, Polite}
	} else { // amd64: both by reference
		v := [3]uint64{3, 1, 0} // VT_I4; Polite
		syscall.SyscallN(setHwndProp, svc, a.status, objidClient, 0, uintptr(unsafe.Pointer(&live)), uintptr(unsafe.Pointer(&v)))
	}
}

func (a *app) setTrackText(s string) {
	if s != a.trackText {
		a.trackText = s
		setText(a.trackLabel, s)
	}
}

// setTitle names the playing track in the window title (taskbar, Alt+Tab).
func (a *app) setTitle() {
	if a.current == "" {
		setText(a.hwnd, titleText)
	} else {
		setText(a.hwnd, a.current+" · "+titleText)
	}
}

func (a *app) text(hwnd uintptr) string {
	n, _, _ := procGetTextLength.Call(hwnd)
	b := make([]uint16, n+1)
	procGetWindowText.Call(hwnd, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	return syscall.UTF16ToString(b)
}
func (a *app) query() string { return strings.TrimSpace(a.text(a.edit)) }
func (a *app) focusSearch()  { procSetFocus.Call(a.edit) }

func (a *app) login(arl string) {
	a.setStatus(i18n.T("Loading…"))
	go func() {
		ok := engine.Init(arl)
		a.dispatch(func() {
			if !ok {
				a.setSignedIn(false)
				reason := i18n.T("Deezer refused the sign-in.")
				switch engine.LoginErrorKind() {
				case 1:
					reason = i18n.T("The ARL has expired or is not valid.")
				case 2:
					reason = i18n.T("No internet connection")
				case 4:
					reason = i18n.T("No audio output device was found.")
				}
				a.announce(i18n.Tf("Login failed: %s", reason))
				return
			}
			_ = config.WriteFile("arl.txt", strings.TrimSpace(arl)+"\n")
			a.setSignedIn(true)
			a.announce(i18n.Tf("Logged in · %s", engine.Client().Account().Name))
			procSendMessage.Call(a.volume, tbmSetPos, 1, uintptr(engine.Volume()*100+0.5))
			a.rows = nil
			a.command(a.lastNav)
			a.setTick(5000)
			a.background(false, func(c *deezer.Client) func() {
				ids, _ := c.FavoriteIDs()
				return func() {
					for _, id := range ids {
						a.liked[id] = true
					}
					a.redrawRows()
				}
			})
		})
	}()
}

func (a *app) setSignedIn(signedIn bool) {
	setText(a.edit, "")
	mask := uintptr(0)
	if !signedIn {
		mask = '●'
	}
	procSendMessage.Call(a.edit, emSetPasswordChar, mask, 0)
	procInvalidateRect.Call(a.edit, 0, 1)
	if !signedIn {
		a.setEditCue(i18n.T("Paste your Deezer ARL"))
	} else {
		a.setEditCue(i18n.T("Search Deezer…"))
	}
}

func loadARL() string {
	if s := strings.TrimSpace(os.Getenv("DEEZER_ARL")); s != "" {
		return s
	}
	dir, err := config.Dir()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, "arl.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
func configDir() string { d, _ := config.Dir(); return d }

func playlistRows(ps []deezer.Playlist, kind string) []row {
	rs := make([]row, 0, len(ps))
	for _, p := range ps {
		rs = append(rs, row{kind: kind, id: p.ID, title: p.Name, meta: fmt.Sprint(p.TrackCount), art: p.ArtworkURL})
	}
	return rs
}

// section is a header row and its rows, or nothing when there are none.
func section(title string, rows []row) []row {
	if len(rows) == 0 {
		return nil
	}
	return append([]row{{kind: "header", title: title}}, rows...)
}

func albumRows(as []deezer.Album) []row {
	rs := make([]row, 0, len(as))
	for _, al := range as {
		rs = append(rs, row{kind: "album", id: al.ID, title: al.Name, meta: artistNames(al.Artists), art: al.ArtworkURL})
	}
	return rs
}

func artistRows(as []deezer.ArtistInfo, meta string) []row {
	rs := make([]row, 0, len(as))
	for _, ar := range as {
		rs = append(rs, row{kind: "artist", id: ar.ID, title: ar.Name, meta: meta, art: ar.ArtworkURL})
	}
	return rs
}

func (a *app) home() {
	a.async(func(c *deezer.Client) ([]row, string, error) {
		ch, err := c.Charts("0")
		if err != nil {
			return nil, i18n.T("Home"), err
		}
		ps, _ := c.Playlists()
		return append(section(i18n.T("Top tracks"), trackRows(ch.Tracks)), section(i18n.T("My Playlists"), playlistRows(ps, "playlist"))...), i18n.T("Home"), nil
	})
}
func (a *app) likedPage() {
	a.async(func(c *deezer.Client) ([]row, string, error) {
		ts, err := c.Favorites()
		return trackRows(ts), i18n.T("Liked Songs"), err
	})
}
func (a *app) playlists() {
	a.async(func(c *deezer.Client) ([]row, string, error) {
		ps, err := c.Playlists()
		return playlistRows(ps, "playlist"), i18n.T("My Playlists"), err
	})
}
func (a *app) flow(mood string) {
	a.async(func(c *deezer.Client) ([]row, string, error) {
		ts, err := c.Flow(mood)
		rows := trackRows(ts)
		if mood == "" {
			rows = append([]row{{kind: "flowMoods", title: i18n.T("Flow moods")}}, rows...)
		}
		return rows, i18n.T("Flow"), err
	})
}
func (a *app) flowMoods() {
	a.async(func(c *deezer.Client) ([]row, string, error) {
		moods, err := c.FlowMoods()
		rows := make([]row, 0, len(moods))
		for _, mood := range moods {
			rows = append(rows, row{kind: "flowMood", id: mood.ID, title: mood.Title})
		}
		return rows, i18n.T("Flow moods"), err
	})
}
func (a *app) charts() {
	a.async(func(c *deezer.Client) ([]row, string, error) {
		ch, err := c.Charts("0")
		if err != nil {
			return nil, i18n.T("Charts"), err
		}
		rs := section(i18n.T("Tracks"), trackRows(ch.Tracks))
		rs = append(rs, section(i18n.T("Albums"), albumRows(ch.Albums))...)
		return append(rs, section(i18n.T("Artists"), artistRows(ch.Artists, ""))...), i18n.T("Charts"), nil
	})
}
func (a *app) podcasts() {
	q := a.query()
	if q == "" {
		q = "podcast"
	}
	a.async(func(c *deezer.Client) ([]row, string, error) {
		ps, err := c.SearchPodcasts(q)
		rs := make([]row, 0, len(ps))
		for _, p := range ps {
			rs = append(rs, row{kind: "podcast", id: p.ID, title: p.Name, meta: fmt.Sprint(p.EpisodeCount), art: p.ArtworkURL})
		}
		return rs, i18n.T("Podcasts"), err
	})
}
func (a *app) search() {
	q := a.query()
	if q == "" {
		return
	}
	if engine.Client() == nil {
		a.login(q)
		return
	}
	a.async(func(c *deezer.Client) ([]row, string, error) {
		best := make(chan []row, 1)
		go func() { b, _ := c.BestResult(q); best <- bestRow(b) }() // beside the search
		r, err := c.Search(q)
		if err != nil {
			return nil, "", err
		}
		rs := section(i18n.T("Top result"), <-best)
		rs = append(rs, section(i18n.T("Tracks"), trackRows(r.Tracks))...)
		rs = append(rs, section(i18n.T("Albums"), albumRows(r.Albums))...)
		rs = append(rs, section(i18n.T("Artists"), artistRows(r.Artists, ""))...)
		var ps []row
		for _, p := range r.Playlists {
			ps = append(ps, row{kind: "playlist", id: p.ID, title: p.Name, meta: p.Owner})
		}
		return append(rs, section(i18n.T("Playlists"), ps)...), i18n.T("Search") + " · " + q, nil
	})
}
func (a *app) recent() {
	a.async(func(*deezer.Client) ([]row, string, error) {
		plays, err := engine.HistoryRecent(100)
		rs := make([]row, 0, len(plays))
		for _, p := range plays {
			t := deezer.Track{ID: p.TrackID, Name: p.Title, Artists: []deezer.Artist{{Name: p.Artist}}, AlbumName: p.Album, ArtworkURL: p.ArtworkURL, DurationMS: p.DurationMS}
			rs = append(rs, trackRow(t))
		}
		return rs, i18n.T("Recently played"), err
	})
}
func trackRow(t deezer.Track) row {
	return row{kind: "track", id: t.ID, title: t.Name, meta: t.ArtistLine(), track: t}
}
func trackRows(ts []deezer.Track) []row {
	rs := make([]row, 0, len(ts))
	for _, t := range ts {
		rs = append(rs, trackRow(t))
	}
	return rs
}
func artistNames(as []deezer.Artist) string {
	ns := make([]string, len(as))
	for i, x := range as {
		ns[i] = x.Name
	}
	return strings.Join(ns, ", ")
}

func (a *app) selected() int {
	n, _, _ := procSendMessage.Call(a.list, lvmGetNextItem, ^uintptr(0), 2) // LVNI_SELECTED
	return int(int32(n))
}

// selectedTrack is the selected row's track, or false with the status set.
func (a *app) selectedTrack() (deezer.Track, bool) {
	if i := a.selected(); i >= 0 && i < len(a.rows) && a.rows[i].track.ID != "" {
		return a.rows[i].track, true
	}
	a.announce(i18n.T("Select a track first."))
	return deezer.Track{}, false
}

func (a *app) activateSelected() {
	if a.signedOut() {
		return
	}
	i := a.selected()
	if i < 0 || i >= len(a.rows) {
		return
	}
	r := a.rows[i]
	switch r.kind {
	case "flowMoods":
		a.flowMoods()
	case "device":
		a.connectTo(r.id)
	case "track":
		a.playRows(i)
	case "queued":
		engine.QueueSetIndex(i)
		a.episodeMode = false
		a.playTrack(r.track, false)
	case "playlist":
		a.async(func(*deezer.Client) ([]row, string, error) {
			ts, err := engine.PlaylistTracks(r.id)
			return trackRows(ts), r.title, err
		})
	case "album":
		a.async(func(*deezer.Client) ([]row, string, error) {
			ts, err := engine.AlbumTracks(r.id)
			return trackRows(ts), r.title, err
		})
	case "artist":
		a.async(func(c *deezer.Client) ([]row, string, error) {
			p, err := c.ArtistProfile(r.id)
			if err != nil {
				return nil, r.title, err
			}
			rs := section(i18n.T("Top tracks"), trackRows(p.Top))
			rs = append(rs, section(i18n.T("Albums"), albumRows(p.Albums))...)
			return append(rs, section(i18n.T("Related artists"), artistRows(p.Related, ""))...), r.title, nil
		})
	case "podcast":
		a.async(func(c *deezer.Client) ([]row, string, error) {
			es, err := c.PodcastEpisodes(r.id)
			rs := make([]row, 0, len(es))
			for _, e := range es {
				rs = append(rs, row{kind: "episode", id: e.ID, title: e.Title, meta: e.PodcastName, track: e.AsTrack()})
			}
			return rs, r.title, err
		})
	case "episode":
		a.playEpisode(r.track)
	case "flowMood":
		a.flow(r.id)
	case "retryLyrics":
		a.loadLyrics(false)
	case "addTarget":
		if a.pendingTrack != nil {
			t := *a.pendingTrack
			a.background(false, func(c *deezer.Client) func() {
				err := c.AddToPlaylist(r.id, t.ID)
				return func() {
					if err != nil {
						a.announce(err.Error())
						return
					}
					a.pendingTrack = nil
					a.back()
					a.announce(i18n.Tf("Added to %s", r.title))
				}
			})
		}
	}
}

func (a *app) playRows(selected int) {
	var ts []deezer.Track
	index := -1
	for i, r := range a.rows {
		if r.kind == "track" {
			if i == selected {
				index = len(ts)
			}
			ts = append(ts, r.track)
		}
	}
	if index < 0 {
		return
	}
	a.episodeMode = false
	b, _ := json.Marshal(bridge.FromTracks(ts))
	_ = engine.QueueSet(string(b))
	engine.QueueSetIndex(index)
	a.playTrack(ts[index], false)
}

func (a *app) playTrack(t deezer.Track, episode bool) {
	a.playbackTitle = t.Name
	a.playbackTrackID = t.ID
	a.playbackState = audio.Loading
	a.playbackStatusActive = true
	a.setStatus(i18n.Tf("Loading: %s", t.Name))
	a.setTick(500)
	a.background(false, func(*deezer.Client) func() {
		var result engine.PlayResult
		if episode {
			result = engine.PlayEpisode(t.ID, t.DurationMS)
		} else {
			result = engine.Play(t.ID, t.DurationMS)
		}
		return func() {
			if result == engine.PlayFailed {
				a.playbackStatusActive = false
				if p := engine.Player(); p != nil {
					a.announce(p.LastError())
				}
			} else if result == engine.PlayStarted {
				a.finishedSeen = engine.FinishedCount()
				a.refreshPlayer()
			}
		}
	})
}

func (a *app) playEpisode(t deezer.Track) {
	var episodes []deezer.Track
	selected := 0
	for _, r := range a.rows {
		if r.kind == "episode" {
			if r.id == t.ID {
				selected = len(episodes)
			}
			episodes = append(episodes, r.track)
		}
	}
	if len(episodes) == 0 {
		episodes = []deezer.Track{t}
	}
	a.queue, a.queueIndex, a.episodeMode = episodes, selected, true
	_ = engine.QueueSet("[]")
	a.playTrack(t, true)
}

// skip moves through the episode list; tracks step through the engine queue.
func (a *app) skip(step int) {
	if len(a.queue) == 0 {
		return
	}
	a.queueIndex = min(max(a.queueIndex+step, 0), len(a.queue)-1)
	a.playTrack(a.queue[a.queueIndex], true)
}

// showQueue lists the engine queue; a row plays from that place in it.
func (a *app) showQueue() {
	if a.episodeMode {
		a.showRows(trackRows(a.queue), i18n.T("Queue"))
		return
	}
	ts, i, _ := engine.QueueSnapshot()
	rs := trackRows(ts)
	for j := range rs {
		rs[j].kind = "queued"
	}
	a.showRows(rs, i18n.T("Queue"))
	if i >= 0 && i < len(rs) {
		a.selectRow(i)
		procSendMessage.Call(a.list, lvmEnsureVisible, uintptr(i), 0)
	}
}

func (a *app) enqueueSelected(next bool) {
	t, ok := a.selectedTrack()
	if !ok {
		return
	}
	b, _ := json.Marshal(bridge.FromTracks([]deezer.Track{t}))
	if next {
		engine.QueueInsertNext(string(b))
		a.announce(i18n.Tf("Playing next: %s", t.Name))
	} else if err := engine.QueueAppend(string(b)); err != nil {
		a.announce(err.Error())
	} else {
		a.announce(i18n.Tf("Added to queue: %s", t.Name))
	}
}

func (a *app) choosePlaylistForTrack() {
	t, ok := a.selectedTrack()
	if !ok {
		return
	}
	a.pendingTrack = &t
	a.async(func(c *deezer.Client) ([]row, string, error) {
		ps, err := c.Playlists()
		rs := playlistRows(ps, "addTarget")
		for i := range rs {
			rs[i].meta = ""
		}
		return rs, i18n.T("Add to playlist"), err
	})
}

func (a *app) likeSelected() {
	t, ok := a.selectedTrack()
	if !ok {
		return
	}
	liked := a.liked[t.ID]
	a.background(false, func(c *deezer.Client) func() {
		var err error
		if liked {
			err = c.RemoveFavoriteTrack(t.ID)
		} else {
			err = c.AddFavoriteTrack(t.ID)
		}
		return func() {
			switch {
			case err != nil:
				a.announce(err.Error())
				return
			case liked:
				delete(a.liked, t.ID)
				a.announce(i18n.Tf("Unliked: %s", t.Name))
			default:
				a.liked[t.ID] = true
				a.announce(i18n.Tf("Liked: %s", t.Name))
			}
			a.redrawRows()
		}
	})
}

func (a *app) newPlaylist() {
	title := a.query()
	if title == "" {
		a.announce(i18n.T("Enter a playlist name."))
		a.focusSearch()
		return
	}
	a.background(false, func(c *deezer.Client) func() {
		_, err := c.CreatePlaylist(title, nil)
		return func() {
			if err != nil {
				a.announce(err.Error())
				return
			}
			a.playlists()
			a.announce(i18n.Tf("Created playlist %s", title))
		}
	})
}

func (a *app) lyrics() { a.loadLyrics(true) }

// loadLyrics shows the playing track's lyrics. While they show they follow the playing
// track (refreshPlayer), and synced lines are selected and scrolled with the song.
func (a *app) loadLyrics(push bool) {
	t := a.nowTrack
	if t.ID == "" {
		a.announce(i18n.T("Nothing playing."))
		return
	}
	title := i18n.Tf("Lyrics: %s", t.Name)
	a.showPage([]row{{kind: "lyric", title: i18n.T("(loading lyrics…)")}}, title, push)
	a.lyricsFor = t.ID
	a.background(true, func(c *deezer.Client) func() {
		l, err := c.Lyrics(t.ID)
		return func() {
			if a.lyricsFor != t.ID {
				return
			}
			if err != nil {
				pdlog.Warn("lyrics %s: %v", t.ID, err)
			}
			rows, times := lyricRows(l, err)
			a.showPage(rows, title, false)
			a.lyricsFor, a.lyricTimes = t.ID, times
			a.setStatus(title)
			a.followLyrics()
		}
	})
	a.setStatus(title)
}

// lyricRows are the lyrics page's rows and, for synced lyrics, each row's start in ms.
// Deezer answers a track without lyrics with empty lyrics, and a failure with an error:
// the page says which, and offers Retry after a failure.
func lyricRows(l *deezer.Lyrics, err error) ([]row, []int64) {
	if err != nil {
		return []row{{kind: "lyric", title: i18n.Tf("Error: %s", err)}, {kind: "retryLyrics", title: i18n.T("Retry")}}, nil
	}
	if l.IsSynced() {
		rows, times := make([]row, len(l.Synced)), make([]int64, len(l.Synced))
		for i, x := range l.Synced {
			rows[i], times[i] = row{kind: "lyric", title: x.Text}, x.TimeMS
		}
		return rows, times
	}
	if strings.TrimSpace(l.Plain) == "" {
		return []row{{kind: "lyric", title: i18n.T("(no lyrics available)")}}, nil
	}
	var rows []row
	for _, line := range strings.Split(strings.ReplaceAll(l.Plain, "\r\n", "\n"), "\n") {
		rows = append(rows, row{kind: "lyric", title: line})
	}
	return rows, nil
}

// activeLine is the synced line sung at posMS: the last one started, -1 before the first.
func activeLine(times []int64, posMS int64) int {
	i := -1
	for j, t := range times {
		if t > posMS {
			break
		}
		i = j
	}
	return i
}

// followLyrics selects the line being sung and keeps it in the middle of the list.
func (a *app) followLyrics() {
	if a.lyricsFor == "" || a.lyricsFor != a.playingID || len(a.lyricTimes) == 0 {
		return
	}
	i := activeLine(a.lyricTimes, engine.PositionMS())
	if i == a.lyricLine {
		return
	}
	a.lyricLine = i
	a.selectRow(i)
	page, _, _ := procSendMessage.Call(a.list, lvmGetCountPerPage, 0, 0)
	a.scrollTo(max(0, i-int(page)/2))
}

// sleep sets the sleep timer: minutes, 0 off, -1 at the end of the track.
func (a *app) sleep(mins int) {
	switch mins {
	case -1:
		engine.SetSleepTimer(0, true)
		a.announce(i18n.T("Sleep: at end of track"))
	case 0:
		engine.CancelSleepTimer()
		a.announce(i18n.T("Sleep timer off"))
	default:
		engine.SetSleepTimer(mins, false)
		a.announce(i18n.Tf("Sleep: pausing in %d min", mins))
	}
}

func onOff(enabled bool) string {
	if enabled {
		return i18n.T("on")
	}
	return i18n.T("off")
}

func (a *app) download() {
	i := a.selected()
	if i < 0 || i >= len(a.rows) {
		return
	}
	r := a.rows[i]
	if r.track.ID == "" && r.kind != "album" && r.kind != "playlist" {
		return
	}
	a.announce(i18n.Tf("Downloading %s…", r.title))
	a.background(false, func(*deezer.Client) func() {
		var result map[string]any
		var path string
		var err error
		switch r.kind {
		case "album":
			result, err = engine.DownloadAlbum(r.id)
		case "playlist":
			result, err = engine.DownloadPlaylist(r.id)
		default:
			path, err = engine.DownloadTrack(r.track.ID, "")
		}
		return func() {
			if detail, _ := result["error"].(string); err == nil && detail != "" {
				err = fmt.Errorf("%s", detail)
			}
			switch {
			case err != nil:
				a.announce(i18n.Tf("Download failed: %s", err))
			case result != nil:
				a.announce(i18n.Tf("Saved to %s", fmt.Sprint(result["dir"])))
			default:
				a.announce(i18n.Tf("Saved to %s", path))
			}
		}
	})
}

func (a *app) pin() {
	i := a.selected()
	if i < 0 || i >= len(a.rows) {
		return
	}
	r := a.rows[i]
	kind, id := "track", r.track.ID
	if r.kind == "album" || r.kind == "playlist" {
		kind, id = r.kind, r.id
	} else if id == "" {
		return
	}
	a.background(false, func(*deezer.Client) func() {
		err := engine.PinOffline(kind, id)
		return func() {
			if err != nil {
				a.announce(err.Error())
			} else {
				a.announce(i18n.T("Kept offline"))
			}
		}
	})
}

func (a *app) connect() {
	if addr := a.query(); addr != "" {
		a.connectTo(addr)
		return
	}
	a.async(func(*deezer.Client) ([]row, string, error) {
		devices := engine.DiscoverDevices(2 * time.Second)
		rows := make([]row, 0, len(devices))
		for _, d := range devices {
			rows = append(rows, row{kind: "device", id: d.Addr, title: d.Name, meta: d.Client + " · " + d.Addr})
		}
		if len(rows) == 0 {
			return nil, "", fmt.Errorf("%s", i18n.T("No devices found — enable PURPLEDISC_CONTROL=:7654 on the target."))
		}
		return rows, i18n.T("Connect to a device"), nil
	})
}

func (a *app) connectTo(addr string) {
	a.announce(i18n.Tf("Connecting to %s…", addr))
	a.background(false, func(*deezer.Client) func() {
		ok := engine.ConnectDevice(addr)
		return func() {
			if ok {
				a.announce(i18n.Tf("Connected to %s", engine.ConnectedDevice()))
				a.setTick(500)
			} else {
				a.announce(i18n.T("Couldn't connect."))
			}
		}
	})
}

func (a *app) remote() {
	enabled, _ := engine.WebRemoteInfo()["enabled"].(bool)
	engine.WebRemoteSetEnabled(!enabled)
	if enabled {
		a.announce(i18n.T("Web remote disabled"))
		return
	}
	info := engine.WebRemoteInfo()
	a.announce(i18n.Tf("Web Remote: %s", fmt.Sprintf("%v · %s %v", info["url"], i18n.T("Code"), info["code"])))
}

// seekMS is the position a seek bar at pos (0 to 1000) stands for.
func seekMS(pos, durMS int64) int64 { return durMS * pos / 1000 }

// seekPos is where the seek bar stands at posMS, 0 without a duration.
func seekPos(posMS, durMS int64) int64 {
	if durMS <= 0 {
		return 0
	}
	return min(max(posMS*1000/durMS, 0), 1000)
}

// timeLabel is the track label: the track and its position over its length.
func timeLabel(track string, posMS, durMS int64) string {
	return fmt.Sprintf("%s    %d:%02d / %d:%02d", track, posMS/60000, (posMS/1000)%60, durMS/60000, (durMS/1000)%60)
}

// refreshPlayer reads the engine: twice a second while something plays or loads, and
// every 5 s otherwise while signed in, so playback started from a phone or another
// device shows up; minimized, not at all.
func (a *app) refreshPlayer() {
	p := engine.Player()
	if p == nil {
		a.setTick(0)
		return
	}
	state := p.State()
	if pause := state == audio.Playing; pause != a.pauseShown {
		a.pauseShown = pause
		label := a.labels[idPlayPause][0]
		if pause {
			label = a.labels[idPlayPause][1]
		}
		setText(a.child(idPlayPause), label)
	}
	if a.episodeMode && engine.FinishedCount() != a.finishedSeen {
		a.finishedSeen = engine.FinishedCount()
		if a.queueIndex+1 < len(a.queue) {
			a.skip(1)
		} else {
			a.episodeMode = false
		}
	}
	now, playing := engine.NowPlaying()
	if playing && now.ID != a.playingID {
		a.playingID = now.ID
		a.current = now.Name + " — " + now.ArtistLine
		a.nowTrack = deezer.Track{ID: now.ID, Name: now.Name, DurationMS: now.DurationMS, Artists: []deezer.Artist{{Name: now.ArtistLine}}, AlbumName: now.AlbumName, ArtworkURL: now.ArtworkURL}
		a.setTitle()
		a.setNowCover(now.ArtworkURL)
		if a.lyricsFor != "" {
			a.loadLyrics(false) // the lyrics page follows the playing track
		} else {
			a.redrawRows()
		}
	}
	a.followLyrics()
	dur, pos := engine.DurationMS(), engine.PositionMS()
	if a.current != "" {
		a.setTrackText(timeLabel(a.current, pos, dur))
	}
	if dur > 0 && !a.seeking {
		procSendMessage.Call(a.seek, tbmSetPos, 1, uintptr(seekPos(pos, dur)))
	}
	if focus, _, _ := procGetFocus.Call(); focus != a.volume {
		if v, _, _ := procSendMessage.Call(a.volume, tbmGetPos, 0, 0); int(v) != int(engine.Volume()*100+0.5) {
			procSendMessage.Call(a.volume, tbmSetPos, 1, uintptr(engine.Volume()*100+0.5))
		}
	}
	if a.playbackStatusActive {
		if state == audio.Errored {
			a.setStatus(p.LastError())
			a.playbackStatusActive = false
		} else if playing && now.ID == a.playbackTrackID && state != a.playbackState {
			a.playbackState = state
			switch state {
			case audio.Playing:
				a.setStatus(i18n.Tf("Playing: %s", a.playbackTitle))
			case audio.Loading:
				a.setStatus(i18n.Tf("Loading: %s", a.playbackTitle))
			case audio.Paused:
				a.setStatus(i18n.T("Paused"))
			default:
				a.setStatus(i18n.T("Stopped"))
				a.playbackStatusActive = false
			}
		}
	} else if state == audio.Errored {
		a.setStatus(p.LastError())
	}
	switch {
	case state == audio.Playing || state == audio.Loading || engine.ConnectedDevice() != "":
		a.idleTicks = 0
		a.setTick(500)
	case engine.Client() == nil:
		a.setTick(0)
	default:
		if a.idleTicks++; a.idleTicks >= 2 {
			a.setTick(5000)
		}
	}
}

// ---- cover art ----
// Covers are fetched through the engine (offline mode refused, 15 s timeout) at the
// smallest CDN size that covers the drawn size, decoded and scaled here, and kept as
// thumbnails in an image list of thumbSlots (about 5 KB each at 150 %): the list view
// draws them beside the rows, the now-playing static the larger one. Rows ask for a
// cover when they are drawn; the newest request goes first, a new page drops the
// unstarted ones, and a failure leaves the placeholder until the page is shown again.

const thumbSlots = 256

// coverSizes are the square edges the Deezer image CDN serves.
var coverSizes = []int{56, 120, 250, 500, 1000}

var cdnSize = regexp.MustCompile(`/\d+x\d+-`)

// coverURL asks the CDN for the smallest size of at least px; other URLs stay as they are.
func coverURL(url string, px int) string {
	size := coverSizes[len(coverSizes)-1]
	for _, s := range coverSizes {
		if s >= px {
			size = s
			break
		}
	}
	return cdnSize.ReplaceAllLiteralString(url, fmt.Sprintf("/%dx%d-", size, size))
}

// thumbnail crops img to its centered square and averages it down (or samples it up) to
// px x px, as top-down BGRA rows for a DIB; flip mirrors it for a right-to-left window,
// which mirrors it back.
func thumbnail(img image.Image, px int, flip bool) []byte {
	b := img.Bounds()
	side := min(b.Dx(), b.Dy())
	x0, y0 := b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2
	out := make([]byte, px*px*4)
	for y := range px {
		sy0, sy1 := y0+y*side/px, max(y0+(y+1)*side/px, y0+y*side/px+1)
		for x := range px {
			sx0, sx1 := x0+x*side/px, max(x0+(x+1)*side/px, x0+x*side/px+1)
			var r, g, bl, n uint32
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					cr, cg, cb, _ := img.At(sx, sy).RGBA()
					r, g, bl, n = r+cr>>8, g+cg>>8, bl+cb>>8, n+1
				}
			}
			dx := x
			if flip {
				dx = px - 1 - x
			}
			o := (y*px + dx) * 4
			out[o], out[o+1], out[o+2], out[o+3] = byte(bl/n), byte(g/n), byte(r/n), 255
		}
	}
	return out
}

// bitmap makes a 32-bit DIB of w x h from BGRA pixels.
func bitmap(pix []byte, w, h int) uintptr {
	type header struct {
		Size                 uint32
		Width, Height        int32
		Planes, BitCount     uint16
		Compression, SizeImg uint32
		XPels, YPels         int32
		Used, Important      uint32
	}
	hd := header{Size: 40, Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32}
	var bits unsafe.Pointer
	hbm, _, _ := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&hd)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if hbm != 0 {
		copy(unsafe.Slice((*byte)(bits), len(pix)), pix)
	}
	return hbm
}

// padRight widens px x px BGRA pixels by gap transparent columns on the right: the space
// between a row's cover and its title (a right-to-left list mirrors it to the left).
func padRight(pix []byte, px, gap int) []byte {
	out := make([]byte, (px+gap)*px*4)
	for y := range px {
		copy(out[y*(px+gap)*4:], pix[y*px*4:(y+1)*px*4])
	}
	return out
}

type coverRequest struct {
	url string
	px  int
	big bool // the now-playing cover
}

// makeImageLists sizes the thumbnails for the window's DPI: 24 logical pixels beside a
// row, 36 for the now-playing cover. Index 0 of the row list is the placeholder.
func (a *app) makeImageLists() {
	px, big := int(24*a.scale+0.5), int(36*a.scale+0.5)
	if px == a.thumbPx && big == a.bigPx {
		return
	}
	for _, l := range []uintptr{a.thumbs, a.bigThumb} {
		if l != 0 {
			procImageListDestroy.Call(l)
		}
	}
	a.thumbPx, a.bigPx, a.thumbGap = px, big, int(4*a.scale+0.5)
	a.thumbs, _, _ = procImageListCreate.Call(uintptr(px+a.thumbGap), uintptr(px), 0x20, 1+thumbSlots, 0) // ILC_COLOR32, with a clear strip before the title
	a.bigThumb, _, _ = procImageListCreate.Call(uintptr(big), uintptr(big), 0x20, 1, 0)
	a.coverIdx, a.coverSlotURL, a.nextSlot, a.nowReady = map[string]int{}, map[int]string{}, 1, false
	a.placeholder()
	if a.nowArt != "" {
		a.requestCover(coverRequest{a.nowArt, a.bigPx, true})
	}
	a.showImages()
}

// placeholder paints index 0, the square shown until a cover arrives or when there is none.
func (a *app) placeholder() {
	c, _, _ := procGetSysColor.Call(15) // COLOR_BTNFACE
	if a.dark && !a.highContrast {
		c = 0x2d2d2d
	}
	pix := make([]byte, a.thumbPx*a.thumbPx*4)
	for i := 0; i < len(pix); i += 4 {
		pix[i], pix[i+1], pix[i+2], pix[i+3] = byte(c>>16), byte(c>>8), byte(c), 255
	}
	hbm := bitmap(padRight(pix, a.thumbPx, a.thumbGap), a.thumbPx+a.thumbGap, a.thumbPx)
	if n, _, _ := procImageListCount.Call(a.thumbs); n == 0 {
		procImageListAdd.Call(a.thumbs, hbm, 0)
	} else {
		procImageListReplace.Call(a.thumbs, 0, hbm, 0)
	}
	procDeleteObject.Call(hbm)
}

// showImages gives the list its thumbnails when the page has any cover, else none, so
// lyrics and moods keep their whole width.
func (a *app) showImages() {
	images := uintptr(0)
	if slices.ContainsFunc(a.rows, func(r row) bool { return rowArt(r) != "" }) {
		images = a.thumbs
	}
	procSendMessage.Call(a.list, 0x1003, 1, images) // LVM_SETIMAGELIST, LVSIL_SMALL
}

func rowArt(r row) string {
	if r.art != "" {
		return r.art
	}
	return r.track.ArtworkURL
}

// coverIndex is the image for a row's cover: the thumbnail when it is here, else the
// placeholder, asking for the cover once.
func (a *app) coverIndex(url string) int {
	if url == "" {
		return 0
	}
	if i, ok := a.coverIdx[url]; ok {
		return i
	}
	if !a.coverWant[url] && !a.coverFail[url] {
		a.coverWant[url] = true
		a.requestCover(coverRequest{url, a.thumbPx, false})
	}
	return 0
}

func (a *app) requestCover(r coverRequest) {
	a.coverMu.Lock()
	a.coverQueue = append(a.coverQueue, r)
	a.coverMu.Unlock()
	a.coverCond.Signal()
}

// dropCoverRequests forgets the requests no worker has started, for a new page.
func (a *app) dropCoverRequests() {
	a.coverMu.Lock()
	dropped := a.coverQueue
	a.coverQueue = nil
	a.coverMu.Unlock()
	for _, r := range dropped {
		if !r.big {
			delete(a.coverWant, r.url)
		}
	}
	clear(a.coverFail)
}

// coverWorker fetches and decodes covers, newest request first; each request wakes one
// worker (requestCover).
func (a *app) coverWorker() {
	for {
		a.coverMu.Lock()
		for len(a.coverQueue) == 0 {
			a.coverCond.Wait()
		}
		n := len(a.coverQueue)
		r := a.coverQueue[n-1]
		a.coverQueue = a.coverQueue[:n-1]
		a.coverMu.Unlock()
		var pix []byte
		if b, err := engine.Fetch(coverURL(r.url, r.px)); err == nil {
			if img, _, err := image.Decode(bytes.NewReader(b)); err == nil {
				pix = thumbnail(img, r.px, i18n.Locale() == "ar")
			}
		}
		a.dispatch(func() { a.coverDone(r, pix) })
	}
}

// coverDone puts a fetched cover in its image list (or notes the failure) and repaints.
func (a *app) coverDone(r coverRequest, pix []byte) {
	if r.big {
		if r.url == a.nowArt && r.px == a.bigPx && pix != nil {
			hbm := bitmap(pix, r.px, r.px)
			if n, _, _ := procImageListCount.Call(a.bigThumb); n == 0 {
				procImageListAdd.Call(a.bigThumb, hbm, 0)
			} else {
				procImageListReplace.Call(a.bigThumb, 0, hbm, 0)
			}
			procDeleteObject.Call(hbm)
			a.nowReady = true
			procInvalidateRect.Call(a.child(idCover), 0, 0)
		}
		return
	}
	delete(a.coverWant, r.url)
	if r.px != a.thumbPx {
		return // the DPI changed meanwhile
	}
	if pix == nil {
		a.coverFail[r.url] = true
		return
	}
	slot := a.nextSlot
	a.nextSlot = slot%thumbSlots + 1
	hbm := bitmap(padRight(pix, r.px, a.thumbGap), r.px+a.thumbGap, r.px)
	if old, used := a.coverSlotURL[slot]; used {
		delete(a.coverIdx, old)
		procImageListReplace.Call(a.thumbs, uintptr(slot), hbm, 0)
	} else {
		procImageListAdd.Call(a.thumbs, hbm, 0)
	}
	procDeleteObject.Call(hbm)
	a.coverSlotURL[slot], a.coverIdx[r.url] = r.url, slot
	a.redrawRows()
}

// setNowCover shows the playing track's cover beside the status lines.
func (a *app) setNowCover(url string) {
	if url == a.nowArt {
		return
	}
	a.nowArt, a.nowReady = url, false
	procInvalidateRect.Call(a.child(idCover), 0, 0)
	if url != "" {
		a.requestCover(coverRequest{url, a.bigPx, true})
	}
}

// drawCover paints the now-playing cover, or a note on the placeholder color.
func (a *app) drawCover(d *drawItem) {
	if a.nowReady {
		procImageListDraw.Call(a.bigThumb, 0, d.DC, uintptr(d.Rect.Left), uintptr(d.Rect.Top), 0)
		return
	}
	c, _, _ := procGetSysColor.Call(15)
	text, _, _ := procGetSysColor.Call(8)
	if a.dark && !a.highContrast {
		c, text = 0x2d2d2d, 0xb0b0b0
	}
	brush, _, _ := procCreateBrush.Call(c)
	procFillRect.Call(d.DC, uintptr(unsafe.Pointer(&d.Rect)), brush)
	procDeleteObject.Call(brush)
	old, _, _ := procSelectObject.Call(d.DC, a.iconFont)
	procSetBkMode.Call(d.DC, 1)
	procSetTextColor.Call(d.DC, text)
	glyph, _ := syscall.UTF16PtrFromString("") // MusicNote
	procDrawText.Call(d.DC, uintptr(unsafe.Pointer(glyph)), 1, uintptr(unsafe.Pointer(&d.Rect)), 0x25)
	procSelectObject.Call(d.DC, old)
}

// customDraw sets section headers in bold on a faint band, with a rule above each one
// after the first.
func (a *app) customDraw(cd *customDraw) uintptr {
	header := func() bool { i := int(cd.ItemSpec); return i < len(a.rows) && a.rows[i].kind == "header" }
	switch cd.Stage {
	case 0x1: // CDDS_PREPAINT
		return 0x20 // CDRF_NOTIFYITEMDRAW
	case 0x10001: // CDDS_ITEMPREPAINT
		if header() {
			procSelectObject.Call(cd.DC, a.boldFont)
			if !a.highContrast {
				band, _, _ := procGetSysColor.Call(15) // COLOR_BTNFACE
				if a.dark {
					band = 0x2b2b2b
				}
				cd.TextBack = uint32(band)
			}
			return 0x2 | 0x10 // CDRF_NEWFONT | CDRF_NOTIFYPOSTPAINT
		}
	case 0x10002: // CDDS_ITEMPOSTPAINT
		if header() && cd.ItemSpec > 0 {
			c, _, _ := procGetSysColor.Call(16) // COLOR_BTNSHADOW
			if a.dark && !a.highContrast {
				c = 0x5a5a5a
			}
			brush, _, _ := procCreateBrush.Call(c)
			line := rect{cd.Rect.Left, cd.Rect.Top, cd.Rect.Right, cd.Rect.Top + int32(max(1, a.scale))}
			procFillRect.Call(cd.DC, uintptr(unsafe.Pointer(&line)), brush)
			procDeleteObject.Call(brush)
		}
	}
	return 0
}

// editSubclass centers the field's line vertically at the buttons' height. A
// single-line edit draws its text at the top of its client area, so the area is
// narrowed to the text's height (WM_NCCALCSIZE), and the bands above and below are
// painted as the field (WM_NCPAINT) and take clicks as the field (WM_NCHITTEST).
func (a *app) editSubclass(h uintptr, m uint32, wp, lp uintptr) uintptr {
	if m == 0x0085 { // WM_NCPAINT: everything outside the line is drawn here, framed at the field's edge
		var w, c rect
		procGetWindowRect.Call(h, uintptr(unsafe.Pointer(&w)))
		procGetClientRect.Call(h, uintptr(unsafe.Pointer(&c)))
		procMapWindowPoints.Call(h, 0, uintptr(unsafe.Pointer(&c)), 2)
		W, H := w.Right-w.Left, w.Bottom-w.Top
		c = rect{c.Left - w.Left, c.Top - w.Top, c.Right - w.Left, c.Bottom - w.Top}
		dc, _, _ := procGetWindowDC.Call(h)
		for _, band := range []rect{{0, 0, W, c.Top}, {0, c.Bottom, W, H}, {0, c.Top, c.Left, c.Bottom}, {c.Right, c.Top, W, c.Bottom}} {
			procFillRect.Call(dc, uintptr(unsafe.Pointer(&band)), a.backgroundBrush)
		}
		frame, _, _ := procGetSysColor.Call(6) // COLOR_WINDOWFRAME, for high contrast
		if !a.highContrast {
			frame = 0x646464 // the themed field's border
			if a.dark {
				frame = 0x6e6e6e
			}
		}
		// Rounded as the buttons beside it (4 logical pixels); the corners outside are the window's.
		pen, _, _ := procCreatePen.Call(0, 1, frame)
		oldPen, _, _ := procSelectObject.Call(dc, pen)
		nullBrush, _, _ := procGetStockObject.Call(5) // NULL_BRUSH
		oldBrush, _, _ := procSelectObject.Call(dc, nullBrush)
		d := uintptr(8*a.scale + 0.5)
		procRoundRect.Call(dc, 0, 0, uintptr(W), uintptr(H), d, d)
		procSelectObject.Call(dc, oldBrush)
		procSelectObject.Call(dc, oldPen)
		procDeleteObject.Call(pen)
		procReleaseDC.Call(h, dc)
		return 0
	}
	r, _, _ := procDefSubclass.Call(h, uintptr(m), wp, lp)
	switch m {
	case 0x0083: // WM_NCCALCSIZE: the first rectangle is the client area either way
		c := *(**rect)(unsafe.Pointer(&lp))
		frame := int32(max(1, a.scale))
		inset := frame + int32(5*a.scale+0.5) // the text's margin from the frame
		c.Left, c.Right = c.Left+inset, c.Right-inset
		if pad := (int(c.Bottom-c.Top) - a.textH) / 2; a.textH > 0 && pad > int(frame) {
			c.Top, c.Bottom = c.Top+int32(pad), c.Bottom-int32(pad)
		} else {
			c.Top, c.Bottom = c.Top+frame, c.Bottom-frame
		}
	case 0x0084: // WM_NCHITTEST
		if r == 0 || r == 18 { // HTNOWHERE, HTBORDER
			return 1 // HTCLIENT
		}
	}
	return r
}

// bestRow is Deezer's top result for a search as a row of its kind, or none.
func bestRow(b *deezer.SearchHistoryItem) []row {
	if b == nil || b.ID == "" {
		return nil
	}
	kind := strings.ToLower(b.Type)
	word := map[string]string{"track": "Track", "album": "Album", "artist": "Artist", "playlist": "playlist", "podcast": "podcast"}[kind] // the keys that exist
	r := row{kind: kind, id: b.ID, title: b.Title, art: b.Picture, meta: capFirst(i18n.T(word))}
	if b.Subtitle != "" {
		r.meta += " · " + b.Subtitle
	}
	switch kind {
	case "track":
		r.track = deezer.Track{ID: b.ID, Name: b.Title, Artists: []deezer.Artist{{Name: b.Subtitle}}, ArtworkURL: b.Picture}
	case "album", "artist", "playlist", "podcast":
	default:
		return nil
	}
	return []row{r}
}

// menuItemBase marks a menu item's data as an index into a.menuItems; below it is a
// menu bar title's index.
const menuItemBase = 1000

// menuColors are the menus' background, highlight, text and disabled text.
func (a *app) menuColors() [4]uintptr {
	if a.dark {
		return [4]uintptr{0x2b2b2b, 0x404040, 0xf0f0f0, 0x7a7a7a}
	}
	return [4]uintptr{0xf9f9f9, 0xe5e5e5, 0x1a1a1a, 0x9a9a9a}
}

// menuLabel splits item i into its text and its shortcut (after a tab).
func (a *app) menuLabel(i uintptr) (string, string) {
	if i < menuItemBase || int(i-menuItemBase) >= len(a.menuItems) {
		return "", ""
	}
	text, keys, _ := strings.Cut(a.menuItems[i-menuItemBase], "\t")
	return text, keys
}

// textWidth is s's width in the UI font.
func (a *app) textWidth(s string) int32 {
	if s == "" {
		return 0
	}
	dc, _, _ := procGetDC.Call(a.hwnd)
	old, _, _ := procSelectObject.Call(dc, a.font)
	p, _ := syscall.UTF16PtrFromString(s)
	var r rect
	procDrawText.Call(dc, uintptr(unsafe.Pointer(p)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), 0x420) // DT_CALCRECT | DT_SINGLELINE
	procSelectObject.Call(dc, old)
	procReleaseDC.Call(a.hwnd, dc)
	return r.Right
}

// measureMenuItem sizes an item: a gutter for its check, its text, its shortcut, room for
// a submenu's arrow.
func (a *app) measureMenuItem(m *measureItem) {
	text, keys := a.menuLabel(m.ItemData)
	gutter, gap := int32(28*a.scale), int32(24*a.scale)
	w := gutter + a.textWidth(text) + gap
	if keys != "" {
		w += a.textWidth(keys) + gap
	}
	m.ItemWidth, m.ItemHeight = uint32(w), uint32(a.textH+int(10*a.scale))
}

// drawMenuItem draws an item in the UI font: highlighted when selected, grey when
// disabled, a check in the gutter, the shortcut at the right.
func (a *app) drawMenuItem(d *drawItem) {
	text, keys := a.menuLabel(d.ItemData)
	c := a.menuColors()
	back, fore := c[0], c[2]
	if d.ItemState&0x1 != 0 { // ODS_SELECTED
		back = c[1]
	}
	if d.ItemState&(0x2|0x4) != 0 { // ODS_GRAYED, ODS_DISABLED
		fore = c[3]
	}
	brush, _, _ := procCreateBrush.Call(back)
	procFillRect.Call(d.DC, uintptr(unsafe.Pointer(&d.Rect)), brush)
	procDeleteObject.Call(brush)
	procSetBkMode.Call(d.DC, 1)
	procSetTextColor.Call(d.DC, fore)
	gutter, gap := int32(28*a.scale), int32(24*a.scale)
	if d.ItemState&0x8 != 0 { // ODS_CHECKED
		old, _, _ := procSelectObject.Call(d.DC, a.iconFont)
		mark, _ := syscall.UTF16PtrFromString("") // CheckMark
		r := rect{d.Rect.Left, d.Rect.Top, d.Rect.Left + gutter, d.Rect.Bottom}
		procDrawText.Call(d.DC, uintptr(unsafe.Pointer(mark)), 1, uintptr(unsafe.Pointer(&r)), 0x25)
		procSelectObject.Call(d.DC, old)
	}
	old, _, _ := procSelectObject.Call(d.DC, a.font)
	flags := uintptr(0x824) // DT_VCENTER | DT_SINGLELINE | DT_NOPREFIX: items have no mnemonics, so an & is shown
	r := rect{d.Rect.Left + gutter, d.Rect.Top, d.Rect.Right - gap, d.Rect.Bottom}
	p, _ := syscall.UTF16PtrFromString(text)
	procDrawText.Call(d.DC, uintptr(unsafe.Pointer(p)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), flags)
	if keys != "" {
		k, _ := syscall.UTF16PtrFromString(keys)
		procDrawText.Call(d.DC, uintptr(unsafe.Pointer(k)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), flags|0x2) // DT_RIGHT
	}
	procSelectObject.Call(d.DC, old)
	if i := int(d.ItemData - menuItemBase); i < len(a.menuSubs) && a.menuSubs[i] {
		// A submenu's arrow in the item's color; Windows would draw its own after this,
		// in its dark default, so the strip is clipped from it.
		arrow := rect{d.Rect.Right - gap, d.Rect.Top, d.Rect.Right, d.Rect.Bottom}
		procSelectObject.Call(d.DC, a.font)
		chevron, _ := syscall.UTF16PtrFromString("›")
		procDrawText.Call(d.DC, uintptr(unsafe.Pointer(chevron)), 1, uintptr(unsafe.Pointer(&arrow)), 0x25) // DT_CENTER | DT_VCENTER | DT_SINGLELINE
		procSelectObject.Call(d.DC, old)
		procExcludeClipRect.Call(d.DC, uintptr(arrow.Left), uintptr(arrow.Top), uintptr(arrow.Right), uintptr(arrow.Bottom))
	}
}
