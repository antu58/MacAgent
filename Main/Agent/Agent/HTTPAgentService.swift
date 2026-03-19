//
//  HTTPAgentService.swift
//  Agent
//
//  Created by Codex on 2026/3/19.
//

import Foundation

final class HTTPAgentService: AgentChatService {
    func chat(request: AgentChatRequest) async throws -> AgentChatResult {
        let normalizedEndpoint = normalizeEndpoint(request.endpoint)
        guard let url = URL(string: normalizedEndpoint) else {
            throw AgentServiceError.invalidEndpoint
        }

        let payload = ChatCompletionsRequest(
            model: request.model,
            messages: request.messages,
            max_tokens: request.maxTokens
        )

        var urlRequest = URLRequest(url: url)
        urlRequest.httpMethod = "POST"
        urlRequest.setValue("application/json", forHTTPHeaderField: "Content-Type")
        urlRequest.httpBody = try JSONEncoder().encode(payload)

        let (data, response) = try await URLSession.shared.data(for: urlRequest)
        guard let httpResponse = response as? HTTPURLResponse else {
            throw AgentServiceError.invalidResponse
        }

        guard (200..<300).contains(httpResponse.statusCode) else {
            let body = String(data: data, encoding: .utf8) ?? "unknown error"
            throw AgentServiceError.backend("HTTP \(httpResponse.statusCode): \(body)")
        }

        let decoded = try JSONDecoder().decode(ChatCompletionsResponse.self, from: data)
        return AgentChatResult(
            text: decoded.choices.first?.message.content ?? "",
            transport: .http
        )
    }

    private func normalizeEndpoint(_ raw: String) -> String {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        let noTrailingSlash = trimmed.hasSuffix("/") ? String(trimmed.dropLast()) : trimmed

        if noTrailingSlash.hasSuffix("/chat/completions") {
            return noTrailingSlash
        }
        if noTrailingSlash.hasSuffix("/v1") {
            return "\(noTrailingSlash)/chat/completions"
        }
        return noTrailingSlash
    }
}

private struct ChatCompletionsRequest: Encodable {
    let model: String
    let messages: [AgentChatMessage]
    let max_tokens: Int
}

private struct ChatCompletionsResponse: Decodable {
    let choices: [Choice]

    struct Choice: Decodable {
        let message: Message
    }

    struct Message: Decodable {
        let content: String
    }
}
