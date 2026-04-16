import * as vscode from 'vscode';
import { TokenTracker } from './tokenTracker';

/**
 * @otw Chat Participant — a thin wrapper around VS Code's Language Model API.
 * Token usage is NOT reported by the extension; ai-monitor.exe captures it via MITM.
 */
export function registerChatParticipant(context: vscode.ExtensionContext, _tracker: TokenTracker, _eventBus?: any) {
    const participant = vscode.chat.createChatParticipant('token-monitor.otw', async (
        request: vscode.ChatRequest,
        chatContext: vscode.ChatContext,
        response: vscode.ChatResponseStream,
        token: vscode.CancellationToken
    ) => {
        const model = request.model;

        if (!request.prompt.trim()) {
            response.markdown('请输入你的问题或指令。');
            return;
        }

        const messages: vscode.LanguageModelChatMessage[] = [];

        for (const turn of chatContext.history) {
            if (turn instanceof vscode.ChatRequestTurn) {
                messages.push(vscode.LanguageModelChatMessage.User(turn.prompt));
            } else if (turn instanceof vscode.ChatResponseTurn) {
                const parts: string[] = [];
                for (const part of turn.response) {
                    if (part instanceof vscode.ChatResponseMarkdownPart) {
                        parts.push(part.value.value);
                    }
                }
                if (parts.length > 0) {
                    messages.push(vscode.LanguageModelChatMessage.Assistant(parts.join('')));
                }
            }
        }

        messages.push(vscode.LanguageModelChatMessage.User(request.prompt));

        try {
            const chatResponse = await model.sendRequest(messages, {}, token);
            for await (const chunk of chatResponse.text) {
                response.markdown(chunk);
            }
        } catch (err) {
            if (err instanceof vscode.LanguageModelError) {
                response.markdown(`Error: ${err.message}`);
            }
            throw err;
        }
    });

    participant.iconPath = new vscode.ThemeIcon('pulse');
    context.subscriptions.push(participant);
}
