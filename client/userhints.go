package main

import (
	"fmt"
	"runtime"
)

// userFacingSetupHint 引导最终用户无需手敲命令（Windows 优先 .bat）。
func userFacingSetupHint() string {
	if runtime.GOOS == "windows" {
		return "请双击「开始使用.bat」或「重新配置.bat」"
	}
	return "请首次运行本程序（不带参数）以打开配置向导"
}

func userFacingManagedLaunchPhrase() string {
	if runtime.GOOS == "windows" {
		return "「开始使用.bat」或「启动-VSCode监控.bat」等脚本"
	}
	return "--launch / --launch-preset"
}

func userFacingPreferredLaunchLine() string {
	if runtime.GOOS == "windows" {
		return "双击「开始使用.bat」或其中的「启动-VSCode/Cursor」等监控脚本，只影响对应程序，不改本机网络。"
	}
	return "使用 --launch 定向启动目标应用，只影响该程序，不改本机网络。"
}

func userFacingLaunchMissingTarget() string {
	if runtime.GOOS == "windows" {
		return "请双击「开始使用.bat」选择受管启动项，或「启动-VSCode监控.bat」等"
	}
	return "请使用 --launch <程序> 或 --launch-preset vscode"
}

func userFacingUnknownPresetHint() string {
	if runtime.GOOS == "windows" {
		return "请双击「开始使用.bat」或「启动-VSCode监控.bat」等选用预设"
	}
	return "可先执行 --list-launch-presets 查看可用名称"
}

func userFacingUninstallHint() string {
	if runtime.GOOS == "windows" {
		return "请双击「卸载.bat」"
	}
	return fmt.Sprintf("请运行 %s --uninstall", selfBinaryName())
}

func userFacingGlobalUninstallHint() string {
	if runtime.GOOS == "windows" {
		return "请双击「全局卸载.bat」"
	}
	return fmt.Sprintf("请运行 %s --global-uninstall", selfBinaryName())
}

func userFacingGlobalInstallRetryHint() string {
	if runtime.GOOS == "windows" {
		return "请再次双击「全局安装.bat」"
	}
	return fmt.Sprintf("请再次运行 %s --global-install", selfBinaryName())
}

func userFacingInstallFullRetryHint() string {
	if runtime.GOOS == "windows" {
		return "请再次双击「快速安装-系统代理.bat」或配置后双击「安装.bat」"
	}
	return fmt.Sprintf("请再次运行 %s --install-full", selfBinaryName())
}

func userFacingInstallFullAfterManualCA() string {
	if runtime.GOOS == "windows" {
		return "请先按上面的提示手动安装 CA，再双击「快速安装-系统代理.bat」（或改 config 后双击「安装.bat」）。"
	}
	return fmt.Sprintf("请先按上面的提示手动安装 CA，再重新执行 %s --install-full。", selfBinaryName())
}

func userFacingRecommendInstallProxyUsage() string {
	if runtime.GOOS == "windows" {
		return "    — 推荐用法: 双击「开始使用.bat」选用受管启动项，仅对子进程注入 HTTP(S)_PROXY 与 Base URL。"
	}
	return fmt.Sprintf("    — 推荐用法: %s --launch <你的程序>，仅对子进程注入 HTTP(S)_PROXY 与 Base URL。", selfBinaryName())
}

func userFacingAfterCAFallbackLaunch() string {
	if runtime.GOOS == "windows" {
		return "临时试用可双击「开始使用.bat」内的受管启动项，不依赖全局代理。"
	}
	return fmt.Sprintf("临时试用: %s --launch <程序> 不依赖全局代理。", selfBinaryName())
}

func userFacingRunMonitorOrLaunchHint() string {
	if runtime.GOOS == "windows" {
		return "双击「启动.bat」启动监控，或「开始使用.bat」选用受管启动项。"
	}
	return fmt.Sprintf("运行 %s 启动监控，或用 --launch 定向启动目标应用。", selfBinaryName())
}

func userFacingRunMonitorHint() string {
	if runtime.GOOS == "windows" {
		return "双击「启动.bat」启动监控。"
	}
	return fmt.Sprintf("运行 %s 启动监控。", selfBinaryName())
}

func userFacingManualStartAfterGlobalInstall() string {
	if runtime.GOOS == "windows" {
		return "请手动双击「启动.bat」或「开始使用.bat」"
	}
	return fmt.Sprintf("请手动运行 %s", selfBinaryName())
}

func userFacingIDEProxyReinstallHint() string {
	if runtime.GOOS == "windows" {
		return "可在 config.json 设 \"install_ide_proxy\": true 后重新双击「安装.bat」"
	}
	return "可在 config.json 设 \"install_ide_proxy\": true 后重新执行 --install"
}

// printMonitorModeHints 在监控运行时打印模式说明（避免 main 内变量名 runtime 遮蔽标准库）。
func printMonitorModeHints() {
	if runtime.GOOS == "windows" {
		fmt.Println("  说明: 默认不修改系统代理；推荐双击「开始使用.bat」内的受管启动项。")
		fmt.Println("        若必须接管整机代理，请使用「快速安装-系统代理.bat」或配置后双击「安装.bat」。")
		return
	}
	fmt.Println("  说明: 默认不修改系统代理；推荐用 `--launch <程序>` 仅对子进程注入代理环境变量。")
	fmt.Println("        若必须接管整机代理，请显式启用 install_system_proxy=true 或使用 --install-full（legacy 模式）。")
}
