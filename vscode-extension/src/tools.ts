import * as vscode from 'vscode';
import { TokenTracker } from './tokenTracker';

/**
 * Runs a prompt against the first available language model.
 * Token usage is NOT reported by the extension; ai-monitor.exe captures it via MITM.
 */
async function runTool(
    prompt: string,
    token: vscode.CancellationToken,
): Promise<string> {
    const models = await vscode.lm.selectChatModels({});
    if (models.length === 0) return 'No model available.';
    const model = models[0];

    const messages = [vscode.LanguageModelChatMessage.User(prompt)];
    let output = '';
    const response = await model.sendRequest(messages, {}, token);
    for await (const chunk of response.text) {
        output += chunk;
    }
    return output;
}

export function registerTools(context: vscode.ExtensionContext, _tracker: TokenTracker, _eventBus?: any) {
    try {
        const codeReviewTool = vscode.lm.registerTool('token-monitor-codeReview', {
            async invoke(
                options: vscode.LanguageModelToolInvocationOptions<{ code: string }>,
                token: vscode.CancellationToken
            ): Promise<vscode.LanguageModelToolResult> {
                const code = options.input.code || '';
                const output = await runTool(
                    `Review the following code for bugs, performance issues, and best practices:\n\n\`\`\`\n${code}\n\`\`\``,
                    token,
                );
                return new vscode.LanguageModelToolResult([new vscode.LanguageModelTextPart(output)]);
            },
            async prepareInvocation() {
                return { invocationMessage: 'Reviewing code...' };
            },
        });

        const explainCodeTool = vscode.lm.registerTool('token-monitor-explainCode', {
            async invoke(
                options: vscode.LanguageModelToolInvocationOptions<{ code: string }>,
                token: vscode.CancellationToken
            ): Promise<vscode.LanguageModelToolResult> {
                const code = options.input.code || '';
                const output = await runTool(
                    `Explain what the following code does, step by step:\n\n\`\`\`\n${code}\n\`\`\``,
                    token,
                );
                return new vscode.LanguageModelToolResult([new vscode.LanguageModelTextPart(output)]);
            },
            async prepareInvocation() {
                return { invocationMessage: 'Explaining code...' };
            },
        });

        const generateTestsTool = vscode.lm.registerTool('token-monitor-generateTests', {
            async invoke(
                options: vscode.LanguageModelToolInvocationOptions<{ code: string; language?: string }>,
                token: vscode.CancellationToken
            ): Promise<vscode.LanguageModelToolResult> {
                const code = options.input.code || '';
                const lang = options.input.language || 'the same language';
                const output = await runTool(
                    `Generate unit tests for the following code in ${lang}:\n\n\`\`\`\n${code}\n\`\`\``,
                    token,
                );
                return new vscode.LanguageModelToolResult([new vscode.LanguageModelTextPart(output)]);
            },
            async prepareInvocation() {
                return { invocationMessage: 'Generating tests...' };
            },
        });

        const generateDocsTool = vscode.lm.registerTool('token-monitor-generateDocs', {
            async invoke(
                options: vscode.LanguageModelToolInvocationOptions<{ code: string; style?: string }>,
                token: vscode.CancellationToken
            ): Promise<vscode.LanguageModelToolResult> {
                const code = options.input.code || '';
                const style = options.input.style || 'JSDoc-style';
                const output = await runTool(
                    `Generate ${style} documentation for the following code:\n\n\`\`\`\n${code}\n\`\`\``,
                    token,
                );
                return new vscode.LanguageModelToolResult([new vscode.LanguageModelTextPart(output)]);
            },
            async prepareInvocation() {
                return { invocationMessage: 'Generating documentation...' };
            },
        });

        const refactorSuggestionsTool = vscode.lm.registerTool('token-monitor-refactorSuggestions', {
            async invoke(
                options: vscode.LanguageModelToolInvocationOptions<{ code: string; goal?: string }>,
                token: vscode.CancellationToken
            ): Promise<vscode.LanguageModelToolResult> {
                const code = options.input.code || '';
                const goal = options.input.goal;
                const output = await runTool(
                    `Suggest refactoring improvements for the following code${goal ? ' with the goal of ' + goal : ''}:\n\n\`\`\`\n${code}\n\`\`\``,
                    token,
                );
                return new vscode.LanguageModelToolResult([new vscode.LanguageModelTextPart(output)]);
            },
            async prepareInvocation() {
                return { invocationMessage: 'Analyzing code for refactoring...' };
            },
        });

        context.subscriptions.push(codeReviewTool, explainCodeTool, generateTestsTool, generateDocsTool, refactorSuggestionsTool);
    } catch (err) {
        console.warn('[AI Token Monitor] Failed to register LM tools:', err);
    }
}
