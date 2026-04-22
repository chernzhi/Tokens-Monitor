import { TokenTracker } from '../tokenTracker';
import * as http from 'http';
import { EventEmitter } from 'events';

jest.mock('http');
jest.mock('https');

describe('TokenTracker (read-only dashboard)', () => {
    let tracker: TokenTracker;
    let mockGlobalState: any;
    let consoleErrorSpy: jest.SpyInstance;

    const mockConfig = {
        serverUrl: 'http://localhost:8000',
        userId: 'user123',
        userName: 'Test User',
        department: 'Engineering',
        copilotOrg: '',
        apiKey: '',
        authToken: '',
    };

    beforeEach(() => {
        jest.clearAllMocks();
        consoleErrorSpy = jest.spyOn(console, 'error').mockImplementation(() => undefined);
        mockGlobalState = {
            get: jest.fn((_key: string, defaultVal: any) => defaultVal),
            update: jest.fn(),
        };
        tracker = new TokenTracker(mockConfig, mockGlobalState);
    });

    afterEach(() => {
        tracker.stop();
        consoleErrorSpy.mockRestore();
    });

    function mockHttpResponse(statusCode: number, body: string) {
        const requestMock = http.request as unknown as jest.Mock;
        requestMock.mockImplementation((_options: unknown, callback: (res: EventEmitter & { statusCode?: number }) => void) => {
            const response = new EventEmitter() as EventEmitter & { statusCode?: number };
            response.statusCode = statusCode;
            const req = new EventEmitter() as EventEmitter & {
                write: jest.Mock;
                end: () => void;
                destroy: jest.Mock;
            };
            req.write = jest.fn();
            req.destroy = jest.fn();
            req.end = () => {
                callback(response);
                response.emit('data', Buffer.from(body, 'utf8'));
                response.emit('end');
            };
            return req;
        });
    }

    test('should initialize with zero stats', () => {
        expect(tracker.todayTokens).toBe(0);
        expect(tracker.todayRequests).toBe(0);
        expect(tracker.totalReported).toBe(0);
        expect(tracker.totalFailed).toBe(0);
    });

    test('addRecord is a no-op (all data collected by ai-monitor)', () => {
        tracker.addRecord({
            vendor: 'openai',
            model: 'gpt-4',
            endpoint: '/v1/chat/completions',
            promptTokens: 100,
            completionTokens: 50,
            totalTokens: 150,
            requestTime: new Date().toISOString(),
            source: 'vscode-lm',
            sourceApp: 'Visual Studio Code',
        });

        expect(tracker.todayTokens).toBe(0);
        expect(tracker.todayRequests).toBe(0);
    });

    test('flushOfflineQueue is a no-op', async () => {
        await tracker.flushOfflineQueue();
        expect(tracker.getRuntimeStatus().pendingQueueLength).toBe(0);
    });

    test('getRuntimeStatus returns read-only status', () => {
        const status = tracker.getRuntimeStatus();
        expect(status.pendingQueueLength).toBe(0);
        expect(status.isReporting).toBe(false);
        expect(status.totalFailed).toBe(0);
        expect(status.totalReported).toBe(0);
    });

    test('syncStats fetches data from server', async () => {
        mockHttpResponse(200, JSON.stringify({
            today_tokens: 1500,
            today_requests: 10,
        }));

        await tracker.syncStats();

        expect(tracker.todayTokens).toBe(1500);
        expect(tracker.todayRequests).toBe(10);
        expect(tracker.totalReported).toBe(1500);
    });

    test('syncStats handles server errors gracefully', async () => {
        mockHttpResponse(500, 'Internal Server Error');

        await tracker.syncStats();

        const status = tracker.getRuntimeStatus();
        expect(status.lastStatsSyncError).toBeDefined();
        expect(status.lastStatsSyncErrorCategory).toBe('http_error');
    });

    test('syncStats skips when config is incomplete', async () => {
        const incompleteTracker = new TokenTracker({
            ...mockConfig,
            userName: '',
        }, mockGlobalState);

        await incompleteTracker.syncStats();

        expect(incompleteTracker.todayTokens).toBe(0);
        incompleteTracker.stop();
    });

    test('updateConfig triggers a re-sync', () => {
        mockHttpResponse(200, JSON.stringify({ today_tokens: 0, today_requests: 0 }));

        tracker.updateConfig({
            ...mockConfig,
            userId: 'user456',
            userName: 'New User',
        });

        expect(tracker.todayTokens).toBe(0);
    });

    test('setSelectedDays updates the day range', () => {
        tracker.setSelectedDays(7);
        expect(tracker.selectedDays).toBe(7);
    });

    test('setAuthToken stores the token', () => {
        tracker.setAuthToken('test-token-123');
        // No direct getter, but it should not throw
        expect(() => tracker.setAuthToken(undefined)).not.toThrow();
    });
});
