//go:build windows

package main

import (
	"errors"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
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

// 把指定窗口强行拉到前台。Windows 对 SetForegroundWindow 有严格限制
// （只有当前持有前台锁的进程才能直接抢前台），所以这里组合用：
//   1) AttachThreadInput 借用当前前台线程的输入队列 →
//      SetForegroundWindow / BringWindowToTop / SetActiveWindow 才会真正生效
//   2) SetWindowPos(HWND_TOPMOST) → SetWindowPos(HWND_NOTOPMOST) 把 Z 序顶到最上
//   3) ShowWindow(SW_RESTORE) 把可能被最小化的窗口还原
//   4) FlashWindowEx 作为兜底：即便抢不到前台，任务栏也会闪烁吸引用户注意
//
// 这是常见的「在安装/异步操作之后弹回前台」的 hack，比单纯 SetForegroundWindow 可靠。
const (
	hwndTopmost     = ^uintptr(0)     // (HWND)-1
	hwndNotopmost   = ^uintptr(0) - 1 // (HWND)-2
	swpNoMove       = 0x0002
	swpNoSize       = 0x0001
	swpShowWindow   = 0x0040
	swRestore       = 9
	swShow          = 5
	flashwAll       = 0x00000003
	flashwTimernofg = 0x0000000C
)

type flashwinfo struct {
	Size    uint32
	Hwnd    uintptr
	Flags   uint32
	Count   uint32
	Timeout uint32
}

func forceWindowToFront(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	user32 := syscall.NewLazyDLL("user32.dll")
	kernel32 := syscall.NewLazyDLL("kernel32.dll")

	getForegroundWindow := user32.NewProc("GetForegroundWindow")
	getWindowThreadProcessId := user32.NewProc("GetWindowThreadProcessId")
	attachThreadInput := user32.NewProc("AttachThreadInput")
	setForegroundWindow := user32.NewProc("SetForegroundWindow")
	bringWindowToTop := user32.NewProc("BringWindowToTop")
	setActiveWindow := user32.NewProc("SetActiveWindow")
	setWindowPos := user32.NewProc("SetWindowPos")
	showWindow := user32.NewProc("ShowWindow")
	flashWindowEx := user32.NewProc("FlashWindowEx")
	getCurrentThreadId := kernel32.NewProc("GetCurrentThreadId")

	fg, _, _ := getForegroundWindow.Call()
	var fgThread uintptr
	if fg != 0 {
		fgThread, _, _ = getWindowThreadProcessId.Call(fg, 0)
	}
	curThread, _, _ := getCurrentThreadId.Call()

	attached := false
	if fgThread != 0 && fgThread != curThread {
		ok, _, _ := attachThreadInput.Call(curThread, fgThread, 1)
		attached = ok != 0
	}
	defer func() {
		if attached {
			attachThreadInput.Call(curThread, fgThread, 0)
		}
	}()

	// 先恢复 + 顶到 Z 序最上
	showWindow.Call(hwnd, uintptr(swRestore))
	setWindowPos.Call(hwnd, hwndTopmost, 0, 0, 0, 0, uintptr(swpNoMove|swpNoSize|swpShowWindow))
	setWindowPos.Call(hwnd, hwndNotopmost, 0, 0, 0, 0, uintptr(swpNoMove|swpNoSize|swpShowWindow))
	bringWindowToTop.Call(hwnd)
	setForegroundWindow.Call(hwnd)
	setActiveWindow.Call(hwnd)

	// 兜底：哪怕前台抢不到，任务栏也闪一下提醒
	fi := flashwinfo{
		Hwnd:    hwnd,
		Flags:   flashwAll | flashwTimernofg,
		Count:   3,
		Timeout: 0,
	}
	fi.Size = uint32(unsafe.Sizeof(fi))
	flashWindowEx.Call(uintptr(unsafe.Pointer(&fi)))
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

		// Navigate 后立即把窗口拽到前台。在「安装完成→关旧窗→开新窗」这种
		// 异步串联流程里，新进程往往拿不到前台焦点，AutoFocus 不够用；
		// 不主动 SetForegroundWindow + 任务栏闪烁，用户根本注意不到新窗口已出现。
		if hwnd != 0 {
			localHwnd := hwnd
			// 第一次：立刻试
			go forceWindowToFront(localHwnd)
			// 第二次：300ms 后再试一次（让 WebView2 完成首屏渲染，避免被它自己的初始化抢回去）
			go func() {
				time.Sleep(300 * time.Millisecond)
				forceWindowToFront(localHwnd)
			}()
		}

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
