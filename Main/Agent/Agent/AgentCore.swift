//
//  AgentCore.swift
//  Agent
//
//  Created by Codex on 2026/3/19.
//

import Foundation

enum AgentTransport: String, Sendable {
    case xpc = "XPC"
    case http = "HTTP"
}

struct AgentChatMessage: Codable, Sendable {
    let role: String
    let content: String
}

struct AgentChatRequest: Codable, Sendable {
    let model: String
    let messages: [AgentChatMessage]
    let maxTokens: Int
    let endpoint: String
}

struct AgentChatResult: Sendable {
    let text: String
    let transport: AgentTransport
}

enum AgentServiceError: LocalizedError {
    case invalidEndpoint
    case invalidResponse
    case backend(String)
    case unavailableTransport(String)

    var errorDescription: String? {
        switch self {
        case .invalidEndpoint:
            return "Invalid endpoint URL."
        case .invalidResponse:
            return "Invalid server response."
        case let .backend(message):
            return message
        case let .unavailableTransport(message):
            return message
        }
    }
}

protocol AgentChatService {
    func chat(request: AgentChatRequest) async throws -> AgentChatResult
}
