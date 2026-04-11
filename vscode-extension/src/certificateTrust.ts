import * as fs from 'fs';
import * as path from 'path';
import { execFileSync } from 'child_process';

export interface WindowsCertificateTrustStatus {
    trusted: boolean | null;
    certPath: string;
    fileThumbprint?: string;
    detail?: string;
    trustSource?: 'powershell' | 'certutil';
}

export interface CertificateTrustLogger {
    info(message: string): void;
    warn(message: string): void;
}

export interface CertificateTrustDependencies {
    platform?: NodeJS.Platform;
    fileExists?: (targetPath: string) => boolean;
    readFileThumbprint?: (certPath: string) => string | null;
    readRootThumbprints?: () => string[] | null;
    isTrustedByCertutil?: (thumbprint: string) => boolean;
    logger?: CertificateTrustLogger;
}

type ExecFileSyncLike = (file: string, args: string[], options: { encoding: 'utf8'; windowsHide?: boolean }) => string;

const ROOT_STORE_THUMBPRINTS_COMMAND = 'Get-ChildItem Cert:\\CurrentUser\\Root | Select-Object -ExpandProperty Thumbprint';

export function getWindowsCertificateTrustStatus(
    certPath: string,
    deps: CertificateTrustDependencies = {},
): WindowsCertificateTrustStatus {
    const platform = deps.platform ?? process.platform;
    const logger = deps.logger ?? console;
    const fileExists = deps.fileExists ?? fs.existsSync;
    const readFileThumbprint = deps.readFileThumbprint ?? readWindowsCertificateFileThumbprint;
    const readRootThumbprints = deps.readRootThumbprints ?? readWindowsCurrentUserRootThumbprints;
    const isTrustedByCertutil = deps.isTrustedByCertutil ?? isWindowsCertificateTrustedByCertutil;

    if (platform !== 'win32') {
        return { trusted: null, certPath };
    }

    if (!certPath || !fileExists(certPath)) {
        return { trusted: false, certPath };
    }

    try {
        const fileThumbprint = readFileThumbprint(certPath)?.toUpperCase();
        if (!fileThumbprint) {
            logger.warn(`[certificateTrust] Failed to read local CA thumbprint: ${certPath}`);
            return {
                trusted: null,
                certPath,
                detail: `${certPath}；无法读取本地 CA 证书指纹。`,
            };
        }

        const trustedThumbprints = readRootThumbprints();
        const powershellMatch = Boolean(trustedThumbprints?.includes(fileThumbprint));
        if (powershellMatch) {
            logger.info(`[certificateTrust] trusted via powershell thumbprint match: ${fileThumbprint}`);
            return {
                trusted: true,
                certPath,
                fileThumbprint,
                trustSource: 'powershell',
            };
        }

        const certutilMatch = isTrustedByCertutil(fileThumbprint);
        logger.info(`[certificateTrust] check result thumbprint=${fileThumbprint} powershellMatch=${powershellMatch} certutilMatch=${certutilMatch}`);
        if (certutilMatch) {
            return {
                trusted: true,
                certPath,
                fileThumbprint,
                trustSource: 'certutil',
            };
        }

        return {
            trusted: false,
            certPath,
            fileThumbprint,
            detail: `${certPath}；当前 CA 指纹 ${fileThumbprint}。如果你刚更新扩展，请完全退出并重新打开 IDE 后再重试。`,
        };
    } catch (error) {
        const message = error instanceof Error ? error.message : '未知错误';
        logger.warn(`[certificateTrust] certificate trust check failed: ${message}`);
        return {
            trusted: null,
            certPath,
            detail: `${certPath}；证书校验执行失败：${message}`,
        };
    }
}

export function readWindowsCertificateFileThumbprint(
    certPath: string,
    execFileSyncImpl: ExecFileSyncLike = execFileSync as ExecFileSyncLike,
    powershellPath: string = getWindowsPowerShellPath(),
): string | null {
    const escapedPath = certPath.replace(/'/g, "''");
    const output = execFileSyncImpl(
        powershellPath,
        [
            '-NoProfile',
            '-NonInteractive',
            '-Command',
            `(New-Object System.Security.Cryptography.X509Certificates.X509Certificate2('${escapedPath}')).Thumbprint`,
        ],
        { encoding: 'utf8', windowsHide: true },
    ).trim();
    return output || null;
}

export function readWindowsCurrentUserRootThumbprints(
    execFileSyncImpl: ExecFileSyncLike = execFileSync as ExecFileSyncLike,
    powershellPath: string = getWindowsPowerShellPath(),
): string[] | null {
    const output = execFileSyncImpl(
        powershellPath,
        [
            '-NoProfile',
            '-NonInteractive',
            '-Command',
            ROOT_STORE_THUMBPRINTS_COMMAND,
        ],
        { encoding: 'utf8', windowsHide: true },
    ).trim();
    if (!output) {
        return [];
    }
    return output
        .split(/\r?\n/)
        .map(line => line.trim().toUpperCase())
        .filter(Boolean);
}

export function isWindowsCertificateTrustedByCertutil(
    fileThumbprint: string,
    execFileSyncImpl: ExecFileSyncLike = execFileSync as ExecFileSyncLike,
): boolean {
    const output = execFileSyncImpl(
        'certutil',
        ['-store', '-user', 'Root', fileThumbprint],
        { encoding: 'utf8', windowsHide: true },
    ).toUpperCase();
    return output.includes(`CERT HASH(SHA1): ${fileThumbprint}`)
        || output.includes(`CERT HASH(SHA1): ${formatThumbprintForCertutil(fileThumbprint)}`);
}

export function formatThumbprintForCertutil(fileThumbprint: string): string {
    return fileThumbprint.match(/.{1,2}/g)?.join(' ') ?? fileThumbprint;
}

export function getWindowsPowerShellPath(env: NodeJS.ProcessEnv = process.env): string {
    const systemRoot = env.SYSTEMROOT || 'C:\\Windows';
    return path.join(systemRoot, 'System32', 'WindowsPowerShell', 'v1.0', 'powershell.exe');
}
