//
//  AgentServiceRouter.swift
//  Agent
//
//  Created by Codex on 2026/3/19.
//

import Foundation

final class AgentServiceRouter {
    private let xpcService = XPCAgentService()

    func ping() async throws {
        try await xpcService.ping()
    }

    func chat(request: AgentChatRequest) async throws -> AgentChatResult {
        try await xpcService.chat(request: request)
    }
}
