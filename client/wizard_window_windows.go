//go:build windows

package main

import (
	"errors"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
)

// DWM 自定义标题栏：Win11 (build 22000+) 支持 caption 颜色 / 文字颜色 / 边框颜色，
// Win10 1809+ 支持深色模式。失败静默忽略——降级到系统默认外观。
const (
	dwmwaUseImmersiveDarkMode = 20
	dwmwaCaptionColor         = 35
	dwmwaTextColor            = 36
	dwmwaBorderColor          = 34
)

// COLORREF (0x00BBGGRR) for the title bar — matches #0b1220 panel background.
const (
	captionColorRef = 0x00200B0B // BGR of #0b1220 ≈ 0B 12 20 → 0x00200B0B
	textColorRef    = 0x00E8E2E8 // BGR of #e2e8f0 (light slate)
	borderColorRef  = 0x003A2A1F // BGR of #1f2a3a
)

func applyDarkCaption(hwnd uintptr) {
	dwmapi := syscall.NewLazyDLL("dwmapi.dll")
	setAttr := dwmapi.NewProc("DwmSetWindowAttribute")
	set := func(attr uint32, value uint32) {
		v := value
		_, _, _ = setAttr.Call(hwnd, uintptr(attr), uintptr(unsafe.Pointer(&v)), unsafe.Sizeof(v))
	}
	set(dwmwaUseImmersiveDarkMode, 1)
	set(dwmwaCaptionColor, captionColorRef)
	set(dwmwaTextColor, textColorRef)
	set(dwmwaBorderColor, borderColorRef)
}

// postWMClose 通过 user32!PostMessageW 给指定窗口投递 WM_CLOSE，触发标准关闭流程。
// PostMessage 是异步且线程安全的，可以从任意 goroutine 调用，比 SendMessage 安全。
const wmClose = 0x0010

func postWMClose(hwnd uintptr) {
	user32 := syscall.NewLazyDLL("user32.dll")
	post := user32.NewProc("PostMessageW")
	_, _, _ = post.Call(hwnd, uintptr(wmClose), 0, 0)
}

// openWizardWindow opens the given URL inside an embedded WebView2 window.
// It runs the window on a dedicated OS-locked goroutine and returns a channel
// that closes when the window is dismissed by the user. Callers that don't
// care about close events can ignore the channel.
//
// Returns an error if WebView2 is unavailable (runtime missing, creation
// failed). On error, callers should fall back to openBrowser.
//
// closeOnRequest, if non-nil, receives a callback that programmatically
// closes the window (used by setup flow to dismiss the wizard once
// configuration has been saved).
func openWizardWindow(url, title string, closeOnRequest *func()) (<-chan struct{}, error) {
	if strings.TrimSpace(url) == "" {
		return nil, errors.New("openWizardWindow: empty url")
	}

	done := make(chan struct{})
	ready := make(chan error, 1)
	var wOnce sync.Once

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(done)

		var w webview2.WebView
		defer func() {
			if r := recover(); r != nil {
				wOnce.Do(func() { ready <- errors.New("webview2 panic") })
			}
			if w != nil {
				w.Destroy()
			}
		}()

		w = webview2.NewWithOptions(webview2.WebViewOptions{
			Debug:     false,
			AutoFocus: true,
			WindowOptions: webview2.WindowOptions{
				Title:  title,
				Width:  1440,
				Height: 920,
				IconId: 2,
				Center: true,
			},
		})
		if w == nil {
			wOnce.Do(func() { ready <- errors.New("WebView2 runtime not available") })
			return
		}
		w.SetSize(1440, 920, webview2.HintNone)

		// Apply dark caption color BEFORE Navigate to avoid a white flash.
		var hwnd uintptr
		if h := w.Window(); h != nil {
			hwnd = uintptr(h)
			applyDarkCaption(hwnd)
		}

		w.Navigate(url)

		if closeOnRequest != nil {
			localW := w
			localHwnd := hwnd
			*closeOnRequest = func() {
				// 双保险：先 PostMessage(WM_CLOSE) 触发标准关闭流程
				// （走 WindowProc，能正确销毁子 WebView2 控件），失败再 Terminate。
				// 单纯 Terminate() 在 Win11 + 新版 WebView2 上有时只退出主循环但
				// 不销毁宿主窗口，造成「卡死无响应的旧窗口」。
				if localHwnd != 0 {
					postWMClose(localHwnd)
				}
				// 同时排队一次 Terminate 作为兜底（如果 WM_CLOSE 被某个处理器吞掉）
				localW.Dispatch(func() { localW.Terminate() })
			}
		}

		wOnce.Do(func() { ready <- nil })
		w.Run()
	}()

	if err := <-ready; err != nil {
		// Drain done so the goroutine can finish even though we're returning early.
		go func() { <-done }()
		return nil, err
	}
	return done, nil
}
