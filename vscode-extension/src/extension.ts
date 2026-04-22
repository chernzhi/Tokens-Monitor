import * as vscode from 'vscode';
import { TokenTracker } from './tokenTracker';
import { StatusBarManager } from './statusBar';
import { DashboardProvider } from './dashboard';
import { registerChatParticipant } from './chatParticipant';
import { registerTools } from './tools';
import { CopilotMetrics } from './copilotMetrics';
import { DEFAULT_SERVER_URL, getConfig } from './config';
import { AUTH_SESSION_SECRET_KEY, authSessionMatchesConfig, parseAuthSession } from './authSession';
import { checkForUpdates } from './updater';

const LEGACY_DEFAULT_SERVER_URL = 'http://192.168.0.135:8000';

async function applyTrackerAuth(
    secrets: vscode.SecretStorage,
    cfg: import('./config').MonitorConfig,
    tracker: TokenTracker,
) {
    const authSession = parseAuthSession(await secrets.get(AUTH_SESSION_SECRET_KEY));
    if (authSession && authSessionMatchesConfig(authSession, cfg)) {
        tracker.setAuthToken(authSession.token);
    } else if (cfg.authToken) {
        tracker.setAuthToken(cfg.authToken);
    } else {
        tracker.setAuthToken(undefined);
    }
}

export async function activate(context: vscode.ExtensionContext) {
    const cfgSection = vscode.workspace.getConfiguration('aiTokenMonitor');

    const currentServerUrl = (cfgSection.get<string>('serverUrl', '') || '').replace(/\/+$/, '');
    if (!currentServerUrl || currentServerUrl === LEGACY_DEFAULT_SERVER_URL) {
        await cfgSection.update('serverUrl', DEFAULT_SERVER_URL, vscode.ConfigurationTarget.Global);
    }

    const cfg = getConfig();

    // TokenTracker: read-only stats viewer, fetches data from server
    const tracker = new TokenTracker(cfg, context.globalState);
    await applyTrackerAuth(context.secrets, cfg, tracker);
    tracker.start();
    context.subscriptions.push({ dispose: () => tracker.stop() });

    // Status bar
    const statusBar = new StatusBarManager(tracker);
    statusBar.show();
    context.subscriptions.push({ dispose: () => statusBar.dispose() });

    // Chat Participant: @otw (usage captured by ai-monitor MITM, not by extension)
    registerChatParticipant(context, tracker);

    // LM Tools (usage captured by ai-monitor MITM, not by extension)
    registerTools(context, tracker);

    // Dashboard WebView (pure data display)
    const extensionVersion = context.extension.packageJSON.version ?? '0.0.0';
    const dashboardProvider = new DashboardProvider(
        context.extensionUri, tracker, cfg, context.globalState, context.secrets, undefined, extensionVersion
    );
    context.subscriptions.push(
        vscode.window.registerWebviewViewProvider('tokenMonitor.dashboard', dashboardProvider)
    );

    // Commands
    context.subscriptions.push(
        vscode.commands.registerCommand('tokenMonitor.showDashboard', () => {
            vscode.commands.executeCommand('tokenMonitor.dashboard.focus');
        }),
        vscode.commands.registerCommand('tokenMonitor.newChat', () => {
            vscode.commands.executeCommand('workbench.action.chat.open', { query: '@otw ' });
        }),
        vscode.commands.registerCommand('tokenMonitor.checkUpdate', async () => {
            return await checkForUpdates(context, getConfig().serverUrl, true);
        })
    );

    // Copilot Metrics API (if org is configured)
    if (cfg.copilotOrg) {
        const metrics = new CopilotMetrics(cfg, tracker, context.secrets);
        metrics.startPolling();
        context.subscriptions.push({ dispose: () => metrics.stopPolling() });
    }

    // Listen for config changes
    context.subscriptions.push(
        vscode.workspace.onDidChangeConfiguration(async e => {
            if (e.affectsConfiguration('aiTokenMonitor')) {
                const newCfg = getConfig();
                await applyTrackerAuth(context.secrets, newCfg, tracker);
                tracker.updateConfig(newCfg);
                statusBar.refresh();
                dashboardProvider.updateConfig(newCfg);
            }
        })
    );

    // First-run setup
    if (!cfg.userId) {
        const action = await vscode.window.showInformationMessage(
            '腾轩 AI 监控: 请先在 ai-monitor 客户端中完成注册，扩展将自动从服务端获取使用数据。',
            '打开监控面板'
        );
        if (action === '打开监控面板') {
            vscode.commands.executeCommand('tokenMonitor.dashboard.focus');
        }
    }

    // One-time tip
    const tipShown = context.globalState.get<boolean>('monitorTipShown', false);
    if (!tipShown) {
        context.globalState.update('monitorTipShown', true);
        const action = await vscode.window.showInformationMessage(
            '腾轩 AI 监控已启动。所有 AI 用量数据由后台 ai-monitor 服务采集，本扩展仅作为看板显示统计数据。',
            '打开面板',
        );
        if (action === '打开面板') {
            vscode.commands.executeCommand('tokenMonitor.dashboard.focus');
        }
    }

    // Self-update check (non-blocking)
    void checkForUpdates(context, cfg.serverUrl);
}

export function deactivate() {}
