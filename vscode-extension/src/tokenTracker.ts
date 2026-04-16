import * as vscode from 'vscode';
import * as https from 'https';
import * as http from 'http';
import * as os from 'os';
import { getNormalizedAppName, MonitorConfig } from './config';

export interface UsageRecord {
    vendor: string;
    model: string;
    endpoint: string;
    promptTokens: number;
    completionTokens: number;
    totalTokens: number;
    requestTime: string;
    source: string;
    sourceApp: string;
    requestId?: string;
    modelFamily?: string;
    modelVersion?: string;
}

export interface TrackerRuntimeStatus {
    pendingQueueLength: number;
    isReporting: boolean;
    lastStatsSyncAt?: string;
    lastStatsSyncError?: string;
    lastStatsSyncErrorCategory?: TrackerErrorCategory;
    lastStatsSyncHttpStatus?: number;
    lastStatsSyncErrorCode?: string;
    totalFailed: number;
    totalReported: number;
    /** @deprecated no longer used */
    lastCollectAttemptAt?: string;
    /** @deprecated no longer used */
    lastCollectSuccessAt?: string;
    /** @deprecated no longer used */
    lastCollectError?: string;
    /** @deprecated no longer used */
    lastCollectErrorCategory?: TrackerErrorCategory;
    /** @deprecated no longer used */
    lastCollectHttpStatus?: number;
    /** @deprecated no longer used */
    lastCollectErrorCode?: string;
}

export type TrackerErrorCategory =
    | 'identity_conflict'
    | 'timeout'
    | 'server_unreachable'
    | 'http_error'
    | 'config_incomplete'
    | 'unknown';

class TrackerRequestError extends Error {
    statusCode?: number;
    responseBody?: string;
    errorCode?: string;
    category: TrackerErrorCategory;

    constructor(
        message: string,
        options: {
            statusCode?: number;
            responseBody?: string;
            errorCode?: string;
            category?: TrackerErrorCategory;
        } = {},
    ) {
        super(message);
        this.name = 'TrackerRequestError';
        this.statusCode = options.statusCode;
        this.responseBody = options.responseBody;
        this.errorCode = options.errorCode;
        this.category = options.category ?? 'unknown';
    }
}

interface TrackerFailureSnapshot {
    message: string;
    category: TrackerErrorCategory;
    statusCode?: number;
    errorCode?: string;
}

/**
 * TokenTracker is a **read-only** stats viewer.
 * All data collection/reporting is handled by ai-monitor.exe (MITM proxy).
 * This class only fetches aggregated stats from the server for dashboard display.
 */
export class TokenTracker {
    private syncTimer: ReturnType<typeof setInterval> | null = null;
    private initialSyncTimer: ReturnType<typeof setTimeout> | null = null;
    private config: MonitorConfig;
    private clientId: string;
    private readonly appScopeKey: string;
    private lastStatsSyncAt?: string;
    private lastStatsSyncError?: string;
    private lastStatsSyncErrorCategory?: TrackerErrorCategory;
    private lastStatsSyncHttpStatus?: number;
    private lastStatsSyncErrorCode?: string;
    private authToken?: string;

    // Stats (observable by StatusBar)
    public todayTokens = 0;
    public todayRequests = 0;
    public totalReported = 0;
    public totalFailed = 0;
    public selectedDays = 1;

    // Breakdown stats (observable by Dashboard) — populated by server sync
    private sourceBreakdown: Map<string, number> = new Map();
    private appBreakdown: Map<string, number> = new Map();
    private modelBreakdown: Map<string, number> = new Map();

    constructor(config: MonitorConfig, _globalState: any, _eventBus?: any) {
        this.config = config;
        this.appScopeKey = getNormalizedAppName();
        this.clientId = this.buildClientId(config);
    }

    private buildClientId(config: MonitorConfig = this.config): string {
        const userId = (config.userId || '').trim() || 'anonymous';
        return `${userId}@${os.hostname()}#${this.appScopeKey}`;
    }

    start() {
        // Sync stats from server periodically (every 30s)
        this.syncTimer = setInterval(() => {
            this.syncStats().catch(console.error);
        }, 30_000);

        // Initial sync after 2s
        this.initialSyncTimer = setTimeout(() => {
            this.initialSyncTimer = null;
            this.syncStats().catch(console.error);
        }, 2000);
    }

    stop() {
        if (this.syncTimer) {
            clearInterval(this.syncTimer);
            this.syncTimer = null;
        }
        if (this.initialSyncTimer) {
            clearTimeout(this.initialSyncTimer);
            this.initialSyncTimer = null;
        }
    }

    updateConfig(config: MonitorConfig) {
        this.config = config;
        this.clientId = this.buildClientId(config);
        // Re-sync from server with new identity
        this.syncStats().catch(console.error);
    }

    setAuthToken(token: string | undefined) {
        this.authToken = token;
    }

    /**
     * @deprecated No-op. All data collection is handled by ai-monitor.exe.
     * Kept for backward compatibility with code that still calls it.
     */
    addRecord(_record: UsageRecord) {
        // Intentionally empty — ai-monitor MITM captures all traffic,
        // extension no longer reports to avoid duplicate records.
    }

    public getBreakdown() {
        return {
            sources: Object.fromEntries(this.sourceBreakdown),
            apps: Object.fromEntries(this.appBreakdown),
            models: Object.fromEntries(this.modelBreakdown),
        };
    }

    public getRuntimeStatus(): TrackerRuntimeStatus {
        return {
            pendingQueueLength: 0,
            isReporting: false,
            lastStatsSyncAt: this.lastStatsSyncAt,
            lastStatsSyncError: this.lastStatsSyncError,
            lastStatsSyncErrorCategory: this.lastStatsSyncErrorCategory,
            lastStatsSyncHttpStatus: this.lastStatsSyncHttpStatus,
            lastStatsSyncErrorCode: this.lastStatsSyncErrorCode,
            totalFailed: this.totalFailed,
            totalReported: this.totalReported,
        };
    }

    /**
     * @deprecated No-op kept for backward compatibility.
     */
    async flushOfflineQueue(): Promise<void> {}

    /**
     * @deprecated No-op kept for backward compatibility.
     */
    async flush(): Promise<void> {}

    setSelectedDays(days: number): void {
        this.selectedDays = days;
    }

    async syncStats(): Promise<void> {
        try {
            if (!this.config.serverUrl || !this.config.userId || !this.config.userName) return;
            const url = `${this.config.serverUrl}/api/clients/my-stats?user_id=${encodeURIComponent(this.config.userId)}&user_name=${encodeURIComponent(this.config.userName)}&department=${encodeURIComponent(this.config.department || '')}&days=${this.selectedDays}`;
            const res = await this.getJSON<{ today_tokens: number; today_requests: number }>(url);
            if (res && typeof res.today_tokens === 'number') {
                this.lastStatsSyncAt = new Date().toISOString();
                this.lastStatsSyncError = undefined;
                this.lastStatsSyncErrorCategory = undefined;
                this.lastStatsSyncHttpStatus = undefined;
                this.lastStatsSyncErrorCode = undefined;

                this.todayTokens = res.today_tokens;
                this.todayRequests = res.today_requests;
                this.totalReported = res.today_tokens;
            }
        } catch (e) {
            const failure = this.summarizeFailure(e, 'my-stats 同步失败');
            this.lastStatsSyncError = failure.message;
            this.lastStatsSyncErrorCategory = failure.category;
            this.lastStatsSyncHttpStatus = failure.statusCode;
            this.lastStatsSyncErrorCode = failure.errorCode;
            console.error('[TokenTracker] syncStats failed:', e);
        }
    }

    private summarizeFailure(error: unknown, prefix: string): TrackerFailureSnapshot {
        if (error instanceof TrackerRequestError) {
            const detail = this.extractErrorDetail(error.responseBody);
            const errorCode = detail?.code || error.errorCode;
            const category = this.categorizeError(error, errorCode);
            const message = detail?.message
                ? `${prefix}：${detail.message}`
                : `${prefix}：${error.message}`;
            return { message, category, statusCode: error.statusCode, errorCode };
        }
        const fallbackMessage = error instanceof Error ? error.message : String(error);
        return { message: `${prefix}：${fallbackMessage}`, category: this.categorizeError(error) };
    }

    private categorizeError(error: unknown, errorCode?: string): TrackerErrorCategory {
        if (errorCode === 'identity_conflict') return 'identity_conflict';
        if (error instanceof TrackerRequestError) {
            if (error.category !== 'unknown') return error.category;
            if (typeof error.statusCode === 'number') return 'http_error';
        }
        const message = error instanceof Error ? error.message : String(error);
        if (/timeout/i.test(message)) return 'timeout';
        if (/ECONNREFUSED|ENOTFOUND|EAI_AGAIN|socket hang up|fetch failed|connect/i.test(message)) return 'server_unreachable';
        if (/HTTP\s+\d+/i.test(message)) return 'http_error';
        return 'unknown';
    }

    private extractErrorDetail(body?: string): { code?: string; message?: string } | undefined {
        if (!body) return undefined;
        try {
            const parsed = JSON.parse(body) as { detail?: string | { code?: string; message?: string } };
            if (typeof parsed.detail === 'string') return { message: parsed.detail };
            if (parsed.detail && typeof parsed.detail === 'object') {
                return { code: parsed.detail.code, message: parsed.detail.message };
            }
        } catch {
            return undefined;
        }
        return undefined;
    }

    private getJSON<T>(url: string): Promise<T> {
        return new Promise((resolve, reject) => {
            const parsed = new URL(url);
            const options: Record<string, any> = {
                hostname: parsed.hostname,
                port: parsed.port,
                path: parsed.pathname + parsed.search,
                method: 'GET',
                timeout: 10_000,
                headers: {
                    ...(this.config.apiKey ? { 'X-API-Key': this.config.apiKey } : {}),
                    ...(this.authToken ? { Authorization: `Bearer ${this.authToken}` } : {}),
                },
            };
            const transport = parsed.protocol === 'https:' ? https : http;
            const req = transport.request(options, (res) => {
                let body = '';
                res.on('data', (chunk) => { body += chunk; });
                res.on('end', () => {
                    if (res.statusCode && res.statusCode >= 200 && res.statusCode < 300) {
                        try { resolve(JSON.parse(body)); } catch (e) { reject(e); }
                    } else {
                        reject(new TrackerRequestError(`HTTP ${res.statusCode}: ${body}`, {
                            statusCode: res.statusCode,
                            responseBody: body,
                        }));
                    }
                });
            });
            req.on('error', reject);
            req.on('timeout', () => { req.destroy(); reject(new TrackerRequestError('GET JSON timeout', { category: 'timeout' })); });
            req.end();
        });
    }
}
