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
    /** 优先于共享 config.json 的显式工作区项（如设置 aiTokenMonitor.authToken） */
    authToken: string;
}

interface IdentityInfo {
    server_url?: string;
    user_id?: string;
    user_name?: string;
    department?: string;
    api_key?: string;
    /** 与 ai-monitor 配置同步，供仅读 API（如 my-stats）在未用扩展密钥区登录时回退 */
    auth_token?: string;
}

let _identityCache: IdentityInfo | null | undefined; // undefined = not yet loaded

/** Reset the identity cache (for testing only). */
export function _resetIdentityCache(): void {
    _identityCache = undefined;
}

export const DEFAULT_SERVER_URL = 'https://otw.tech:59889';

function loadIdentity(): IdentityInfo | null {
    if (_identityCache !== undefined) { return _identityCache; }
    const appData = process.env.APPDATA;
    if (!appData) { _identityCache = null; return null; }

    const configPath = path.join(appData, 'ai-monitor', 'config.json');
    try {
        const raw = fs.readFileSync(configPath, 'utf8');
        _identityCache = parseJSONWithLineComments<IdentityInfo>(raw);
        return _identityCache;
    } catch {
        // Legacy fallback for older ai-monitor installs that wrote identity.json.
    }

    try {
        const identityPath = path.join(appData, 'ai-monitor', 'identity.json');
        const raw = fs.readFileSync(identityPath, 'utf8');
        _identityCache = parseJSONWithLineComments<IdentityInfo>(raw);
    } catch {
        _identityCache = null;
    }
    return _identityCache;
}

function parseJSONWithLineComments<T>(raw: string): T {
    return JSON.parse(stripJSONLineComments(raw)) as T;
}

function stripJSONLineComments(raw: string): string {
    let out = '';
    let inString = false;
    let escaped = false;
    for (let i = 0; i < raw.length; i++) {
        const ch = raw[i];
        if (inString) {
            out += ch;
            if (escaped) {
                escaped = false;
            } else if (ch === '\\') {
                escaped = true;
            } else if (ch === '"') {
                inString = false;
            }
            continue;
        }
        if (ch === '"') {
            inString = true;
            out += ch;
            continue;
        }
        if (ch === '/' && raw[i + 1] === '/') {
            while (i < raw.length && raw[i] !== '\n') {
                i++;
            }
            if (i < raw.length) {
                out += raw[i];
            }
            continue;
        }
        out += ch;
    }
    return out;
}

export function getConfig(): MonitorConfig {
    const cfg = vscode.workspace.getConfiguration('aiTokenMonitor');
    const identity = loadIdentity();
    const serverUrl = cfg.get<string>('serverUrl', '') || identity?.server_url || DEFAULT_SERVER_URL;
    return {
        serverUrl: serverUrl.replace(/\/+$/, ''),
        userId: cfg.get<string>('userId', '') || identity?.user_id || '',
        userName: cfg.get<string>('userName', '') || identity?.user_name || '',
        department: cfg.get<string>('department', '') || identity?.department || '',
        copilotOrg: cfg.get<string>('copilotOrg', ''),
        apiKey: cfg.get<string>('apiKey', '') || identity?.api_key || '',
        authToken: (cfg.get<string>('authToken', '') || identity?.auth_token || '').trim(),
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
