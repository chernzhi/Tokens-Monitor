import * as vscode from 'vscode';
import * as path from 'path';
import * as fs from 'fs';
import * as http from 'http';
import * as net from 'net';
import * as tls from 'tls';
import { spawn, ChildProcess, execFileSync } from 'child_process';
import { MonitorConfig } from './config';

export type ProxyStatus = 'external' | 'internal' | 'off' | 'blocked';

export type ProxyEnvironmentKind =
    | 'direct'
    | 'configured-upstream'
    | 'vscode-http-proxy'
    | 'previous-vscode-http-proxy'
    | 'system-proxy'
    | 'system-pac'
    | 'env-proxy'
    | 'desktop-proxy'
    | 'tun';

export interface ProxyEnvironmentSignals {
    configuredUpstream?: string;
    vscodeProxy?: string;
    previousProxy?: string;
    systemProxy?: string;
    systemPacUrl?: string;
    envProxy?: string;
    localDiscoveredProxy?: string;
    desktopProcesses?: string[];
    tunAdapters?: string[];
}

interface WindowsNetworkAdapterSnapshot {
    Name?: string;
    InterfaceDescription?: string;
    Status?: string;
}

export interface ProxyEnvironmentDiagnosis {
    kind: ProxyEnvironmentKind;
    level: 'ok' | 'warning' | 'neutral';
    allowTakeover: boolean;
    summary: string;
    detail: string;
    recommendedAction?: string;
    upstreamProxy?: string;
    upstreamSource?: 'config' | 'vscode' | 'previous' | 'system' | 'env' | 'local-discovery';
    detectedDesktopProcesses: string[];
    detectedTunAdapters: string[];
    checkedAt: string;
}

const KNOWN_DESKTOP_PROXY_PROCESS_NAMES = new Set([
    'proxifier.exe',
    'sing-box.exe',
    'clash.exe',
    'clash-verge.exe',
    'clash-verge-service.exe',
    'nekoray.exe',
    'v2rayn.exe',
    'surge.exe',
    'mihomo.exe',
    'xray.exe',
    'hysteria.exe',
]);

const DESKTOP_PROXY_PROCESS_KEYWORDS = [
    'proxy',
    'sock',
    'sing-box',
    'clash',
    'v2ray',
    'xray',
    'hysteria',
    'trojan',
    'mihomo',
    'nekoray',
    'tun',
    'tunnel',
];

const CANDIDATE_LOCAL_PROXY_PORTS = [7890, 7891, 7897, 7898, 8080, 8081, 1080, 10808, 10809, 20170, 6152];

function normalizeSignalValue(value?: string): string {
    return (value ?? '').trim().replace(/\/+$/, '');
}

function createDiagnosis(
    kind: ProxyEnvironmentKind,
    level: ProxyEnvironmentDiagnosis['level'],
    allowTakeover: boolean,
    summary: string,
    detail: string,
    extras: Partial<ProxyEnvironmentDiagnosis> = {},
): ProxyEnvironmentDiagnosis {
    return {
        kind,
        level,
        allowTakeover,
        summary,
        detail,
        recommendedAction: extras.recommendedAction,
        upstreamProxy: extras.upstreamProxy,
        upstreamSource: extras.upstreamSource,
        detectedDesktopProcesses: extras.detectedDesktopProcesses ?? [],
        detectedTunAdapters: extras.detectedTunAdapters ?? [],
        checkedAt: new Date().toISOString(),
    };
}

export function buildProxyEnvironmentDiagnosis(signals: ProxyEnvironmentSignals): ProxyEnvironmentDiagnosis {
    const configuredUpstream = normalizeSignalValue(signals.configuredUpstream);
    const vscodeProxy = normalizeSignalValue(signals.vscodeProxy);
    const previousProxy = normalizeSignalValue(signals.previousProxy);
    const systemProxy = normalizeSignalValue(signals.systemProxy);
    const systemPacUrl = normalizeSignalValue(signals.systemPacUrl);
    const envProxy = normalizeSignalValue(signals.envProxy);
    const localDiscoveredProxy = normalizeSignalValue(signals.localDiscoveredProxy);
    const desktopProcesses = (signals.desktopProcesses ?? []).filter(Boolean);
    const tunAdapters = (signals.tunAdapters ?? []).filter(Boolean);
    const processNames = desktopProcesses.map(value => value.toLowerCase());
    const hasSingBox = processNames.some(value => value.includes('sing-box'));
    const hasProxifier = processNames.some(value => value.includes('proxifier'));
    const hasGenericDesktopProxy = desktopProcesses.length > 0;

    if (configuredUpstream) {
        return createDiagnosis(
            'configured-upstream',
            'ok',
            true,
            `将复用已配置的上游代理 ${configuredUpstream}。`,
            '启动 ai-monitor 时会把 VS Code 流量接到本地监控代理，再继续转发到这个上游代理。',
            { upstreamProxy: configuredUpstream, upstreamSource: 'config', detectedDesktopProcesses: desktopProcesses, detectedTunAdapters: tunAdapters },
        );
    }

    if (tunAdapters.length > 0) {
        return createDiagnosis(
            'tun',
            'warning',
            false,
            '检测到 TUN/虚拟网卡高风险环境，已阻止自动接管。',
            `命中的虚拟网卡: ${tunAdapters.join('；')}`,
            {
                detectedDesktopProcesses: desktopProcesses,
                detectedTunAdapters: tunAdapters,
                recommendedAction: '如果桌面代理已暴露本地 HTTP/SOCKS 端口，请在扩展设置里显式填写上游代理后再重试；否则请改走非接管模式。',
            },
        );
    }

    if (vscodeProxy) {
        return createDiagnosis(
            'vscode-http-proxy',
            'ok',
            true,
            `将复用当前 VS Code 代理 ${vscodeProxy} 作为上游。`,
            '接管后会保留现有 IDE 代理出口，避免把原来的外网链路改成直连。',
            { upstreamProxy: vscodeProxy, upstreamSource: 'vscode', detectedDesktopProcesses: desktopProcesses, detectedTunAdapters: tunAdapters },
        );
    }

    if (previousProxy) {
        return createDiagnosis(
            'previous-vscode-http-proxy',
            'ok',
            true,
            `将复用上次保存的 VS Code 代理 ${previousProxy}。`,
            '当前 http.proxy 已被其他流程清空，但历史代理仍可作为安全上游链路使用。',
            { upstreamProxy: previousProxy, upstreamSource: 'previous', detectedDesktopProcesses: desktopProcesses, detectedTunAdapters: tunAdapters },
        );
    }

    if (systemProxy) {
        return createDiagnosis(
            'system-proxy',
            'ok',
            true,
            `将复用系统代理 ${systemProxy} 作为上游。`,
            '这通常适用于公司代理、桌面代理显式接管 WinINet 的环境。',
            { upstreamProxy: systemProxy, upstreamSource: 'system', detectedDesktopProcesses: desktopProcesses, detectedTunAdapters: tunAdapters },
        );
    }

    if (envProxy) {
        return createDiagnosis(
            'env-proxy',
            'ok',
            true,
            `将复用环境变量代理 ${envProxy} 作为上游。`,
            '检测到当前进程环境已声明 HTTP/HTTPS/ALL_PROXY，可继续沿用该出口。',
            { upstreamProxy: envProxy, upstreamSource: 'env', detectedDesktopProcesses: desktopProcesses, detectedTunAdapters: tunAdapters },
        );
    }

    if (localDiscoveredProxy) {
        return createDiagnosis(
            'desktop-proxy',
            'ok',
            true,
            `检测到可复用的本地代理端口 ${localDiscoveredProxy}。`,
            '虽然当前未读取到系统代理或 VS Code 代理，但本机存在可验证的本地 HTTP/SOCKS 代理端口，可作为安全上游继续串联。',
            { upstreamProxy: localDiscoveredProxy, upstreamSource: 'local-discovery', detectedDesktopProcesses: desktopProcesses, detectedTunAdapters: tunAdapters },
        );
    }

    if (systemPacUrl) {
        return createDiagnosis(
            'system-pac',
            'warning',
            false,
            '检测到系统 PAC 自动代理，当前版本不会直接接管。',
            `PAC 地址: ${systemPacUrl}`,
            {
                detectedDesktopProcesses: desktopProcesses,
                detectedTunAdapters: tunAdapters,
                recommendedAction: '请先在高级设置中填写一个明确可用的 HTTP/SOCKS 上游代理，或暂时保持当前网络链路不接管。',
            },
        );
    }

    if (hasSingBox) {
        return createDiagnosis(
            'desktop-proxy',
            'warning',
            false,
            '检测到 sing-box 进程，但没有发现可复用的 HTTP/SOCKS 上游代理。',
            `当前仅识别到桌面代理进程: ${desktopProcesses.join('、')}`,
            {
                detectedDesktopProcesses: desktopProcesses,
                detectedTunAdapters: tunAdapters,
                recommendedAction: '请确认 sing-box 是否开启了本地 HTTP/SOCKS 端口，并把该端口配置为系统代理、VS Code 代理或扩展上游代理后再启用接管。',
            },
        );
    }

    if (hasProxifier) {
        return createDiagnosis(
            'desktop-proxy',
            'ok',
            true,
            '检测到 Proxifier 进程，将保留现有桌面代理链路后接管 VS Code。',
            'Proxifier 按规则转发时通常不需要额外声明 HTTP/SOCKS 上游；ai-monitor 出口会继续沿用当前桌面代理规则。',
            { detectedDesktopProcesses: desktopProcesses, detectedTunAdapters: tunAdapters },
        );
    }

    if (hasGenericDesktopProxy) {
        return createDiagnosis(
            'desktop-proxy',
            'warning',
            false,
            `检测到桌面代理软件 ${desktopProcesses.join('、')}，但没有发现可复用的明确上游代理。`,
            '为避免把现有网络出口误改成直连，当前版本不会仅凭进程存在就自动接管 VS Code 代理。',
            {
                detectedDesktopProcesses: desktopProcesses,
                detectedTunAdapters: tunAdapters,
                recommendedAction: '请让该代理软件显式写入系统代理或暴露本地 HTTP/SOCKS 端口，再通过扩展设置、VS Code 代理或系统代理让扩展复用该上游。',
            },
        );
    }

    return createDiagnosis(
        'direct',
        'neutral',
        true,
        '当前网络环境为直连，可直接接管 VS Code 代理。',
        '如果你的机器必须依赖公司代理或桌面代理出网，请先配置明确的上游代理后再启用。',
        { detectedDesktopProcesses: desktopProcesses, detectedTunAdapters: tunAdapters },
    );
}

export interface ProxyStartResult {
    status: ProxyStatus;
    routingChanged: boolean;
    diagnosis: ProxyEnvironmentDiagnosis;
}

export function selectHighRiskTunAdapters(
    adapters: WindowsNetworkAdapterSnapshot[],
): string[] {
    const keywords = ['wintun', 'sing-box', 'tap-windows', 'wireguard', 'clash', 'nekoray'];
    return adapters
        .map(adapter => {
            const status = (adapter.Status ?? '').trim().toLowerCase();
            if (status !== 'up') {
                return '';
            }

            const name = (adapter.Name ?? '').trim();
            const description = (adapter.InterfaceDescription ?? '').trim();
            const combined = `${name} ${description}`.toLowerCase();
            if (!combined || !keywords.some(keyword => combined.includes(keyword))) {
                return '';
            }
            return description ? `${name || '未知适配器'} (${description})` : (name || '未知适配器');
        })
        .filter(Boolean);
}

interface LocalBinaryCandidate {
    path: string;
    kind: 'bundled' | 'workspace' | 'global';
}

export interface LocalProxyStatusSnapshot {
    status?: string;
    version?: string;
    mode?: string;
    pid?: number;
    port?: number;
    uptime_seconds?: number;
    upstream_proxy?: string;
    user?: string;
    department?: string;
    source_app?: string;
    server?: string;
    ai_domains?: number;
    ai_wildcard_patterns?: number;
    extra_monitor_hosts?: number;
    extra_monitor_suffixes?: number;
    stats?: {
        total_reported?: number;
        total_tokens?: number;
    };
}

interface TakeoverPreflightResult {
    ok: boolean;
    detail: string;
    recommendedAction?: string;
}

export class ProxyManager {
    private process: ChildProcess | null = null;
    private outputChannel: vscode.OutputChannel;
    private readonly recentOutputLines: string[] = [];
    private partialOutputLine = '';
    private healthCheckTimer?: ReturnType<typeof setInterval>;
    /** When set, overrides config.proxyPort — used when we discover an existing instance on a different port */
    private activePort?: number;
    private certificatePrepared = false;
    private lastStartupError?: string;
    private lastEnvironmentDiagnosis?: { fetchedAt: number; data: ProxyEnvironmentDiagnosis };
    private environmentDiagnosisInFlight?: Promise<ProxyEnvironmentDiagnosis>;

    constructor(
        private config: MonitorConfig,
        private readonly context: vscode.ExtensionContext,
    ) {
        this.outputChannel = vscode.window.createOutputChannel('AI Token Monitor Proxy');
    }

    private pushRecentOutputLine(line: string): void {
        const trimmed = line.trimEnd();
        if (!trimmed) {
            return;
        }
        this.recentOutputLines.push(trimmed);
        if (this.recentOutputLines.length > 200) {
            this.recentOutputLines.splice(0, this.recentOutputLines.length - 200);
        }
    }

    private appendOutput(text: string): void {
        this.outputChannel.append(text);
        const normalized = text.replace(/\r\n/g, '\n').replace(/\r/g, '\n');
        const combined = this.partialOutputLine + normalized;
        const lines = combined.split('\n');
        this.partialOutputLine = lines.pop() ?? '';
        for (const line of lines) {
            this.pushRecentOutputLine(line);
        }
    }

    private appendOutputLine(line: string): void {
        this.outputChannel.appendLine(line);
        this.pushRecentOutputLine(line);
    }

    public get isRunning(): boolean {
        return this.process !== null && this.process.exitCode === null;
    }

    public updateConfig(config: MonitorConfig): void {
        this.config = config;
        this.lastEnvironmentDiagnosis = undefined;
    }

    public getRecentOutputLines(limit = 40): string[] {
        return this.recentOutputLines.slice(-Math.max(1, limit));
    }

    public async getLocalStatus(): Promise<LocalProxyStatusSnapshot | null> {
        return new Promise(resolve => {
            const req = http.request({
                hostname: '127.0.0.1',
                port: this.getEffectivePort(),
                path: '/status',
                method: 'GET',
                timeout: 1500,
            }, res => {
                let body = '';
                res.on('data', chunk => {
                    body += chunk.toString();
                });
                res.on('end', () => {
                    if (!res.statusCode || res.statusCode < 200 || res.statusCode >= 300) {
                        resolve(null);
                        return;
                    }
                    try {
                        resolve(JSON.parse(body) as LocalProxyStatusSnapshot);
                    } catch {
                        resolve(null);
                    }
                });
            });
            req.on('error', () => resolve(null));
            req.on('timeout', () => {
                req.destroy();
                resolve(null);
            });
            req.end();
        });
    }

    public async start(options?: { skipUpstreamDetect?: boolean }): Promise<ProxyStartResult> {
        const diagnosis = await this.getEnvironmentDiagnosis(true);
        if (!this.config.transparentMode) {
            this.appendOutputLine('[proxy] Transparent proxy disabled by configuration.');
            await this.restoreHttpProxyIfMitmUnavailable();
            return { status: 'off', routingChanged: false, diagnosis };
        }

        if (!diagnosis.allowTakeover) {
            this.appendOutputLine(`[proxy] Takeover blocked: ${diagnosis.summary}`);
            if (diagnosis.detail) {
                this.appendOutputLine(`[proxy] ${diagnosis.detail}`);
            }
            await this.restoreHttpProxyIfMitmUnavailable();
            return { status: 'blocked', routingChanged: false, diagnosis };
        }

        this.lastStartupError = undefined;
        const binaryPath = await this.resolveBinaryPath();
        const configPath = path.join(this.context.globalStorageUri.fsPath, 'proxy-config.json');
        await fs.promises.mkdir(path.dirname(configPath), { recursive: true });

        let upstreamProxy = options?.skipUpstreamDetect ? '' : diagnosis.upstreamProxy ?? '';
        if (upstreamProxy && await this.shouldIgnoreUpstreamProxy(upstreamProxy)) {
            const blockedDiagnosis = this.blockTakeover(
                diagnosis,
                '检测到的上游代理当前不可达，已停止自动接管。',
                `上游代理 ${upstreamProxy} 当前无法连通；为避免把现有网络改坏，本次不会覆盖 VS Code 的代理设置。`,
                '请确认本地代理端口仍在运行，或在高级设置里改成一个可用的 HTTP/SOCKS 上游代理后再试。',
            );
            this.appendOutputLine(`[proxy] Blocking takeover due to unreachable upstream proxy: ${upstreamProxy}`);
            await this.restoreHttpProxyIfMitmUnavailable();
            return { status: 'blocked', routingChanged: false, diagnosis: blockedDiagnosis };
        }
        const proxyConfig: Record<string, unknown> = {
            server_url: this.config.serverUrl,
            user_name: this.config.userName,
            user_id: this.config.userId,
            department: this.config.department,
            port: this.config.proxyPort,
            gateway_port: this.config.gatewayPort,
            report_opaque_traffic: true,
        };
        if (upstreamProxy) {
            proxyConfig.upstream_proxy = upstreamProxy;
        }

        await fs.promises.writeFile(configPath, JSON.stringify(proxyConfig, null, 2), 'utf8');

        if (binaryPath) {
            const certificateReady = await this.ensureCertificateInstalled(binaryPath, configPath);
            if (!certificateReady) {
                await this.restoreHttpProxyIfMitmUnavailable();
                return { status: 'off', routingChanged: false, diagnosis };
            }
        }

        // NOTE: We intentionally do NOT clear stale MITM routing here.
        // If the proxy is about to (re)start, clearing http.proxy then re-setting it
        // causes ensureVsCodeProxyRouting() to return routingChanged=true, triggering
        // an infinite reload loop. All failure paths below already call
        // restoreHttpProxyIfMitmUnavailable() individually.

        if (this.detectExternalProxy()) {
            const preflight = await this.runTakeoverPreflight();
            if (!preflight.ok) {
                const blockedDiagnosis = this.blockTakeover(
                    diagnosis,
                    '接管前预演失败，已保持当前网络链路不变。',
                    preflight.detail,
                    preflight.recommendedAction,
                );
                await this.restoreHttpProxyIfMitmUnavailable();
                return { status: 'blocked', routingChanged: false, diagnosis: blockedDiagnosis };
            }
            const routingChanged = await this.ensureVsCodeProxyRouting();
            this.startHealthCheck();
            return { status: 'external', routingChanged, diagnosis };
        }

        if (this.isRunning || await this.isProxyAvailable()) {
            const preflight = await this.runTakeoverPreflight();
            if (!preflight.ok) {
                const blockedDiagnosis = this.blockTakeover(
                    diagnosis,
                    '接管前预演失败，已保持当前网络链路不变。',
                    preflight.detail,
                    preflight.recommendedAction,
                );
                await this.restoreHttpProxyIfMitmUnavailable();
                return { status: 'blocked', routingChanged: false, diagnosis: blockedDiagnosis };
            }
            const routingChanged = await this.ensureVsCodeProxyRouting();
            this.startHealthCheck();
            return { status: this.isRunning ? 'internal' : 'external', routingChanged, diagnosis };
        }

        // Discover an existing ai-monitor instance (another IDE may have started it)
        const existing = await this.discoverRunningInstance();
        if (existing) {
            this.appendOutputLine(`[proxy] Discovered existing ai-monitor on port ${existing.port} — reusing`);
            this.activePort = existing.port;
            const preflight = await this.runTakeoverPreflight();
            if (!preflight.ok) {
                const blockedDiagnosis = this.blockTakeover(
                    diagnosis,
                    '接管前预演失败，已保持当前网络链路不变。',
                    preflight.detail,
                    preflight.recommendedAction,
                );
                await this.restoreHttpProxyIfMitmUnavailable();
                return { status: 'blocked', routingChanged: false, diagnosis: blockedDiagnosis };
            }
            const routingChanged = await this.ensureVsCodeProxyRouting();
            this.startHealthCheck();
            return { status: 'external', routingChanged, diagnosis };
        }

        if (!binaryPath) {
            this.appendOutputLine('[proxy] Binary not found. Falling back to in-process interception only.');
            await this.restoreHttpProxyIfMitmUnavailable();
            return { status: 'off', routingChanged: false, diagnosis };
        }

        const args = ['--config', configPath];
        this.appendOutputLine(`[proxy] Starting: ${binaryPath} ${args.join(' ')}`);

        const proc = spawn(binaryPath, args, {
            stdio: ['pipe', 'pipe', 'pipe'],
            detached: false,
            windowsHide: true,
            env: { ...process.env, AI_MONITOR_NO_CONSOLE: '1' }
        });
        this.process = proc;

        proc.on('error', (error: NodeJS.ErrnoException) => {
            const message = error.message || String(error);
            this.lastStartupError = message;
            this.appendOutputLine(`[proxy] Failed to start local proxy process: ${message}`);
            if (error.code) {
                this.appendOutputLine(`[proxy] spawn error code: ${error.code}`);
            }
            this.process = null;
            this.activePort = undefined;
            this.stopHealthCheck();
            void this.restoreHttpProxyIfMitmUnavailable();
        });

        proc.stdout?.on('data', (data: Buffer) => {
            this.appendOutput(data.toString());
        });
        proc.stderr?.on('data', (data: Buffer) => {
            this.appendOutput(data.toString());
        });
        proc.on('exit', (code) => {
            this.appendOutputLine(`[proxy] Process exited with code ${code}`);
            this.process = null;
            this.activePort = undefined;
            this.stopHealthCheck();
            void this.restoreHttpProxyIfMitmUnavailable();
        });

        const ready = await this.waitForProxyReady(8_000);
        if (!ready) {
            this.appendOutputLine('[proxy] Local proxy did not become ready.');
            if (this.lastStartupError) {
                this.appendOutputLine(`[proxy] Last startup error: ${this.lastStartupError}`);
            }
            await this.restoreHttpProxyIfMitmUnavailable();
            return { status: 'off', routingChanged: false, diagnosis };
        }

        // After process starts, read back the actual port from /status
        const statusAfterStart = await this.getLocalStatus();
        if (statusAfterStart?.port && statusAfterStart.port !== this.config.proxyPort) {
            this.appendOutputLine(`[proxy] Actual port ${statusAfterStart.port} differs from configured ${this.config.proxyPort}`);
            this.activePort = statusAfterStart.port;
        }

        const preflight = await this.runTakeoverPreflight();
        if (!preflight.ok) {
            const blockedDiagnosis = this.blockTakeover(
                diagnosis,
                '接管前预演失败，已停止自动接管。',
                preflight.detail,
                preflight.recommendedAction,
            );
            await this.stop();
            return { status: 'blocked', routingChanged: false, diagnosis: blockedDiagnosis };
        }

        this.appendOutputLine(`[proxy] Running on MITM:${this.getEffectivePort()} Gateway:${this.config.gatewayPort}`);
        const routingChanged = await this.ensureVsCodeProxyRouting();
        this.startHealthCheck();
        return { status: 'internal', routingChanged, diagnosis };
    }

    public async restart(config?: MonitorConfig): Promise<ProxyStartResult> {
        if (config) {
            this.config = config;
        }
        await this.stop();
        return this.start();
    }

    public async stop(): Promise<void> {
        this.stopHealthCheck();
        if (!this.process) {
            this.activePort = undefined;
            return;
        }

        this.appendOutputLine('[proxy] Stopping...');
        this.process.kill('SIGTERM');

        await new Promise<void>((resolve) => {
            const proc = this.process;
            if (!proc) {
                resolve();
                return;
            }

            const timeout = setTimeout(() => {
                if (this.process && this.process.exitCode === null) {
                    this.process.kill('SIGKILL');
                }
                resolve();
            }, 5000);

            proc.once('exit', () => {
                clearTimeout(timeout);
                resolve();
            });
        });

        this.process = null;
        this.activePort = undefined;
        this.appendOutputLine('[proxy] Stopped');
        await this.restoreHttpProxyIfMitmUnavailable();
    }

    public getGatewayUrl(): string {
        return `http://127.0.0.1:${this.config.gatewayPort}`;
    }

    public getEffectivePort(): number {
        return this.activePort ?? this.config.proxyPort;
    }

    public getMitmProxyUrl(): string {
        return `http://127.0.0.1:${this.getEffectivePort()}`;
    }

    public detectExternalProxy(): boolean {
        return process.env['AI_MONITOR_LAUNCH_MODE'] === '1';
    }

    public isProxyAvailable(): Promise<boolean> {
        return new Promise(resolve => {
            const req = http.request({
                hostname: '127.0.0.1',
                port: this.getEffectivePort(),
                path: '/status',
                method: 'GET',
            }, res => resolve(res.statusCode === 200));
            req.setTimeout(1000, () => {
                req.destroy();
                resolve(false);
            });
            req.on('error', () => resolve(false));
            req.end();
        });
    }

    public async getProxyStatus(): Promise<ProxyStatus> {
        const diagnosis = await this.getEnvironmentDiagnosis();
        if (this.config.transparentMode && !diagnosis.allowTakeover) {
            return 'blocked';
        }
        if (this.detectExternalProxy()) {
            return 'external';
        }
        if (this.isRunning) {
            return 'internal';
        }
        return (await this.isProxyAvailable()) ? 'external' : 'off';
    }

    public async getEnvironmentDiagnosis(force = false): Promise<ProxyEnvironmentDiagnosis> {
        const cacheTtlMs = 15_000;
        if (!force && this.lastEnvironmentDiagnosis && (Date.now() - this.lastEnvironmentDiagnosis.fetchedAt) < cacheTtlMs) {
            return this.lastEnvironmentDiagnosis.data;
        }
        if (this.environmentDiagnosisInFlight) {
            return this.environmentDiagnosisInFlight;
        }

        this.environmentDiagnosisInFlight = Promise.resolve().then(() => {
            const configuredUpstream = this.normalizeProxyValue(this.config.upstreamProxy);
            const httpProxyConfig = vscode.workspace.getConfiguration('http');
            const vscodeProxy = this.normalizeProxyValue(httpProxyConfig.get<string>('proxy', ''));
            const previousProxy = this.normalizeProxyValue(
                this.context.globalState.get<string>('aiTokenMonitor.previousHttpProxy', ''),
            );
            const systemSettings = this.readWindowsInternetSettings();
            const envProxy = this.getEnvironmentProxy();
            const desktopProcesses = this.detectDesktopProxyProcesses();
            const tunAdapters = this.detectHighRiskTunAdapters();
            const candidateSignals = {
                configuredUpstream: !this.isMitmProxyValue(configuredUpstream) ? configuredUpstream : '',
                vscodeProxy: !this.isMitmProxyValue(vscodeProxy) ? vscodeProxy : '',
                previousProxy: !this.isMitmProxyValue(previousProxy) ? previousProxy : '',
                systemProxy: !this.isMitmProxyValue(systemSettings.proxyServer) ? systemSettings.proxyServer : '',
                systemPacUrl: systemSettings.autoConfigUrl,
                envProxy: !this.isMitmProxyValue(envProxy) ? envProxy : '',
                desktopProcesses,
                tunAdapters,
            } satisfies ProxyEnvironmentSignals;

            return Promise.resolve().then(async () => {
                const hasExplicitUpstream = Boolean(
                    candidateSignals.configuredUpstream
                    || candidateSignals.vscodeProxy
                    || candidateSignals.previousProxy
                    || candidateSignals.systemProxy
                    || candidateSignals.envProxy,
                );
                const localDiscoveredProxy = (!hasExplicitUpstream && !candidateSignals.systemPacUrl && tunAdapters.length === 0 && desktopProcesses.length > 0)
                    ? await this.discoverLocalUpstreamProxyCandidate()
                    : '';
                const diagnosis = buildProxyEnvironmentDiagnosis({
                    ...candidateSignals,
                    localDiscoveredProxy,
                });
                this.lastEnvironmentDiagnosis = { fetchedAt: Date.now(), data: diagnosis };
                return diagnosis;
            });
        }).finally(() => {
            this.environmentDiagnosisInFlight = undefined;
        });

        return this.environmentDiagnosisInFlight;
    }

    public findBinaryPath(): string | null {
        return this.findBinaryCandidate()?.path ?? null;
    }

    private findBinaryCandidate(): LocalBinaryCandidate | null {
        const ext = process.platform === 'win32' ? '.exe' : '';
        const binaryName = `ai-monitor${ext}`;

        const bundledPath = path.join(this.context.extensionPath, 'bin', binaryName);
        if (fs.existsSync(bundledPath)) {
            return { path: bundledPath, kind: 'bundled' };
        }

        const workspaceFolders = vscode.workspace.workspaceFolders ?? [];
        for (const folder of workspaceFolders) {
            const workspaceBin = path.join(folder.uri.fsPath, 'bin', binaryName);
            if (fs.existsSync(workspaceBin)) {
                return { path: workspaceBin, kind: 'workspace' };
            }

            const siblingClient = path.resolve(folder.uri.fsPath, '..', 'client', binaryName);
            if (fs.existsSync(siblingClient)) {
                return { path: siblingClient, kind: 'workspace' };
            }

            const nestedClient = path.join(folder.uri.fsPath, 'client', binaryName);
            if (fs.existsSync(nestedClient)) {
                return { path: nestedClient, kind: 'workspace' };
            }
        }

        const globalPath = path.join(this.context.globalStorageUri.fsPath, 'bin', binaryName);
        if (fs.existsSync(globalPath)) {
            return { path: globalPath, kind: 'global' };
        }

        return null;
    }

    private async resolveBinaryPath(): Promise<string | null> {
        const candidate = this.findBinaryCandidate();
        if (!candidate) {
            return null;
        }
        if (candidate.kind !== 'bundled') {
            return candidate.path;
        }

        const ext = process.platform === 'win32' ? '.exe' : '';
        const binaryName = `ai-monitor${ext}`;
        const globalPath = path.join(this.context.globalStorageUri.fsPath, 'bin', binaryName);
        try {
            await fs.promises.mkdir(path.dirname(globalPath), { recursive: true });
            const sourceStat = await fs.promises.stat(candidate.path);
            let shouldCopy = true;
            try {
                const targetStat = await fs.promises.stat(globalPath);
                shouldCopy = targetStat.size !== sourceStat.size || targetStat.mtimeMs < sourceStat.mtimeMs - 2000;
            } catch {
                shouldCopy = true;
            }

            if (shouldCopy) {
                await fs.promises.copyFile(candidate.path, globalPath);
                this.appendOutputLine(`[proxy] Prepared local binary: ${globalPath}`);
            }
            return globalPath;
        } catch (error) {
            const message = error instanceof Error ? error.message : String(error);
            this.appendOutputLine(`[proxy] Failed to stage bundled ai-monitor binary, falling back to extension copy: ${message}`);
            return candidate.path;
        }
    }

    /**
     * Discover an already-running ai-monitor instance started by another IDE or CLI.
     * Checks: configured port → PID file → port range scan (18090..18099).
     */
    public async discoverRunningInstance(): Promise<{ port: number; status: LocalProxyStatusSnapshot } | null> {
        // 1. Check configured port first (fast path)
        const configStatus = await this.getLocalStatus();
        if (configStatus?.status === 'running') {
            return { port: this.config.proxyPort, status: configStatus };
        }

        // 2. Check PID file (%APPDATA%/ai-monitor/instance.json)
        const pidInfo = await this.readInstanceInfo();
        if (pidInfo?.port && pidInfo.port !== this.config.proxyPort) {
            const pidStatus = await this.probePort(pidInfo.port);
            if (pidStatus?.status === 'running') {
                return { port: pidInfo.port, status: pidStatus };
            }
        }

        // 3. Scan nearby port range
        const basePort = this.config.proxyPort;
        for (let offset = 1; offset <= 10; offset++) {
            const port = basePort + offset;
            const probeStatus = await this.probePort(port);
            if (probeStatus?.status === 'running') {
                return { port, status: probeStatus };
            }
        }

        return null;
    }

    private async readInstanceInfo(): Promise<{ pid: number; port: number; version?: string } | null> {
        const appData = process.env.APPDATA;
        if (!appData) {
            return null;
        }
        const infoPath = path.join(appData, 'ai-monitor', 'instance.json');
        try {
            const content = await fs.promises.readFile(infoPath, 'utf8');
            return JSON.parse(content);
        } catch {
            return null;
        }
    }

    private probePort(port: number): Promise<LocalProxyStatusSnapshot | null> {
        return new Promise(resolve => {
            const req = http.request({
                hostname: '127.0.0.1',
                port,
                path: '/status',
                method: 'GET',
                timeout: 1500,
            }, res => {
                let body = '';
                res.on('data', chunk => {
                    body += chunk.toString();
                });
                res.on('end', () => {
                    if (!res.statusCode || res.statusCode < 200 || res.statusCode >= 300) {
                        resolve(null);
                        return;
                    }
                    try {
                        resolve(JSON.parse(body) as LocalProxyStatusSnapshot);
                    } catch {
                        resolve(null);
                    }
                });
            });
            req.on('error', () => resolve(null));
            req.on('timeout', () => {
                req.destroy();
                resolve(null);
            });
            req.end();
        });
    }

    private startHealthCheck(): void {
        this.stopHealthCheck();
        this.healthCheckTimer = setInterval(async () => {
            const proxyUp = await this.isProxyAvailable();
            if (!proxyUp) {
                this.appendOutputLine('[proxy] Health check failed — proxy unreachable');
                const restored = await this.restoreHttpProxyIfMitmUnavailable();
                if (restored) {
                    this.stopHealthCheck();
                }
            }
        }, 30_000);
    }

    private stopHealthCheck(): void {
        if (this.healthCheckTimer) {
            clearInterval(this.healthCheckTimer);
            this.healthCheckTimer = undefined;
        }
    }

    private normalizeProxyValue(value: string): string {
        return value.trim().replace(/\/+$/, '');
    }

    private isMitmProxyValue(value: string): boolean {
        const normalized = this.normalizeProxyValue(value);
        if (!normalized) {
            return false;
        }

        try {
            const parsed = new URL(normalized);
            const port = parsed.port ? Number(parsed.port) : (parsed.protocol === 'https:' ? 443 : 80);
            const isLocal = parsed.hostname === '127.0.0.1' || parsed.hostname === 'localhost';
            // Match the configured port, the active port, or the known fallback range (18090..18099)
            const basePort = this.config.proxyPort;
            return isLocal && (
                port === this.getEffectivePort()
                || (port >= basePort && port < basePort + 10)
            );
        } catch {
            return false;
        }
    }

    private async waitForProxyReady(timeoutMs: number): Promise<boolean> {
        const startedAt = Date.now();
        while (Date.now() - startedAt < timeoutMs) {
            if (await this.isProxyAvailable()) {
                return true;
            }
            await new Promise(resolve => setTimeout(resolve, 250));
        }
        return false;
    }

    private async shouldIgnoreUpstreamProxy(proxyUrl: string): Promise<boolean> {
        try {
            const parsed = new URL(proxyUrl);
            const isLoopback = parsed.hostname === '127.0.0.1' || parsed.hostname === 'localhost';
            if (!isLoopback || this.isMitmProxyValue(proxyUrl)) {
                return false;
            }

            const port = parsed.port ? Number(parsed.port) : (parsed.protocol === 'https:' ? 443 : 80);
            return !(await this.isTcpPortReachable(parsed.hostname, port, 1000));
        } catch {
            return false;
        }
    }

    private isTcpPortReachable(host: string, port: number, timeoutMs: number): Promise<boolean> {
        return new Promise(resolve => {
            const socket = net.createConnection({ host, port });
            const finish = (result: boolean) => {
                socket.removeAllListeners();
                socket.destroy();
                resolve(result);
            };

            socket.setTimeout(timeoutMs);
            socket.once('connect', () => finish(true));
            socket.once('timeout', () => finish(false));
            socket.once('error', () => finish(false));
        });
    }

    private resolveUpstreamProxy(): string {
        const configured = this.normalizeProxyValue(this.config.upstreamProxy);
        if (configured && !this.isMitmProxyValue(configured)) {
            return configured;
        }

        const httpProxyConfig = vscode.workspace.getConfiguration('http');
        const currentProxy = this.normalizeProxyValue(httpProxyConfig.get<string>('proxy', ''));
        if (currentProxy && !this.isMitmProxyValue(currentProxy)) {
            return currentProxy;
        }

        const previousProxy = this.normalizeProxyValue(
            this.context.globalState.get<string>('aiTokenMonitor.previousHttpProxy', ''),
        );
        if (previousProxy && !this.isMitmProxyValue(previousProxy)) {
            return previousProxy;
        }

        const systemProxy = this.normalizeProxyValue(this.readWindowsSystemProxy());
        if (systemProxy && !this.isMitmProxyValue(systemProxy)) {
            return systemProxy;
        }

        const configPath = path.join(this.context.globalStorageUri.fsPath, 'proxy-config.json');
        try {
            if (fs.existsSync(configPath)) {
                const parsed = JSON.parse(fs.readFileSync(configPath, 'utf8')) as { upstream_proxy?: string };
                const persistedProxy = this.normalizeProxyValue(parsed.upstream_proxy ?? '');
                if (persistedProxy && !this.isMitmProxyValue(persistedProxy)) {
                    return persistedProxy;
                }
            }
        } catch {
            // Ignore unreadable historical config and continue probing.
        }

        for (const key of ['HTTPS_PROXY', 'https_proxy', 'HTTP_PROXY', 'http_proxy', 'ALL_PROXY', 'all_proxy']) {
            const envProxy = this.normalizeProxyValue(process.env[key] ?? '');
            if (envProxy && !this.isMitmProxyValue(envProxy)) {
                return envProxy;
            }
        }

        return '';
    }

    private getEnvironmentProxy(): string {
        for (const key of ['HTTPS_PROXY', 'https_proxy', 'HTTP_PROXY', 'http_proxy', 'ALL_PROXY', 'all_proxy']) {
            const envProxy = this.normalizeProxyValue(process.env[key] ?? '');
            if (envProxy && !this.isMitmProxyValue(envProxy)) {
                return envProxy;
            }
        }
        return '';
    }

    private detectDesktopProxyProcesses(): string[] {
        try {
            const output = process.platform === 'win32'
                ? execFileSync('tasklist', ['/FO', 'CSV', '/NH'], { encoding: 'utf8', windowsHide: true })
                : execFileSync('ps', ['-A', '-o', 'comm='], { encoding: 'utf8' });
            const parsed = output
                .split(/\r?\n/)
                .map(line => {
                    const trimmed = line.trim();
                    if (!trimmed) {
                        return '';
                    }
                    if (process.platform === 'win32') {
                        const match = trimmed.match(/^"([^"]+)"/);
                        return (match?.[1] ?? '').toLowerCase();
                    }
                    return path.basename(trimmed).toLowerCase();
                })
                .filter(value => {
                    if (!value) {
                        return false;
                    }
                    return KNOWN_DESKTOP_PROXY_PROCESS_NAMES.has(value)
                        || DESKTOP_PROXY_PROCESS_KEYWORDS.some(keyword => value.includes(keyword));
                });
            return Array.from(new Set(parsed));
        } catch {
            return [];
        }
    }

    private async discoverLocalUpstreamProxyCandidate(): Promise<string> {
        const probePorts = CANDIDATE_LOCAL_PROXY_PORTS.filter(port => port !== this.getEffectivePort() && port !== this.config.gatewayPort);
        const results = await Promise.all(probePorts.map(async port => {
            const httpProxy = await this.probeHttpProxyPort(port);
            if (httpProxy) {
                return `http://127.0.0.1:${port}`;
            }

            const socksProxy = await this.probeSocks5ProxyPort(port);
            if (socksProxy) {
                return `socks5://127.0.0.1:${port}`;
            }

            return '';
        }));

        return results.find(Boolean) ?? '';
    }

    private probeHttpProxyPort(port: number): Promise<boolean> {
        return new Promise(resolve => {
            const socket = net.createConnection({ host: '127.0.0.1', port });
            const finish = (result: boolean) => {
                socket.removeAllListeners();
                socket.destroy();
                resolve(result);
            };

            socket.setTimeout(500);
            socket.once('connect', () => {
                socket.write('CONNECT 127.0.0.1:1 HTTP/1.1\r\nHost: 127.0.0.1:1\r\nProxy-Connection: Keep-Alive\r\n\r\n');
            });
            socket.on('data', (chunk: Buffer) => {
                const text = chunk.toString('utf8');
                const match = text.match(/^HTTP\/1\.[01]\s+(\d{3})/i);
                if (!match) {
                    finish(false);
                    return;
                }
                const statusCode = Number(match[1]);
                finish([200, 403, 407, 502, 503, 504].includes(statusCode));
            });
            socket.once('timeout', () => finish(false));
            socket.once('error', () => finish(false));
            socket.once('end', () => finish(false));
        });
    }

    private probeSocks5ProxyPort(port: number): Promise<boolean> {
        return new Promise(resolve => {
            const socket = net.createConnection({ host: '127.0.0.1', port });
            const finish = (result: boolean) => {
                socket.removeAllListeners();
                socket.destroy();
                resolve(result);
            };

            socket.setTimeout(500);
            socket.once('connect', () => {
                socket.write(Buffer.from([0x05, 0x01, 0x00]));
            });
            socket.on('data', (chunk: Buffer) => {
                if (chunk.length < 2) {
                    finish(false);
                    return;
                }
                finish(chunk[0] === 0x05 && [0x00, 0x02, 0xff].includes(chunk[1]));
            });
            socket.once('timeout', () => finish(false));
            socket.once('error', () => finish(false));
            socket.once('end', () => finish(false));
        });
    }

    private detectHighRiskTunAdapters(): string[] {
        if (process.platform !== 'win32') {
            return [];
        }

        try {
            const output = execFileSync(
                this.getWindowsPowerShellPath(),
                [
                    '-NoProfile',
                    '-NonInteractive',
                    '-Command',
                    '@(Get-NetAdapter -IncludeHidden | Select-Object Name, InterfaceDescription, Status | ConvertTo-Json -Compress -Depth 3)',
                ],
                { encoding: 'utf8', windowsHide: true },
            ).trim();
            if (!output) {
                return [];
            }

            const adapters = JSON.parse(output) as WindowsNetworkAdapterSnapshot[] | WindowsNetworkAdapterSnapshot;
            const list = Array.isArray(adapters) ? adapters : [adapters];
            return selectHighRiskTunAdapters(list);
        } catch {
            return [];
        }
    }

    private readWindowsInternetSettings(): { proxyServer: string; autoConfigUrl: string } {
        if (process.platform !== 'win32') {
            return { proxyServer: '', autoConfigUrl: '' };
        }

        let proxyServer = '';
        let autoConfigUrl = '';

        try {
            const enableOutput = execFileSync(
                'reg',
                ['query', 'HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings', '/v', 'ProxyEnable'],
                { encoding: 'utf8', windowsHide: true },
            );
            if (enableOutput.includes('0x1')) {
                const serverOutput = execFileSync(
                    'reg',
                    ['query', 'HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings', '/v', 'ProxyServer'],
                    { encoding: 'utf8', windowsHide: true },
                );
                const line = serverOutput
                    .split(/\r?\n/)
                    .map(value => value.trim())
                    .find(value => value.startsWith('ProxyServer'));
                if (line) {
                    const parts = line.split(/REG_SZ/i);
                    const value = parts.length >= 2 ? parts[1].trim() : '';
                    if (value) {
                        proxyServer = value.includes('://') ? value : `http://${value}`;
                    }
                }
            }
        } catch {
            // Ignore registry read errors and fall through.
        }

        try {
            const pacOutput = execFileSync(
                'reg',
                ['query', 'HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings', '/v', 'AutoConfigURL'],
                { encoding: 'utf8', windowsHide: true },
            );
            const line = pacOutput
                .split(/\r?\n/)
                .map(value => value.trim())
                .find(value => value.startsWith('AutoConfigURL'));
            if (line) {
                const parts = line.split(/REG_SZ/i);
                autoConfigUrl = parts.length >= 2 ? parts[1].trim() : '';
            }
        } catch {
            // PAC is optional.
        }

        return { proxyServer, autoConfigUrl };
    }

    private readWindowsSystemProxy(): string {
        return this.readWindowsInternetSettings().proxyServer;
    }

    private getWindowsPowerShellPath(): string {
        const systemRoot = process.env.SYSTEMROOT || 'C:\\Windows';
        return path.join(systemRoot, 'System32', 'WindowsPowerShell', 'v1.0', 'powershell.exe');
    }

    private async ensureCertificateInstalled(binaryPath: string, configPath: string): Promise<boolean> {
        if (this.certificatePrepared || process.platform !== 'win32') {
            this.certificatePrepared = true;
            return true;
        }

        return new Promise(resolve => {
            let settled = false;
            const finish = (value: boolean) => {
                if (settled) {
                    return;
                }
                settled = true;
                resolve(value);
            };
            const proc = spawn(binaryPath, ['--install', '--install-cert-only', '--config', configPath], {
                stdio: ['ignore', 'pipe', 'pipe'],
                detached: false,
                windowsHide: true,
                env: { ...process.env, AI_MONITOR_NO_CONSOLE: '1' },
            });

            let output = '';
            const timeout = setTimeout(() => {
                output += '\ncertificate installation timed out';
                proc.kill();
            }, 15000);

            proc.stdout?.on('data', (data: Buffer) => {
                output += data.toString();
            });
            proc.stderr?.on('data', (data: Buffer) => {
                output += data.toString();
            });
            proc.on('error', error => {
                clearTimeout(timeout);
                output += `\nspawn error: ${error.message || String(error)}`;
                this.appendOutputLine('[proxy] Failed to launch certificate installer');
                this.appendOutputLine(output.trim());
                finish(false);
            });
            proc.on('exit', code => {
                clearTimeout(timeout);
                if (code === 0) {
                    this.certificatePrepared = true;
                    this.appendOutputLine('[proxy] Ensured current-user CA certificate is installed');
                    finish(true);
                    return;
                }

                const trimmed = output.trim();
                this.appendOutputLine('[proxy] Failed to install current-user CA certificate automatically');
                if (trimmed) {
                    this.appendOutputLine(trimmed);
                }
                void vscode.window.showWarningMessage(
                    'AI Token 监控无法自动安装当前用户证书，已停止接管网络。请先允许证书安装后再启用监控代理。',
                );
                finish(false);
            });
        });
    }

    /**
     * If `http.proxy` points at this extension's MITM port but no proxy is reachable,
     * restore the previously saved user proxy or clear the setting so VS Code networking works.
     */
    public async restoreHttpProxyIfMitmUnavailable(): Promise<boolean> {
        const httpConfig = vscode.workspace.getConfiguration('http');
        const currentProxy = this.normalizeProxyValue(httpConfig.get<string>('proxy', ''));

        if (!this.isMitmProxyValue(currentProxy)) {
            return false;
        }

        const proxyUp = this.isRunning || (await this.isProxyAvailable());
        if (proxyUp) {
            return false;
        }

        const previous = this.normalizeProxyValue(
            this.context.globalState.get<string>('aiTokenMonitor.previousHttpProxy', ''),
        );
        await httpConfig.update('proxy', previous, vscode.ConfigurationTarget.Global);

        // Restore proxyStrictSSL
        const prevStrictSSL = this.context.globalState.get<boolean | undefined>('aiTokenMonitor.previousProxyStrictSSL');
        if (prevStrictSSL !== undefined) {
            await httpConfig.update('proxyStrictSSL', prevStrictSSL, vscode.ConfigurationTarget.Global);
        }

        this.appendOutputLine(
            `[proxy] Restored VS Code http.proxy (MITM not running) -> ${previous || '(empty)'}`,
        );
        return true;
    }

    private async ensureVsCodeProxyRouting(): Promise<boolean> {
        if (!this.config.transparentMode) {
            return false;
        }

        const httpConfig = vscode.workspace.getConfiguration('http');
        const currentProxy = this.normalizeProxyValue(httpConfig.get<string>('proxy', ''));
        const mitmProxy = this.getMitmProxyUrl();

        if (currentProxy && !this.isMitmProxyValue(currentProxy)) {
            await this.context.globalState.update('aiTokenMonitor.previousHttpProxy', currentProxy);
        }

        if (currentProxy === mitmProxy) {
            // Ensure proxyStrictSSL is disabled even if proxy was already set
            const strictSSL = httpConfig.get<boolean>('proxyStrictSSL', true);
            if (strictSSL) {
                await this.context.globalState.update('aiTokenMonitor.previousProxyStrictSSL', strictSSL);
                await httpConfig.update('proxyStrictSSL', false, vscode.ConfigurationTarget.Global);
                this.appendOutputLine('[proxy] Disabled http.proxyStrictSSL for MITM proxy');
                return true;
            }
            return false;
        }

        // Save previous proxyStrictSSL before overriding
        const prevStrictSSL = httpConfig.get<boolean>('proxyStrictSSL', true);
        await this.context.globalState.update('aiTokenMonitor.previousProxyStrictSSL', prevStrictSSL);

        await httpConfig.update('proxy', mitmProxy, vscode.ConfigurationTarget.Global);
        await httpConfig.update('proxyStrictSSL', false, vscode.ConfigurationTarget.Global);
        this.appendOutputLine(`[proxy] Updated VS Code http.proxy -> ${mitmProxy}, proxyStrictSSL -> false`);
        return true;
    }

    private blockTakeover(
        diagnosis: ProxyEnvironmentDiagnosis,
        summary: string,
        detail: string,
        recommendedAction?: string,
    ): ProxyEnvironmentDiagnosis {
        const blockedDiagnosis = createDiagnosis(
            diagnosis.kind,
            'warning',
            false,
            summary,
            detail,
            {
                upstreamProxy: diagnosis.upstreamProxy,
                upstreamSource: diagnosis.upstreamSource,
                detectedDesktopProcesses: diagnosis.detectedDesktopProcesses,
                detectedTunAdapters: diagnosis.detectedTunAdapters,
                recommendedAction,
            },
        );
        this.lastEnvironmentDiagnosis = { fetchedAt: Date.now(), data: blockedDiagnosis };
        return blockedDiagnosis;
    }

    private async runTakeoverPreflight(): Promise<TakeoverPreflightResult> {
        const localStatus = await this.getLocalStatus();
        if (!localStatus || localStatus.status !== 'running') {
            return {
                ok: false,
                detail: '本地 ai-monitor 没有返回 running 状态，接管前预演未通过。',
                recommendedAction: '请先检查 Output 面板中的 AI Token Monitor Proxy 输出，确认本地代理已成功启动。',
            };
        }

        const probeUrl = this.buildServerHealthProbeUrl();
        if (!probeUrl) {
            return {
                ok: true,
                detail: '未配置可用的服务端 health 探测地址，已跳过接管前预演。',
            };
        }

        const healthCheck = await this.requestHealthViaMitmProxy(probeUrl);
        if (!healthCheck.ok) {
            return {
                ok: false,
                detail: `本地代理已启动，但通过候选链路访问 ${probeUrl} 失败：${healthCheck.detail}`,
                recommendedAction: '请确认上报服务器可达，以及当前桌面代理或系统代理链路允许访问该服务端地址。',
            };
        }

        return {
            ok: true,
            detail: `已通过本地代理链路验证 ${probeUrl} 可达。`,
        };
    }

    private buildServerHealthProbeUrl(): string {
        const base = this.config.serverUrl.trim();
        if (!base) {
            return '';
        }

        try {
            return new URL('/health', base.endsWith('/') ? base : `${base}/`).toString();
        } catch {
            return '';
        }
    }

    private async requestHealthViaMitmProxy(targetUrl: string): Promise<{ ok: boolean; detail: string }> {
        try {
            const target = new URL(targetUrl);
            if (target.protocol === 'http:') {
                return await this.requestHttpHealthViaProxy(target);
            }
            if (target.protocol === 'https:') {
                return await this.requestHttpsHealthViaProxy(target);
            }
            return { ok: false, detail: `不支持的协议 ${target.protocol}` };
        } catch (error) {
            return {
                ok: false,
                detail: error instanceof Error ? error.message : 'health 探测地址无效',
            };
        }
    }

    private requestHttpHealthViaProxy(target: URL): Promise<{ ok: boolean; detail: string }> {
        return new Promise(resolve => {
            const req = http.request({
                host: '127.0.0.1',
                port: this.getEffectivePort(),
                method: 'GET',
                path: target.toString(),
                headers: {
                    Host: target.host,
                    Connection: 'close',
                },
                timeout: 5000,
            }, res => {
                const statusCode = res.statusCode ?? 0;
                res.resume();
                res.on('end', () => {
                    if (statusCode === 200) {
                        resolve({ ok: true, detail: 'HTTP /health 返回 200' });
                        return;
                    }
                    resolve({ ok: false, detail: `HTTP ${statusCode}` });
                });
            });

            req.on('timeout', () => {
                req.destroy();
                resolve({ ok: false, detail: '请求超时' });
            });
            req.on('error', error => {
                resolve({ ok: false, detail: error.message });
            });
            req.end();
        });
    }

    private requestHttpsHealthViaProxy(target: URL): Promise<{ ok: boolean; detail: string }> {
        return new Promise(resolve => {
            const proxySocket = net.createConnection({ host: '127.0.0.1', port: this.getEffectivePort() });
            let settled = false;
            let connectResponse = '';
            let tlsSocket: tls.TLSSocket | undefined;

            const finish = (result: { ok: boolean; detail: string }) => {
                if (settled) {
                    return;
                }
                settled = true;
                proxySocket.removeAllListeners();
                proxySocket.destroy();
                tlsSocket?.removeAllListeners();
                tlsSocket?.destroy();
                resolve(result);
            };

            proxySocket.setTimeout(5000);
            proxySocket.once('connect', () => {
                const targetPort = target.port || '443';
                const authority = `${target.hostname}:${targetPort}`;
                proxySocket.write(`CONNECT ${authority} HTTP/1.1\r\nHost: ${authority}\r\nProxy-Connection: Keep-Alive\r\n\r\n`);
            });
            proxySocket.on('data', (chunk: Buffer) => {
                connectResponse += chunk.toString('utf8');
                if (!connectResponse.includes('\r\n\r\n')) {
                    return;
                }

                const [headers] = connectResponse.split('\r\n\r\n', 1);
                const statusLine = headers.split('\r\n', 1)[0] ?? '';
                const match = statusLine.match(/^HTTP\/1\.[01]\s+(\d{3})/i);
                if (!match) {
                    finish({ ok: false, detail: 'CONNECT 响应无效' });
                    return;
                }

                if (Number(match[1]) !== 200) {
                    finish({ ok: false, detail: `CONNECT 返回 ${match[1]}` });
                    return;
                }

                proxySocket.removeAllListeners('data');
                proxySocket.removeAllListeners('timeout');
                proxySocket.removeAllListeners('error');

                tlsSocket = tls.connect({
                    socket: proxySocket,
                    servername: target.hostname,
                    rejectUnauthorized: false,
                }, () => {
                    const pathWithQuery = `${target.pathname || '/'}${target.search}`;
                    tlsSocket?.write(`GET ${pathWithQuery} HTTP/1.1\r\nHost: ${target.host}\r\nConnection: close\r\n\r\n`);
                });

                let response = '';
                tlsSocket.setTimeout(5000);
                tlsSocket.on('data', tlsChunk => {
                    response += tlsChunk.toString('utf8');
                    const statusMatch = response.match(/^HTTP\/1\.[01]\s+(\d{3})/i);
                    if (!statusMatch) {
                        return;
                    }
                    const statusCode = Number(statusMatch[1]);
                    finish(statusCode === 200
                        ? { ok: true, detail: 'HTTPS /health 返回 200' }
                        : { ok: false, detail: `HTTPS ${statusCode}` });
                });
                tlsSocket.once('timeout', () => finish({ ok: false, detail: 'TLS 请求超时' }));
                tlsSocket.once('error', error => finish({ ok: false, detail: error.message }));
                tlsSocket.once('end', () => {
                    if (!settled) {
                        finish({ ok: false, detail: 'TLS 连接提前关闭' });
                    }
                });
            });
            proxySocket.once('timeout', () => finish({ ok: false, detail: 'CONNECT 超时' }));
            proxySocket.once('error', error => finish({ ok: false, detail: error.message }));
        });
    }
}