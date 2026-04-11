import { buildProxyEnvironmentDiagnosis } from '../proxyManager';

describe('buildProxyEnvironmentDiagnosis', () => {
    test('prefers configured upstream proxy when provided', () => {
        const diagnosis = buildProxyEnvironmentDiagnosis({
            configuredUpstream: 'http://127.0.0.1:7890',
            desktopProcesses: ['sing-box.exe'],
        });

        expect(diagnosis.allowTakeover).toBe(true);
        expect(diagnosis.kind).toBe('configured-upstream');
        expect(diagnosis.upstreamProxy).toBe('http://127.0.0.1:7890');
        expect(diagnosis.upstreamSource).toBe('config');
    });

    test('allows configured upstream proxy even when tun adapter exists', () => {
        const diagnosis = buildProxyEnvironmentDiagnosis({
            configuredUpstream: 'socks5://127.0.0.1:8089',
            tunAdapters: ['corp-tun (Wintun Userspace Tunnel)'],
        });

        expect(diagnosis.allowTakeover).toBe(true);
        expect(diagnosis.kind).toBe('configured-upstream');
        expect(diagnosis.upstreamProxy).toBe('socks5://127.0.0.1:8089');
    });

    test('blocks takeover when PAC is detected without explicit upstream', () => {
        const diagnosis = buildProxyEnvironmentDiagnosis({
            systemPacUrl: 'http://proxy.example.com/proxy.pac',
        });

        expect(diagnosis.allowTakeover).toBe(false);
        expect(diagnosis.kind).toBe('system-pac');
        expect(diagnosis.recommendedAction).toContain('HTTP/SOCKS');
    });

    test('blocks sing-box when no reusable upstream is found', () => {
        const diagnosis = buildProxyEnvironmentDiagnosis({
            desktopProcesses: ['sing-box.exe'],
        });

        expect(diagnosis.allowTakeover).toBe(false);
        expect(diagnosis.kind).toBe('desktop-proxy');
        expect(diagnosis.summary).toContain('sing-box');
    });

    test('allows proxifier without forcing an explicit upstream', () => {
        const diagnosis = buildProxyEnvironmentDiagnosis({
            desktopProcesses: ['proxifier.exe'],
        });

        expect(diagnosis.allowTakeover).toBe(true);
        expect(diagnosis.kind).toBe('desktop-proxy');
        expect(diagnosis.summary).toContain('Proxifier');
    });

    test('blocks unknown commercial proxy software without an explicit upstream', () => {
        const diagnosis = buildProxyEnvironmentDiagnosis({
            desktopProcesses: ['enterpriseproxy.exe'],
        });

        expect(diagnosis.allowTakeover).toBe(false);
        expect(diagnosis.kind).toBe('desktop-proxy');
        expect(diagnosis.summary).toContain('enterpriseproxy.exe');
    });

    test('still allows takeover when unknown proxy software also exposes an explicit upstream', () => {
        const diagnosis = buildProxyEnvironmentDiagnosis({
            desktopProcesses: ['enterpriseproxy.exe'],
            systemProxy: 'http://127.0.0.1:8888',
        });

        expect(diagnosis.allowTakeover).toBe(true);
        expect(diagnosis.kind).toBe('system-proxy');
        expect(diagnosis.upstreamProxy).toBe('http://127.0.0.1:8888');
    });

    test('allows takeover when a reusable local proxy port is discovered', () => {
        const diagnosis = buildProxyEnvironmentDiagnosis({
            desktopProcesses: ['enterpriseproxy.exe'],
            localDiscoveredProxy: 'socks5://127.0.0.1:10808',
        });

        expect(diagnosis.allowTakeover).toBe(true);
        expect(diagnosis.kind).toBe('desktop-proxy');
        expect(diagnosis.upstreamProxy).toBe('socks5://127.0.0.1:10808');
        expect(diagnosis.upstreamSource).toBe('local-discovery');
    });

    test('blocks high-risk tun adapters before direct takeover', () => {
        const diagnosis = buildProxyEnvironmentDiagnosis({
            tunAdapters: ['sing-box-tun (Wintun Userspace Tunnel)'],
        });

        expect(diagnosis.allowTakeover).toBe(false);
        expect(diagnosis.kind).toBe('tun');
        expect(diagnosis.detail).toContain('sing-box-tun');
    });

    test('still blocks tun even when another local upstream candidate exists', () => {
        const diagnosis = buildProxyEnvironmentDiagnosis({
            tunAdapters: ['corp-tun (Wintun Userspace Tunnel)'],
            localDiscoveredProxy: 'http://127.0.0.1:7890',
        });

        expect(diagnosis.allowTakeover).toBe(false);
        expect(diagnosis.kind).toBe('tun');
    });
});