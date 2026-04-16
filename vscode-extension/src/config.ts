import * as vscode from 'vscode';
import * as fs from 'fs';
import * as path from 'path';

export interface MonitorConfig {
    serverUrl: string;
    userId: string;
    userName: string;
    department: string;
    copilotOrg: string;
    apiKey: string;
}

interface IdentityInfo {
    user_id?: string;
    user_name?: string;
    department?: string;
    api_key?: string;
}

let _identityCache: IdentityInfo | null | undefined; // undefined = not yet loaded

/** Reset the identity cache (for testing only). */
export function _resetIdentityCache(): void {
    _identityCache = undefined;
}

export const DEFAULT_SERVER_URL = 'https://otw.tech:59889';

function loadIdentity(): IdentityInfo | null {
    if (_identityCache !== undefined) { return _identityCache; }
    try {
        const appData = process.env.APPDATA;
        if (!appData) { _identityCache = null; return null; }
        const p = path.join(appData, 'ai-monitor', 'identity.json');
        const raw = fs.readFileSync(p, 'utf8');
        _identityCache = JSON.parse(raw) as IdentityInfo;
    } catch {
        _identityCache = null;
    }
    return _identityCache;
}

export function getConfig(): MonitorConfig {
    const cfg = vscode.workspace.getConfiguration('aiTokenMonitor');
    const identity = loadIdentity();
    return {
        serverUrl: cfg.get<string>('serverUrl', DEFAULT_SERVER_URL).replace(/\/+$/, ''),
        userId: cfg.get<string>('userId', '') || identity?.user_id || '',
        userName: cfg.get<string>('userName', '') || identity?.user_name || '',
        department: cfg.get<string>('department', '') || identity?.department || '',
        copilotOrg: cfg.get<string>('copilotOrg', ''),
        apiKey: cfg.get<string>('apiKey', '') || identity?.api_key || '',
    };
}

/** 返回当前编辑器的原始名称，如 "Visual Studio Code"、"Cursor"、"Kiro" */
export function getAppName(): string {
    return vscode.env.appName;
}

/** 将 appName 映射为简短标识符，用于 source 字段拼接 */
export function getNormalizedAppName(): string {
    const name = vscode.env.appName;
    const map: Record<string, string> = {
        'Visual Studio Code': 'vscode',
        'Visual Studio Code - Insiders': 'vscode-insiders',
        'Cursor': 'cursor',
        'Kiro': 'kiro',
        'Windsurf': 'windsurf',
        'VSCodium': 'vscodium',
        'Trae': 'trae',
    };
    return map[name] || name.toLowerCase().replace(/\s+/g, '-');
}
