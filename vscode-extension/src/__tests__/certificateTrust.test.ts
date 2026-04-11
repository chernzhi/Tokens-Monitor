import {
    formatThumbprintForCertutil,
    getWindowsCertificateTrustStatus,
    readWindowsCurrentUserRootThumbprints,
} from '../certificateTrust';

describe('certificateTrust', () => {
    test('uses powershell thumbprint match when available', () => {
        const info = jest.fn();
        const warn = jest.fn();

        const result = getWindowsCertificateTrustStatus('C:/Users/test/AppData/Roaming/ai-monitor/ca.crt', {
            platform: 'win32',
            fileExists: () => true,
            readFileThumbprint: () => 'abc123',
            readRootThumbprints: () => ['ABC123', 'DEF456'],
            isTrustedByCertutil: () => false,
            logger: { info, warn },
        });

        expect(result.trusted).toBe(true);
        expect(result.trustSource).toBe('powershell');
        expect(result.fileThumbprint).toBe('ABC123');
        expect(info).toHaveBeenCalledWith('[certificateTrust] trusted via powershell thumbprint match: ABC123');
        expect(warn).not.toHaveBeenCalled();
    });

    test('falls back to certutil when powershell does not match', () => {
        const info = jest.fn();

        const result = getWindowsCertificateTrustStatus('C:/Users/test/AppData/Roaming/ai-monitor/ca.crt', {
            platform: 'win32',
            fileExists: () => true,
            readFileThumbprint: () => '445b12',
            readRootThumbprints: () => ['AAAAAA'],
            isTrustedByCertutil: () => true,
            logger: { info, warn: jest.fn() },
        });

        expect(result.trusted).toBe(true);
        expect(result.trustSource).toBe('certutil');
        expect(info).toHaveBeenCalledWith('[certificateTrust] check result thumbprint=445B12 powershellMatch=false certutilMatch=true');
    });

    test('returns warning detail when not trusted', () => {
        const result = getWindowsCertificateTrustStatus('C:/Users/test/AppData/Roaming/ai-monitor/ca.crt', {
            platform: 'win32',
            fileExists: () => true,
            readFileThumbprint: () => '445b12',
            readRootThumbprints: () => [],
            isTrustedByCertutil: () => false,
            logger: { info: jest.fn(), warn: jest.fn() },
        });

        expect(result.trusted).toBe(false);
        expect(result.detail).toContain('当前 CA 指纹 445B12');
    });

    test('returns null trusted with detail when thumbprint read fails', () => {
        const warn = jest.fn();

        const result = getWindowsCertificateTrustStatus('C:/Users/test/AppData/Roaming/ai-monitor/ca.crt', {
            platform: 'win32',
            fileExists: () => true,
            readFileThumbprint: () => null,
            logger: { info: jest.fn(), warn },
        });

        expect(result.trusted).toBeNull();
        expect(result.detail).toContain('无法读取本地 CA 证书指纹');
        expect(warn).toHaveBeenCalled();
    });

    test('returns null trusted with detail when validation throws', () => {
        const warn = jest.fn();

        const result = getWindowsCertificateTrustStatus('C:/Users/test/AppData/Roaming/ai-monitor/ca.crt', {
            platform: 'win32',
            fileExists: () => true,
            readFileThumbprint: () => '445b12',
            readRootThumbprints: () => {
                throw new Error('boom');
            },
            logger: { info: jest.fn(), warn },
        });

        expect(result.trusted).toBeNull();
        expect(result.detail).toContain('证书校验执行失败：boom');
        expect(warn).toHaveBeenCalledWith('[certificateTrust] certificate trust check failed: boom');
    });

    test('uses the correct powershell certificate store path', () => {
        const execFileSyncImpl = jest.fn().mockReturnValue('ABC123\nDEF456');

        const result = readWindowsCurrentUserRootThumbprints(execFileSyncImpl as any, 'C:/Windows/System32/WindowsPowerShell/v1.0/powershell.exe');

        expect(result).toEqual(['ABC123', 'DEF456']);
        expect(execFileSyncImpl).toHaveBeenCalledWith(
            'C:/Windows/System32/WindowsPowerShell/v1.0/powershell.exe',
            [
                '-NoProfile',
                '-NonInteractive',
                '-Command',
                'Get-ChildItem Cert:\\CurrentUser\\Root | Select-Object -ExpandProperty Thumbprint',
            ],
            { encoding: 'utf8', windowsHide: true },
        );
    });

    test('formats certutil thumbprints as spaced pairs', () => {
        expect(formatThumbprintForCertutil('445B12BE')).toBe('44 5B 12 BE');
    });
});